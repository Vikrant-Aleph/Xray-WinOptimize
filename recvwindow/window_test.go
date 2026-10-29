package recvwindow

import (
	"testing"
	"time"
)

func TestGrowthAndBounds(t *testing.T) {
	c := New()
	now := time.Now()
	_, _ = c.Observe(now, 160*time.Millisecond, 0, 0)
	if _, grew := c.Observe(now.Add(80*time.Millisecond), 160*time.Millisecond, 200000, 0); grew {
		t.Fatal("grew before one RTT")
	}
	w, grew := c.Observe(now.Add(170*time.Millisecond), 160*time.Millisecond, 230000, 40000)
	if !grew || w <= Initial {
		t.Fatalf("expected growth, got %v %v", w, grew)
	}
	for i := 2; i < 100; i++ {
		c.Observe(now.Add(time.Duration(i)*170*time.Millisecond), 160*time.Millisecond, uint64(i)*500000, 160000)
	}
	if c.Window > Maximum {
		t.Fatalf("over max: %d", c.Window)
	}
}
func TestNoGrowthOnIdle(t *testing.T) {
	c := New()
	n := time.Now()
	c.Observe(n, 150*time.Millisecond, 0, 0)
	if _, g := c.Observe(n.Add(time.Second), 150*time.Millisecond, 1024, 0); g {
		t.Fatal("unexpected growth on idle")
	}
}
func TestSequenceReset(t *testing.T) {
	c := New()
	n := time.Now()
	c.Observe(n, 100*time.Millisecond, 90000, 0)
	if _, g := c.Observe(n.Add(time.Second), 100*time.Millisecond, 10, 0); g {
		t.Fatal("growth on counter reset")
	}
}
func TestReorderCanDriveGrowth(t *testing.T) {
	c := New()
	n := time.Now()
	c.Observe(n, 100*time.Millisecond, 0, 0)
	w, g := c.Observe(n.Add(110*time.Millisecond), 100*time.Millisecond, 80000, 250000)
	if !g || w <= Initial {
		t.Fatalf("expected reorder-influenced growth: %v %v", w, g)
	}
}
