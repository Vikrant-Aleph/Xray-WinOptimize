package portable

import (
	"testing"
	"time"
)

func TestRTTCacheBoundedPerTargetAndExpiration(t *testing.T) {
	c := newWinOptPathRTTCache(2, time.Minute)
	now := time.Unix(100, 0)
	c.remember("a:443", 160*time.Millisecond, now)
	c.remember("b:443", 50*time.Millisecond, now.Add(time.Second))
	if got := c.lookup("a:443", now.Add(2*time.Second)); got != 160*time.Millisecond {
		t.Fatalf("a RTT = %v", got)
	}
	if got := c.lookup("b:443", now.Add(2*time.Second)); got != 50*time.Millisecond {
		t.Fatalf("b RTT = %v", got)
	}
	c.remember("c:443", 70*time.Millisecond, now.Add(3*time.Second))
	if got := c.lookup("a:443", now.Add(4*time.Second)); got != 0 {
		t.Fatalf("oldest entry not evicted: %v", got)
	}
	if got := c.lookup("c:443", now.Add(4*time.Second)); got != 70*time.Millisecond {
		t.Fatalf("new entry lost: %v", got)
	}
	if got := c.lookup("c:443", now.Add(65*time.Second)); got != 0 {
		t.Fatalf("expired entry: %v", got)
	}
}

func TestRTTHistorySmoothsRejectsInvalidAndDoesNotRememberBuffer(t *testing.T) {
	c := newWinOptPathRTTCache(256, 10*time.Minute)
	now := time.Unix(300, 0)
	c.remember("example:443", 160*time.Millisecond, now)
	c.remember("example:443", 240*time.Millisecond, now.Add(time.Second))
	if r := c.lookup("example:443", now.Add(2*time.Second)); r != 170*time.Millisecond {
		t.Fatalf("EWMA: %s", r)
	}
	c.remember("example:443", 0, now.Add(3*time.Second))
	c.remember("example:443", 11*time.Second, now.Add(3*time.Second))
	if r := c.lookup("example:443", now.Add(4*time.Second)); r != 170*time.Millisecond {
		t.Fatalf("invalid RTT changed history: %s", r)
	}
	if r := c.lookup("other:443", now.Add(2*time.Second)); r != 0 {
		t.Fatalf("history leaked across targets: %s", r)
	}
}

func TestPreconnectCapacityIsExplicitOptIn(t *testing.T) {
	for _, v := range []string{"", "off", "0", "false", "surprise"} {
		if winOptPreconnectCapacityEnabled(v) {
			t.Fatalf("preconnect unexpectedly enabled by %q", v)
		}
	}
	for _, v := range []string{"on", "1", " TRUE ", "enabled"} {
		if !winOptPreconnectCapacityEnabled(v) {
			t.Fatalf("preconnect not enabled by %q", v)
		}
	}
}
