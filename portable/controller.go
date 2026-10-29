package portable

import "time"

// winOptSample is collected from Windows SIO_TCP_INFO. BytesIn counts data
// received by TCP, NOT bytes consumed by the application. RTT is the TCP
// sender-side RTT estimate, NOT Linux's rcv_rtt_est. BytesReordered is a
// cumulative counter, NOT the current out-of-order queue length.
type winOptSample struct {
	At             time.Time
	RTT            time.Duration
	BytesIn        uint64
	RcvBuf         uint32
	RcvWnd         uint32
	BytesReordered uint32
}

type winOptController struct {
	maxBytes       uint32
	last           winOptSample
	ready          bool
	roundStart     winOptSample
	roundReady     bool
	previousRound  uint64
	activityRounds int
	applied        uint32
	lastChange     time.Time
	lastProbe      time.Time
	flatProbes     int
	reorderHint    uint64
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
// The first 1MiB proactive probe breaks Windows' low-window/low-bandwidth
// feedback loop. After that, growth is constrained by visible window pressure
// and by a global memory policy in the Windows socket layer.
func (c *winOptController) Observe(s winOptSample) uint32 {
	if s.At.IsZero() || s.RTT <= 0 || s.RcvBuf == 0 {
		c.last = s
		c.ready = true
		c.roundReady = false
		c.activityRounds = 0
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
		c.activityRounds = 0
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
		c.activityRounds = 0
		c.previousRound = bytesThisRound
		return 0
	}
	c.activityRounds++

	if c.applied == 0 {
		// Do not disable Windows' own autotuning for short/low-RTT flows.
		if s.RTT >= winOptMinProbeRTT && s.BytesIn >= winOptProbeBytes &&
			c.activityRounds >= 2 && s.RcvBuf < 768*1024 {
			return minU32(winOptMiB, c.maxBytes)
		}
		c.previousRound = bytesThisRound
		return 0
	}
	if s.At.Sub(c.lastChange) < maxDuration(2*s.RTT, time.Second) {
		c.previousRound = bytesThisRound
		return 0
	}
	if c.applied >= c.maxBytes {
		c.previousRound = bytesThisRound
		return 0
	}

	// RcvWnd is the advertised window, NOT free receive-buffer memory.
	// A low window is used only as a conservative pressure gate.
	pressure := uint64(s.RcvWnd)*100 < uint64(c.applied)*70
	current := bytesThisRound
	old := c.previousRound
	c.previousRound = current
	if !pressure {
		c.flatProbes = 0
		return 0
	}

	// Modern Linux reserves at least 2x the RTT copied-data window, and
	// additionally anticipates slow-start growth. Avoid float overflow.
	target := satMul(current, 2)
	if old > 0 && current > old {
		gain := current - old
		target = satAdd(target, satMulDiv(current, satMul(4, gain), old))
		c.flatProbes = 0
	} else if c.flatProbes < 1 && s.At.Sub(c.lastProbe) > 3*s.RTT {
		// A *single* exploratory doubling despite flat measurements avoids
		// treating a window-limited delivery rate as available path bandwidth.
		target = maxU64(target, uint64(c.applied)*2)
		c.flatProbes++
		c.lastProbe = s.At
	}
	// Linux adds current ooo queue span; Windows exposes no equivalent here.
	// Bounded cumulative-counter delta is a pressure hint, NOT queue bytes.
	reorderHeadroom := minU64(reordered, 256*1024)
	target = satAdd(target, reorderHeadroom)
	// Windows SO_RCVBUF is not Linux skb truesize. 5/4 is an empirical margin.
	target = satMulDiv(target, 5, 4)
	if target <= uint64(c.applied)*9/10 {
		return 0
	}
	// Grow at most 2x on each decision, and never above the configured cap.
	next := minU64(target, uint64(c.applied)*2)
	next = minU64(next, uint64(c.maxBytes))
	if next <= uint64(c.applied) {
		return 0
	}
	// Align increases to 64KiB to avoid a stream of tiny setsockopt calls.
	rounded := (next + (64*1024 - 1)) / (64 * 1024) * (64 * 1024)
	return uint32(minU64(rounded, uint64(c.maxBytes)))
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
