// Package recvwindow implements an experimental Linux-inspired TCP receive window controller.
// It is NOT a line-for-line Linux kernel port: Linux has socket-memory accounting and
// recv RTT observations which are not fully available here.
package recvwindow

import "time"

const (
	Initial = 256 << 10
	Maximum = 8 << 20
)

type Controller struct {
	Window    int32
	lastTime  time.Time
	lastTotal uint64
	priorPeak uint64
}

func New() Controller { return Controller{Window: Initial} }

// Observe measures bytes delivered to the application over roughly one RTT.
// Delivered is a monotonically increasing per-connection counter, not instantaneous throughput.
// ReorderSpan is the TCP sequence-space gap represented by buffered out-of-order data.
// The controller only grows; shrinking without knowing outstanding advertisements is unsafe.
func (c *Controller) Observe(now time.Time, rtt time.Duration, delivered uint64, reorderSpan int32) (int32, bool) {
	if c.Window <= 0 {
		c.Window = Initial
	}
	if rtt <= 0 {
		return c.Window, false
	}
	if rtt < 10*time.Millisecond {
		rtt = 10 * time.Millisecond
	}
	if rtt > 3*time.Second {
		rtt = 3 * time.Second
	}
	if c.lastTime.IsZero() || delivered < c.lastTotal {
		c.lastTime, c.lastTotal = now, delivered
		return c.Window, false
	}
	if now.Sub(c.lastTime) < rtt {
		return c.Window, false
	}
	copied := delivered - c.lastTotal
	c.lastTime, c.lastTotal = now, delivered
	if copied == 0 || c.Window >= Maximum {
		return c.Window, false
	}
	if reorderSpan < 0 {
		reorderSpan = 0
	}
	if reorderSpan > Maximum {
		reorderSpan = Maximum
	}
	// Approximate Linux DRS: react to an increase in data consumed per RTT.
	// The current window must already be substantially used, otherwise no growth.
	if copied < uint64(c.Window)/3 && reorderSpan < c.Window/4 {
		if copied > c.priorPeak {
			c.priorPeak = copied
		}
		return c.Window, false
	}
	candidate := copied*2 + uint64(reorderSpan)
	if copied > c.priorPeak && c.priorPeak > 0 {
		candidate += 2 * (copied - c.priorPeak)
	}
	if copied > c.priorPeak {
		c.priorPeak = copied
	}
	if candidate <= uint64(c.Window) {
		return c.Window, false
	}
	// No more than doubling per RTT; this bounds memory and ACK-window jumps.
	maxStep := uint64(c.Window) * 2
	if candidate > maxStep {
		candidate = maxStep
	}
	if candidate > Maximum {
		candidate = Maximum
	}
	// Round to MSS-ish granularity without losing the bound.
	candidate = (candidate + 4095) &^ uint64(4095)
	if candidate > Maximum {
		candidate = Maximum
	}
	if candidate <= uint64(c.Window) {
		return c.Window, false
	}
	c.Window = int32(candidate)
	return c.Window, true
}
