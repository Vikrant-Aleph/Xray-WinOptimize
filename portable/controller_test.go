package portable

import (
	"testing"
	"time"
)

func sample(at time.Time, ms int, bytes uint64, buf, wnd uint32) winOptSample {
	return winOptSample{At: at, RTT: time.Duration(ms) * time.Millisecond, BytesIn: bytes, RcvBuf: buf, RcvWnd: wnd}
}
func TestHighRTTBulkProbe(t *testing.T) {
	c := winOptNewController(2 * winOptMiB)
	now := time.Now()
	for i := 0; i < 3; i++ {
		got := c.Observe(sample(now.Add(time.Duration(i)*350*time.Millisecond), 160, uint64(60+i*80)*1024, 256*1024, 128*1024))
		if i < 2 && got != 0 {
			t.Fatalf("premature probe %d", got)
		}
		if i == 2 && got != winOptMiB {
			t.Fatalf("expected probe,got %d", got)
		}
	}
}
func TestLowRTTNeverProbes(t *testing.T) {
	c := winOptNewController(2 * winOptMiB)
	now := time.Now()
	for i := 0; i < 10; i++ {
		if n := c.Observe(sample(now.Add(time.Duration(i)*350*time.Millisecond), 12, uint64(i)*100*1024, 256*1024, 128*1024)); n != 0 {
			t.Fatalf("low RTT probe %d", n)
		}
	}
}
func TestNoTinyFlowProbe(t *testing.T) {
	c := winOptNewController(2 * winOptMiB)
	now := time.Now()
	for i := 0; i < 10; i++ {
		if n := c.Observe(sample(now.Add(time.Duration(i)*350*time.Millisecond), 160, uint64(i)*2048, 256*1024, 128*1024)); n != 0 {
			t.Fatalf("tiny flow probe %d", n)
		}
	}
}
func TestPressureAndCeiling(t *testing.T) {
	c := winOptNewController(2 * winOptMiB)
	now := time.Now()
	for i := 0; i < 3; i++ {
		c.Observe(sample(now.Add(time.Duration(i)*350*time.Millisecond), 160, uint64(60+i*200)*1024, 256*1024, 128*1024))
	}
	c.Applied(winOptMiB, now.Add(700*time.Millisecond))
	// Pressure sustained after cooldown, BDP close to 1 MiB.
	got := c.Observe(sample(now.Add(2*time.Second), 160, 4*1024*1024, winOptMiB, 512*1024))
	if got != 2*winOptMiB {
		t.Fatalf("expected 2MiB, got %d", got)
	}
	c.Applied(got, now.Add(2*time.Second))
	got = c.Observe(sample(now.Add(4*time.Second), 160, 8*1024*1024, 2*winOptMiB, 512*1024))
	if got != 0 {
		t.Fatalf("exceeded ceiling: %d", got)
	}
}
func TestNoPressureNoMoreGrowth(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	now := time.Now()
	c.Observe(sample(now, 160, 100*1024, 256*1024, 128*1024))
	c.Observe(sample(now.Add(time.Second), 160, 300*1024, 256*1024, 128*1024))
	c.Applied(winOptMiB, now.Add(time.Second))
	if got := c.Observe(sample(now.Add(3*time.Second), 160, uint64(3*winOptMiB), winOptMiB, winOptMiB)); got != 0 {
		t.Fatalf("grew without pressure %d", got)
	}
}

func TestRTTPacedGrowthOnlyAfterRound(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	start := time.Unix(100, 0)
	if n := c.Observe(sample(start, 900, 0, 256*1024, 128*1024)); n != 0 {
		t.Fatal(n)
	}
	for i := 1; i < 5; i++ {
		if n := c.Observe(sample(start.Add(time.Duration(i)*200*time.Millisecond), 900, uint64(i)*128*1024, 256*1024, 128*1024)); n != 0 {
			t.Fatalf("sample %d changed buffer before RTT: %d", i, n)
		}
	}
	if n := c.Observe(sample(start.Add(time.Second), 900, 700*1024, 256*1024, 128*1024)); n != 0 {
		t.Fatalf("first complete RTT should not force changes: %d", n)
	}
	if n := c.Observe(sample(start.Add(2*time.Second), 900, 1600*1024, 256*1024, 128*1024)); n != winOptMiB {
		t.Fatalf("second busy RTT should probe: %d", n)
	}
}
func TestLongRTTGrowthMayExceedTwoMiB(t *testing.T) {
	c := winOptNewController(16 * winOptMiB)
	t0 := time.Unix(100, 0)
	_ = c.Observe(sample(t0, 500, 0, 256*1024, 128*1024))
	_ = c.Observe(sample(t0.Add(500*time.Millisecond), 500, 300*1024, 256*1024, 128*1024))
	_ = c.Observe(sample(t0.Add(time.Second), 500, 600*1024, 256*1024, 128*1024))
	c.Applied(winOptMiB, t0.Add(time.Second))
	_ = c.Observe(sample(t0.Add(1500*time.Millisecond), 500, 1200*1024, winOptMiB, 256*1024)) // cooldown
	n := c.Observe(sample(t0.Add(2500*time.Millisecond), 500, 2700*1024, winOptMiB, 256*1024))
	if n <= winOptMiB {
		t.Fatalf("expected post-RTT adaptive growth, got %d", n)
	}
	if n > 2*winOptMiB {
		t.Fatalf("grew more than 2x: %d", n)
	}
}
func TestOverflowDoesNotWrapBelowApplied(t *testing.T) {
	c := winOptNewController(64 * winOptMiB)
	t0 := time.Unix(100, 0)
	_ = c.Observe(sample(t0, 160, 0, winOptMiB, 256*1024))
	c.Applied(winOptMiB, t0)
	_ = c.Observe(sample(t0.Add(time.Second), 160, 100*1024, winOptMiB, 256*1024))
	n := c.Observe(sample(t0.Add(2*time.Second), 160, ^uint64(0)-1, winOptMiB, 256*1024))
	if n > 64*winOptMiB {
		t.Fatalf("overflow: %d", n)
	}
}
