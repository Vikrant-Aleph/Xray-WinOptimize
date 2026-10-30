package portable

import (
	"testing"
	"time"
)

func sample(at time.Time, ms int, bytes uint64, buf, wnd uint32) winOptSample {
	return winOptSample{At: at, RTT: time.Duration(ms) * time.Millisecond, BytesIn: bytes, RcvBuf: buf, RcvWnd: wnd}
}

func TestInitialGrowthIsDemandBasedAndNotOneMiB(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	now := time.Unix(100, 0)
	for i := 0; i < 3; i++ {
		got := c.Observe(sample(now.Add(time.Duration(i)*350*time.Millisecond), 160, uint64(100+i*90)*1024, 256*1024, 256*1024))
		if i < 2 && got != 0 {
			t.Fatalf("premature growth %d", got)
		}
		if i == 2 && got != 640*1024 {
			t.Fatalf("expected bounded first increase from steady demand, got %d", got)
		}
	}
}

func TestLowRTTDoesNotInterveneInitially(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	now := time.Unix(100, 0)
	for i := 0; i < 10; i++ {
		if n := c.Observe(sample(now.Add(time.Duration(i)*350*time.Millisecond), 12, uint64(i)*100*1024, 256*1024, 128*1024)); n != 0 {
			t.Fatalf("low RTT first growth %d", n)
		}
	}
}

func TestNoTinyFlowIntervention(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	now := time.Unix(100, 0)
	for i := 0; i < 10; i++ {
		if n := c.Observe(sample(now.Add(time.Duration(i)*350*time.Millisecond), 160, uint64(i)*2048, 256*1024, 128*1024)); n != 0 {
			t.Fatalf("tiny flow increase %d", n)
		}
	}
}

func TestNoAdvertisedWindowPressureGate(t *testing.T) {
	c := winOptNewController(4 * winOptMiB)
	start := time.Unix(100, 0)
	buf := winOptMiB
	c.Observe(sample(start, 160, 0, buf, buf))
	c.Applied(buf, start)
	c.Observe(sample(start.Add(200*time.Millisecond), 160, 200*1024, buf, buf))
	// RcvWnd == RcvBuf. The old 70% gate would have discarded this sample.
	n := c.Observe(sample(start.Add(1400*time.Millisecond), 160, 1600*1024, buf, buf))
	if n <= buf || n > 4*buf {
		t.Fatalf("expected demand-based growth with full advertised window within 4x cap, got %d", n)
	}
}

func TestGrowthIsNotConditionedOnAdvertisedWindow(t *testing.T) {
	start := time.Unix(100, 0)
	var proposals []uint32
	for _, wnd := range []uint32{64 * 1024, winOptMiB} {
		c := winOptNewController(8 * winOptMiB)
		c.Observe(sample(start, 160, 0, winOptMiB, wnd))
		c.Applied(winOptMiB, start)
		c.Observe(sample(start.Add(200*time.Millisecond), 160, 400*1024, winOptMiB, wnd))
		proposals = append(proposals, c.Observe(sample(start.Add(1400*time.Millisecond), 160, 1500*1024, winOptMiB, wnd)))
	}
	if proposals[0] == 0 || proposals[0] != proposals[1] {
		t.Fatalf("advertised window wrongly gated growth: %v", proposals)
	}
}

func TestGrowthHonorsCooldownAndCap(t *testing.T) {
	c := winOptNewController(2 * winOptMiB)
	start := time.Unix(100, 0)
	c.Observe(sample(start, 160, 0, winOptMiB, winOptMiB))
	c.Applied(winOptMiB, start)
	if n := c.Observe(sample(start.Add(200*time.Millisecond), 160, 512*1024, winOptMiB, winOptMiB)); n != 0 {
		t.Fatalf("cooldown ignored: %d", n)
	}
	n := c.Observe(sample(start.Add(1400*time.Millisecond), 160, 4*1024*1024, winOptMiB, winOptMiB))
	if n != 2*winOptMiB {
		t.Fatalf("expected capped 2MiB growth, got %d", n)
	}
	c.Applied(n, start.Add(1400*time.Millisecond))
	if n = c.Observe(sample(start.Add(3*time.Second), 160, 8*1024*1024, 2*winOptMiB, 2*winOptMiB)); n != 0 {
		t.Fatalf("exceeded cap: %d", n)
	}
}

func TestLowDemandAfterProbingDoesNotExpandIndefinitely(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	start := time.Unix(100, 0)
	c.Observe(sample(start, 160, 100*1024, 256*1024, 256*1024))
	c.Observe(sample(start.Add(200*time.Millisecond), 160, 180*1024, 256*1024, 256*1024))
	if n := c.Observe(sample(start.Add(400*time.Millisecond), 160, 260*1024, 256*1024, 256*1024)); n != 640*1024 {
		t.Fatalf("expected one bounded capacity probe, got %d", n)
	}
	c.Applied(640*1024, start.Add(400*time.Millisecond))
	// Only 80KiB during the next full measurement cycle; one flat probe
	// already happened, so don't keep doubling a low-demand connection.
	if n := c.Observe(sample(start.Add(1600*time.Millisecond), 160, 340*1024, 640*1024, 640*1024)); n != 0 {
		t.Fatalf("repeated unproductive probe %d", n)
	}
}

func TestFlatFlowDoesNotBlindlyDoubleALargeBuffer(t *testing.T) {
	c := winOptNewController(16 * winOptMiB)
	start := time.Unix(100, 0)
	buf := uint32(4 * winOptMiB)
	c.Observe(sample(start, 160, 0, buf, buf))
	c.Observe(sample(start.Add(200*time.Millisecond), 160, 80*1024, buf, buf))
	if n := c.Observe(sample(start.Add(400*time.Millisecond), 160, 160*1024, buf, buf)); n != 0 {
		t.Fatalf("tiny transfer triggered pointless 4MiB buffer doubling: %d", n)
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
	if n := c.Observe(sample(start.Add(time.Second), 900, 700*1024, 256*1024, 128*1024)); n != 1*winOptMiB {
		t.Fatalf("first completed RTT should grow at most 4x when demand warrants: %d", n)
	}
}

func TestGenericGrowthWorksBeyondTwoMiB(t *testing.T) {
	c := winOptNewController(16 * winOptMiB)
	start := time.Unix(100, 0)
	base := uint32(4 * winOptMiB)
	c.Observe(sample(start, 200, 0, base, base))
	c.Applied(base, start)
	c.Observe(sample(start.Add(200*time.Millisecond), 200, uint64(3*winOptMiB), base, base))
	n := c.Observe(sample(start.Add(1600*time.Millisecond), 200, uint64(10*winOptMiB), base, base))
	if n != 16*winOptMiB {
		t.Fatalf("expected size-independent 4->16MiB growth when demand warrants, got %d", n)
	}
}

func TestGrowthIgnoresLegacyAggressiveFlag(t *testing.T) {
	start := time.Unix(100, 0)
	decide := func(env string) uint32 {
		t.Setenv("XRAY_WINOPT_GROWTH", env)
		c := winOptNewController(8 * winOptMiB)
		c.Observe(sample(start, 160, 0, winOptMiB, winOptMiB))
		c.Applied(winOptMiB, start)
		c.Observe(sample(start.Add(200*time.Millisecond), 160, 400*1024, winOptMiB, winOptMiB))
		return c.Observe(sample(start.Add(1400*time.Millisecond), 160, 1500*1024, winOptMiB, winOptMiB))
	}
	if a, b := decide(""), decide("aggressive"); a != b || a == 0 {
		t.Fatalf("legacy setting should have no effect: default=%d legacy=%d", a, b)
	}
}

func TestOverflowDoesNotWrapBelowApplied(t *testing.T) {
	c := winOptNewController(64 * winOptMiB)
	start := time.Unix(100, 0)
	c.Observe(sample(start, 160, 0, winOptMiB, 256*1024))
	c.Applied(winOptMiB, start)
	c.Observe(sample(start.Add(time.Second), 160, 100*1024, winOptMiB, 256*1024))
	n := c.Observe(sample(start.Add(2*time.Second), 160, ^uint64(0)-1, winOptMiB, 256*1024))
	if n > 64*winOptMiB {
		t.Fatalf("overflow: %d", n)
	}
}

// A burst can justify expanding after a *single* completed measurement RTT;
// no need to spend an additional RTT at the Windows-default receive window.
func TestOneHighDemandRTTCanStartFastRamp(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	start := time.Unix(300, 0)
	buf := uint32(256 * 1024)
	if n := c.Observe(sample(start, 160, 0, buf, buf)); n != 0 {
		t.Fatal(n)
	}
	if n := c.Observe(sample(start.Add(200*time.Millisecond), 160, 800*1024, buf, buf)); n != 1*winOptMiB {
		t.Fatalf("high demand should permit 4x on first RTT, got %d", n)
	}
}

// A supported fast-ramp request must still be separated from its predecessor
// by 400 ms (or one RTT if longer), not an arbitrary 1-second pause.
func TestGrowthCooldownUsesRTTOr400ms(t *testing.T) {
	c := winOptNewController(16 * winOptMiB)
	start := time.Unix(300, 0)
	base := uint32(winOptMiB)
	c.Observe(sample(start, 160, 0, base, base))
	c.Applied(base, start)
	if n := c.Observe(sample(start.Add(200*time.Millisecond), 160, 500*1024, base, base)); n != 0 {
		t.Fatalf("growth before cooldown: %d", n)
	}
	n := c.Observe(sample(start.Add(400*time.Millisecond), 160, 1500*1024, base, base))
	if n <= base || n > 4*base {
		t.Fatalf("should permit growth at 400ms, got %d", n)
	}
	c.Applied(n, start.Add(400*time.Millisecond))
	if again := c.Observe(sample(start.Add(600*time.Millisecond), 160, 2800*1024, n, n)); again != 0 {
		t.Fatalf("cooldown must reset after successful growth, got %d", again)
	}
}

func TestFastRampUsesDemandNotForcedFourfold(t *testing.T) {
	c := winOptNewController(16 * winOptMiB)
	start := time.Unix(300, 0)
	base := uint32(winOptMiB)
	c.Observe(sample(start, 160, 0, base, base))
	c.Applied(base, start)
	n := c.Observe(sample(start.Add(600*time.Millisecond), 160, 560*1024, base, base))
	if n <= base || n >= 2*base {
		t.Fatalf("moderate measured demand must not force 4x: %d", n)
	}
}

func TestFastRampStillHonorsMaximum(t *testing.T) {
	c := winOptNewController(2 * winOptMiB)
	start := time.Unix(300, 0)
	base := uint32(512 * 1024)
	c.Observe(sample(start, 160, 0, base, base))
	c.Applied(base, start)
	n := c.Observe(sample(start.Add(600*time.Millisecond), 160, uint64(16*winOptMiB), base, base))
	if n != 2*winOptMiB {
		t.Fatalf("maximum must constrain fast ramp, got %d", n)
	}
}

func TestTimestampOptionParsing(t *testing.T) {
	cases := []struct {
		value string
		want  int
		set   bool
	}{
		{"", 0, false}, {"auto", 0, false}, {"On", 1, true}, {"1", 1, true},
		{"off", 0, true}, {" FALSE ", 0, true}, {"invalid", 0, false},
	}
	for _, tc := range cases {
		got, ok := winOptTimestampValue(tc.value)
		if got != tc.want || ok != tc.set {
			t.Errorf("input %q => (%d, %v), want (%d, %v)", tc.value, got, ok, tc.want, tc.set)
		}
	}
}

// Unlike a reused SO_RCVBUF value, the RTT history only helps retain the
// first observation window before SIO_TCP_INFO has a current-connection RTT.
func TestHistoricalRTTHintOnlyHelpsObserve(t *testing.T) {
	start := time.Unix(400, 0)
	base := uint32(256 * 1024)
	hinted := winOptNewController(8 * winOptMiB)
	hinted.SetRTTHint(160 * time.Millisecond)
	noHint := winOptNewController(8 * winOptMiB)
	first := winOptSample{At: start, RTT: 0, RcvBuf: base, RcvWnd: base}
	if n := hinted.Observe(first); n != 0 {
		t.Fatalf("hint caused premature change: %d", n)
	}
	if n := noHint.Observe(first); n != 0 {
		t.Fatalf("no-hint premature change: %d", n)
	}
	next := sample(start.Add(200*time.Millisecond), 160, 800*1024, base, base)
	if n := noHint.Observe(next); n != 0 {
		t.Fatalf("unhinted connection should just initialize: %d", n)
	}
	if n := hinted.Observe(next); n != winOptMiB {
		t.Fatalf("hint should allow first completed RTT observation: %d", n)
	}
}

func TestRTTHintCannotCauseGrowthWithoutLiveRTT(t *testing.T) {
	c := winOptNewController(8 * winOptMiB)
	c.SetRTTHint(160 * time.Millisecond)
	start := time.Unix(400, 0)
	for i := 0; i < 10; i++ {
		s := winOptSample{At: start.Add(time.Duration(i) * 200 * time.Millisecond), RTT: 0, BytesIn: uint64(i) * 1024 * 1024, RcvBuf: 256 * 1024, RcvWnd: 256 * 1024}
		if n := c.Observe(s); n != 0 {
			t.Fatalf("cached RTT must not authorize SO_RCVBUF, got %d", n)
		}
	}
}
