package portable

import (
	"strings"
	"time"
)

// winOptSample is collected from Windows SIO_TCP_INFO. BytesIn counts data
// received by TCP, NOT bytes consumed by the application. RTT is the TCP
// sender-side RTT estimate, NOT Linux's rcv_rtt_est. BytesReordered is a
// cumulative counter, NOT the current out-of-order queue length.

// winOptTimestampValue parses an opt-in Windows TCP_TIMESTAMPS experiment.
// The default (including an empty string) leaves the system policy untouched.
func winOptTimestampValue(value string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "1", "enabled", "true":
		return 1, true
	case "off", "0", "disabled", "false":
		return 0, true
	default:
		return 0, false
	}
}

type winOptSample struct {
	At             time.Time
	RTT            time.Duration
	BytesIn        uint64
	RcvBuf         uint32
	RcvWnd         uint32
	BytesReordered uint32
}

type winOptController struct {
	maxBytes      uint32
	last          winOptSample
	ready         bool
	roundStart    winOptSample
	roundReady    bool
	previousRound uint64
	applied       uint32
	lastChange    time.Time
	lastProbe     time.Time
	flatProbes    int
	reorderHint   uint64
}

const (
	winOptMiB           = uint32(1024 * 1024)
	winOptProbeBytes    = 128 * 1024
	winOptMinRoundBytes = 32 * 1024
	winOptMinProbeRTT   = 40 * time.Millisecond // Portable policy, not Linux kernel constant.
)

func winOptNewController(maxBytes uint32) winOptController {
	if maxBytes < winOptMiB {
		maxBytes = winOptMiB
	}
	if maxBytes > 64*winOptMiB {
		maxBytes = 64 * winOptMiB
	}
	return winOptController{maxBytes: maxBytes}
}

// Observe uses a receiver-RTT-paced measurement window and adapts the modern
// Linux tcp_rcvbuf_grow idea:
//
//	target_window = 2 * current_round_bytes
//	              + 4 * current_round_bytes * (growth / previous_round_bytes)
//
// (when the copied bytes per RTT are rising).
// Linux's exact sk_buff accounting and out-of-order queue length are not
// available from Windows. We use an explicitly bounded reorder *hint* and
// conservative socket-memory headroom, rather than claiming exact parity.
// The same demand-based growth formula is used for the first intervention and
// every later adjustment. The advertised RcvWnd is not an occupancy or BDP
// measurement, so it is not used as a percentage-based growth gate.
// A single bounded exploratory doubling can break a flat-rate feedback loop.
// Growth is further limited by the Windows socket-layer memory budget.
// Fast ramp: permit the first qualified RTT window to intervene, shorten
// the adjustment cooldown, and accept up to 4x when *measured demand*
// warrants it. The existing per-socket and process memory bounds remain.
func (c *winOptController) Observe(s winOptSample) uint32 {
	if s.At.IsZero() || s.RTT <= 0 || s.RcvBuf == 0 {
		c.last = s
		c.ready = true
		c.roundReady = false
		return 0
	}
	if !c.ready {
		c.last = s
		c.roundStart = s
		c.ready = true
		c.roundReady = true
		return 0
	}
	prev := c.last
	c.last = s
	if s.At.Before(prev.At) || s.BytesIn < prev.BytesIn || s.At.Sub(prev.At) > 10*time.Second {
		c.roundStart = s
		c.roundReady = true
		c.previousRound = 0
		c.reorderHint = 0
		return 0
	}
	if !c.roundReady {
		c.roundStart = s
		c.roundReady = true
		return 0
	}
	// SIO_TCP_INFO's RTT is an imperfect proxy for Linux's receiver RTT.
	roundLength := s.RTT
	if roundLength < 100*time.Millisecond {
		roundLength = 100 * time.Millisecond
	}
	if s.At.Sub(c.roundStart.At) < roundLength {
		return 0
	}
	bytesThisRound := s.BytesIn - c.roundStart.BytesIn
	reordered := uint64(0)
	if s.BytesReordered >= c.roundStart.BytesReordered {
		reordered = uint64(s.BytesReordered - c.roundStart.BytesReordered)
	}
	c.roundStart = s
	if bytesThisRound < winOptMinRoundBytes {
		c.previousRound = bytesThisRound
		return 0
	}

	// The first intervention still excludes tiny and very low-RTT flows.
	// One completed, sufficiently active RTT window is enough to evaluate demand.
	// Once active, use exactly the same demand estimator as for later growth;
	// there are no fixed-size 1 MiB / 2 MiB special cases.
	if c.applied == 0 {
		if s.RTT < winOptMinProbeRTT || s.BytesIn < winOptProbeBytes {
			c.previousRound = bytesThisRound
			return 0
		}
	} else if s.At.Sub(c.lastChange) < maxDuration(s.RTT, 400*time.Millisecond) {
		c.previousRound = bytesThisRound
		return 0
	}

	// Compare the formula against the buffer Windows currently reports.
	// After an earlier successful setsockopt, use at least our requested size.
	// The current advertised RcvWnd is NOT free space and cannot determine
	// whether this capacity suffices for the path BDP.
	baseline := s.RcvBuf
	if c.applied > baseline {
		baseline = c.applied
	}
	if baseline >= c.maxBytes {
		c.previousRound = bytesThisRound
		return 0
	}
	current := bytesThisRound
	old := c.previousRound
	c.previousRound = current

	// Modern Linux reserves at least 2x the RTT copied-data window, and
	// additionally anticipates slow-start growth. Avoid float overflow.
	target := satMul(current, 2)
	if old > 0 && current > old {
		gain := current - old
		target = satAdd(target, satMulDiv(current, satMul(4, gain), old))
		c.flatProbes = 0
	} else if old > 0 && c.flatProbes < 1 && s.BytesIn >= uint64(baseline) && s.At.Sub(c.lastProbe) > 3*s.RTT {
		// One size-independent exploratory step per flat-rate episode can
		// reveal a window-limited flow even when the estimator stalls.
		// Only probe after observing at least one buffer's worth of total
		// received data, avoiding huge probes on short, small transfers.
		target = maxU64(target, uint64(baseline)*2)
		c.flatProbes++
		c.lastProbe = s.At
	}
	// Linux adds current ooo queue span; Windows exposes no equivalent here.
	// Bounded cumulative-counter delta is a pressure hint, NOT queue bytes.
	reorderHeadroom := minU64(reordered, 256*1024)
	target = satAdd(target, reorderHeadroom)
	// Windows SO_RCVBUF is not Linux skb truesize. 5/4 is an empirical margin.
	target = satMulDiv(target, 5, 4)
	// Grow only if estimated need exceeds the *current* socket buffer.
	// No arbitrary RcvWnd / RcvBuf percentage gate.
	if target <= uint64(baseline) {
		return 0
	}
	// Fast ramp: permit up to 4x only when the estimator requests it.
	// The cap, quota and low-memory guard still bound every adjustment.
	next := minU64(target, satMul(uint64(baseline), 4))
	next = minU64(next, uint64(c.maxBytes))
	if next <= uint64(baseline) {
		return 0
	}
	// Align increases to 64KiB to avoid a stream of tiny setsockopt calls.
	rounded := (next + (64*1024 - 1)) / (64 * 1024) * (64 * 1024)
	proposal := uint32(minU64(rounded, uint64(c.maxBytes)))
	if proposal <= baseline {
		return 0
	}
	return proposal
}

func (c *winOptController) Applied(bytes uint32, at time.Time) {
	if bytes > c.applied {
		c.applied = bytes
		c.lastChange = at
	}
}
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
func minU32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
func minU64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
func satAdd(a, b uint64) uint64 {
	if (^uint64(0))-a < b {
		return ^uint64(0)
	}
	return a + b
}
func satMul(a, b uint64) uint64 {
	if a != 0 && b > (^uint64(0))/a {
		return ^uint64(0)
	}
	return a * b
}
func satMulDiv(a, b, d uint64) uint64 {
	if d == 0 {
		return ^uint64(0)
	}
	if a != 0 && b > (^uint64(0))/a {
		return ^uint64(0)
	}
	return a * b / d
}
