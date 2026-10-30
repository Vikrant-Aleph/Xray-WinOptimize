//go:build windows

package portable

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// TCP_INFO_v1 Win64 ABI. Mirrors mstcpip.h; assert struct size/offset below.
type winOptTCPInfo struct {
	State             uint32
	Mss               uint32
	ConnectionTimeMs  uint64
	TimestampsEnabled uint8
	_                 [3]byte
	RttUs             uint32
	MinRttUs          uint32
	BytesInFlight     uint32
	Cwnd              uint32
	SndWnd            uint32
	RcvWnd            uint32
	RcvBuf            uint32
	BytesOut          uint64
	BytesIn           uint64
	BytesReordered    uint32
	BytesRetrans      uint32
	FastRetrans       uint32
	DupAcksIn         uint32
	TimeoutEpisodes   uint32
	SynRetrans        uint8
	_                 [3]byte
	SndLimTransRwin   uint32
	SndLimTimeRwin    uint32
	SndLimBytesRwin   uint64
	SndLimTransCwnd   uint32
	SndLimTimeCwnd    uint32
	SndLimBytesCwnd   uint64
	SndLimTransSnd    uint32
	SndLimTimeSnd     uint32
	SndLimBytesSnd    uint64
}

var _ [136 - int(unsafe.Sizeof(winOptTCPInfo{}))]byte
var _ [int(unsafe.Sizeof(winOptTCPInfo{})) - 136]byte
var _ [44 - int(unsafe.Offsetof(winOptTCPInfo{}.RcvBuf))]byte
var _ [int(unsafe.Offsetof(winOptTCPInfo{}.RcvBuf)) - 44]byte

const winOptSioTCPInfo = uint32(syscall.IOC_INOUT | syscall.IOC_VENDOR | 39)

func winOptInfo(raw syscall.RawConn) (winOptTCPInfo, error) {
	var info winOptTCPInfo
	version := uint32(1)
	var returned uint32
	var callErr error
	err := raw.Control(func(fd uintptr) {
		callErr = syscall.WSAIoctl(syscall.Handle(fd), winOptSioTCPInfo,
			(*byte)(unsafe.Pointer(&version)), uint32(unsafe.Sizeof(version)),
			(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &returned, nil, 0)
	})
	if err != nil {
		return info, err
	}
	if callErr != nil {
		return info, callErr
	}
	if returned < uint32(unsafe.Sizeof(info)) {
		return info, fmt.Errorf("short TCP info: %d", returned)
	}
	return info, nil
}

func winOptEnabled(network string) bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("XRAY_WINOPT_MODE")))
	return (mode == "" || mode == "portable") && strings.HasPrefix(network, "tcp") && os.Getenv("XRAY_EXPERIMENTAL_TCP_STACK") != "1"
}

// Windows MEMORYSTATUSEX. TotalPhys scales the receive-buffer cap in the
// spirit of Linux tcp_rmem[2], but there is no equivalent Windows sysctl.
type winOptMemoryStatus struct {
	Size                 uint32
	Load                 uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// GlobalMemoryStatusEx MEMORYSTATUSEX must be 64 bytes on Windows x64.
var _ [64 - int(unsafe.Sizeof(winOptMemoryStatus{}))]byte
var _ [int(unsafe.Sizeof(winOptMemoryStatus{})) - 64]byte

var winOptGlobalMem = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func winOptMemory() (total, available uint64) {
	m := winOptMemoryStatus{Size: uint32(unsafe.Sizeof(winOptMemoryStatus{}))}
	ok, _, _ := winOptGlobalMem.Call(uintptr(unsafe.Pointer(&m)))
	if ok == 0 {
		return 0, 0
	}
	return m.TotalPhys, m.AvailPhys
}
func winOptLimit() uint32 {
	if n, e := strconv.Atoi(os.Getenv("XRAY_WINOPT_MAX_MIB")); e == nil && n >= 1 && n <= 64 {
		return uint32(n) * winOptMiB
	}
	total, _ := winOptMemory()
	if total == 0 {
		return 8 * winOptMiB
	} // conservative failure fallback
	m := total / 256
	if m < uint64(4*winOptMiB) {
		m = uint64(4 * winOptMiB)
	}
	if m > uint64(32*winOptMiB) {
		m = uint64(32 * winOptMiB)
	}
	return uint32(m)
}
func winOptBudgetLimit() uint64 {
	if n, e := strconv.Atoi(os.Getenv("XRAY_WINOPT_BUDGET_MIB")); e == nil && n >= 32 && n <= 2048 {
		return uint64(n) * uint64(winOptMiB)
	}
	total, _ := winOptMemory()
	if total == 0 {
		return 64 * uint64(winOptMiB)
	}
	m := total / 128
	if m < 64*uint64(winOptMiB) {
		m = 64 * uint64(winOptMiB)
	}
	if m > 512*uint64(winOptMiB) {
		m = 512 * uint64(winOptMiB)
	}
	return m
}
func winOptMemoryHealthy() bool {
	total, free := winOptMemory()
	if total == 0 {
		return true
	} // budget still applies; cannot establish pressure
	threshold := total / 20
	if threshold < 256*uint64(winOptMiB) {
		threshold = 256 * uint64(winOptMiB)
	}
	return free >= threshold
}
func winOptLog(format string, args ...any) {
	if os.Getenv("XRAY_WINOPT_LOG") == "1" {
		fmt.Fprintf(os.Stderr, "WINOPT "+format+"\n", args...)
	}
}

// Global *requested* buffer-size budget, NOT actual resident memory. A slot is
// reserved before setsockopt; after failure it is rolled back; it is released
// when the TCP connection closes. This is a safety approximation, not Linux's
// sk_rmem_alloc or memory-pressure management.
var winOptBudget winOptQuota

func winOptReserve(id *uint64, bytes uint32) bool {
	if !winOptMemoryHealthy() {
		return false
	}
	return winOptBudget.reserve(id, bytes, winOptBudgetLimit())
}
func winOptRelease(id uint64)         { winOptBudget.release(id) }
func winOptReserved(id uint64) uint32 { return winOptBudget.reserved(id) }

type winOptCached struct {
	bytes uint32
	until time.Time
}

var winOptCache = struct {
	sync.Mutex
	entries map[string]winOptCached
}{entries: make(map[string]winOptCached)}

func winOptRemember(address string, bytes uint32) {
	winOptCache.Lock()
	defer winOptCache.Unlock()
	now := time.Now()
	for k, v := range winOptCache.entries {
		if now.After(v.until) {
			delete(winOptCache.entries, k)
		}
	}
	if len(winOptCache.entries) >= 256 {
		for k := range winOptCache.entries {
			delete(winOptCache.entries, k)
			break
		}
	}
	if old, ok := winOptCache.entries[address]; ok && old.bytes > bytes {
		bytes = old.bytes
	}
	winOptCache.entries[address] = winOptCached{bytes: bytes, until: now.Add(10 * time.Minute)}
}
func winOptKnown(address string) uint32 {
	winOptCache.Lock()
	defer winOptCache.Unlock()
	v, ok := winOptCache.entries[address]
	if !ok {
		return 0
	}
	if time.Now().After(v.until) {
		delete(winOptCache.entries, address)
		return 0
	}
	if v.bytes > winOptLimit() {
		return winOptLimit()
	}
	return v.bytes
}

type winOptWarmSocket struct {
	id    uint64
	bytes uint32
}
type winOptConnectState struct {
	sync.Mutex
	pending map[uintptr]winOptWarmSocket
	closed  bool
}

// Control can run concurrently on IPv4 and IPv6 candidates. Never store one
// global "current fd"; retain each pending socket and release losers on dial.
// winOptApplyTCPOpts is an explicit opt-in experiment. It runs before the
// TCP handshake, so a timestamp option can be negotiated on SYN. The option
// affects only Xray TCP outbound sockets; it never modifies OS-wide settings.
// Failure is logged but deliberately does not make the Xray dial fail.
func winOptApplyTCPOpts(raw syscall.RawConn, network, address string) {
	if !winOptEnabled(network) {
		return
	}
	enabled, apply := winOptTimestampValue(os.Getenv("XRAY_WINOPT_TCP_TIMESTAMPS"))
	if !apply {
		return
	}
	const tcpTimestamps = 10 // ws2ipdef.h TCP_TIMESTAMPS
	err := raw.Control(func(fd uintptr) {
		sockErr := syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_TCP, tcpTimestamps, enabled)
		if sockErr != nil {
			winOptLog("tcp_timestamps failed target=%s enabled=%d err=%v", address, enabled, sockErr)
		} else {
			winOptLog("tcp_timestamps target=%s enabled=%d", address, enabled)
		}
	})
	if err != nil {
		winOptLog("tcp_timestamps raw control failed target=%s err=%v", address, err)
	}
}

func winOptBeforeConnect(raw syscall.RawConn, network, address string, state *winOptConnectState) {
	// Default: never apply a learned SO_RCVBUF before SYN. This keeps the
	// default connection handshake independent of previous flow capacity.
	if !winOptEnabled(network) || !winOptPreconnectCapacityEnabled(os.Getenv("XRAY_WINOPT_PRECONNECT_CACHE")) || winOptActiveWatchers.Load() >= 256 {
		return
	}
	bytes := winOptKnown(address)
	if bytes == 0 {
		return
	}
	var handle uintptr
	var id uint64
	var sockErr error
	err := raw.Control(func(fd uintptr) {
		handle = fd
		if !winOptReserve(&id, bytes) {
			return
		}
		sockErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, int(bytes))
	})
	if id == 0 {
		return
	}
	if err != nil || sockErr != nil {
		winOptRelease(id)
		winOptLog("preconnect failed %s %v %v", address, err, sockErr)
		return
	}
	state.Lock()
	if state.pending == nil {
		state.pending = make(map[uintptr]winOptWarmSocket)
	}
	if state.closed {
		state.Unlock()
		winOptRelease(id)
		return
	}
	if old, ok := state.pending[handle]; ok {
		winOptRelease(old.id)
	}
	state.pending[handle] = winOptWarmSocket{id: id, bytes: bytes}
	state.Unlock()
	winOptLog("preconnect target=%s bytes=%d", address, bytes)
}
func winOptDialFailed(state *winOptConnectState) {
	state.Lock()
	defer state.Unlock()
	for _, p := range state.pending {
		winOptRelease(p.id)
	}
	state.pending = nil
	state.closed = true
}
func winOptClaimWarm(conn *net.TCPConn, state *winOptConnectState) winOptWarmSocket {
	var handle uintptr
	raw, err := conn.SyscallConn()
	if err == nil {
		_ = raw.Control(func(fd uintptr) { handle = fd })
	}
	state.Lock()
	defer state.Unlock()
	winner := state.pending[handle]
	for fd, p := range state.pending {
		if fd != handle {
			winOptRelease(p.id)
		}
	}
	state.pending = nil
	state.closed = true
	return winner
}

// We don't wrap *net.TCPConn: Xray's Windows ReadV / direct-copy interfaces
// must remain intact. Consequently BytesIn is the closest available proxy
// for Linux's application-consumed data, and not an exact equivalent.
var winOptActiveWatchers atomic.Int32

func winOptAfterConnect(conn net.Conn, network, address string, explicit bool, state *winOptConnectState) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok || explicit || !winOptEnabled(network) {
		winOptDialFailed(state)
		return
	}
	warm := winOptClaimWarm(tcp, state)
	releaseWarm := func() { winOptRelease(warm.id) }
	remote, ok := tcp.RemoteAddr().(*net.TCPAddr)
	if !ok || remote.IP == nil {
		releaseWarm()
		return
	}
	addr, ok := netip.AddrFromSlice(remote.IP)
	if !ok || !addr.IsValid() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		releaseWarm()
		return
	}
	if winOptActiveWatchers.Add(1) > 256 {
		winOptActiveWatchers.Add(-1)
		releaseWarm()
		return
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		winOptActiveWatchers.Add(-1)
		releaseWarm()
		return
	}
	peer := tcp.RemoteAddr().String()
	go func() {
		defer winOptActiveWatchers.Add(-1)
		id := warm.id
		defer func() { winOptRelease(id) }()
		ctrl := winOptNewController(winOptLimit())
		// Linux tcp_metrics keeps path observations apart from per-socket
		// receive-buffer growth. Reuse only an RTT hint, after connect.
		hint := winOptRTTHistory.lookup(address, time.Now())
		if hint == 0 {
			hint = winOptRTTHistory.lookup(peer, time.Now())
		}
		if hint > 0 {
			ctrl.SetRTTHint(hint)
			winOptLog("rtt_hint target=%s rtt_ms=%d", address, hint.Milliseconds())
		}
		initialized := false
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for now := range ticker.C {
			info, err := winOptInfo(raw)
			if err != nil {
				return
			} // socket closed or no TCP_INFO available
			if info.RcvBuf == 0 {
				continue
			}
			// Keep path history even when this connection never needs a
			// larger receive buffer. No new packet is sent by this lookup.
			if info.RttUs > 0 {
				observed := time.Duration(info.RttUs) * time.Microsecond
				winOptRTTHistory.remember(address, observed, now)
				winOptRTTHistory.remember(peer, observed, now)
			}
			if !initialized {
				initialized = true
				// Only treat warm socket as applied if Windows confirms size.
				if warm.bytes > 0 && info.RcvBuf >= warm.bytes/2 {
					ctrl.Applied(warm.bytes, now)
				}
			}
			s := winOptSample{At: now, RTT: time.Duration(info.RttUs) * time.Microsecond,
				BytesIn: info.BytesIn, RcvBuf: info.RcvBuf, RcvWnd: info.RcvWnd, BytesReordered: info.BytesReordered}
			proposal := ctrl.Observe(s)
			if proposal == 0 {
				continue
			}
			previous := winOptReserved(id)
			if !winOptReserve(&id, proposal) {
				winOptLog("growth deferred budget/pressure %s target=%d", address, proposal)
				continue
			}
			if err := tcp.SetReadBuffer(int(proposal)); err != nil {
				// Roll back reservation to previous size.
				if previous > 0 {
					_ = winOptReserve(&id, previous)
				} else {
					winOptRelease(id)
					id = 0
				}
				winOptLog("setbuf failed %s: %v", address, err)
				return
			}
			ctrl.Applied(proposal, now)
			// Legacy capacity cache exists only for explicit performance A/B.
			// Default history never saves or reapplies SO_RCVBUF across flows.
			if winOptPreconnectCapacityEnabled(os.Getenv("XRAY_WINOPT_PRECONNECT_CACHE")) {
				winOptRemember(address, proposal)
				winOptRemember(peer, proposal)
			}
			winOptLog("expanded %s rtt_ms=%d rcvbuf=%d rcvwnd=%d bytes=%d set=%d budget=%d", address, info.RttUs/1000, info.RcvBuf, info.RcvWnd, info.BytesIn, proposal, winOptBudgetLimit())
		}
	}()
}
