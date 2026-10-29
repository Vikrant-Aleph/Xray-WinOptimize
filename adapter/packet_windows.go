//go:build windows

// Package adapter is a WinDivert-backed packet transport for Xray's user-mode TCP stack.
// Experimental: it is not intended to capture operating-system TCP connections.
// It captures replies only for exact flow tuples opened by this process.
package adapter

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const maxFlows = 128
const packetCapacity = 65536

var (
	divertDLL = syscall.NewLazyDLL("WinDivert.dll")
	openFn    = divertDLL.NewProc("WinDivertOpen")
	recvFn    = divertDLL.NewProc("WinDivertRecv")
	sendFn    = divertDLL.NewProc("WinDivertSend")
	closeFn   = divertDLL.NewProc("WinDivertClose")
)

type tuple struct{ src, dst netip.AddrPort }
type flowHandle struct {
	handle      syscall.Handle
	reservation syscall.Handle
	last        time.Time
	closing     bool
}

type PacketAdapter struct {
	mu       sync.Mutex
	flows    map[tuple]*flowHandle
	received chan []byte
	done     chan struct{}
	closed   bool
}

// New creates no driver or routing rule. The WinDivert handle is opened only
// for an actual outgoing TCP flow, before its first SYN is injected.
func New() *PacketAdapter {
	return &PacketAdapter{
		flows:    make(map[tuple]*flowHandle),
		received: make(chan []byte, 2048),
		done:     make(chan struct{}),
	}
}

func (p *PacketAdapter) Name() (string, error) { return "experimental-windivert-tcp", nil }
func (p *PacketAdapter) Index() (int, error)   { return 0, nil }
func (p *PacketAdapter) Start() error {
	if err := divertDLL.Load(); err != nil {
		return fmt.Errorf("load WinDivert.dll: %w (place DLL + matching signed driver beside xray.exe)", err)
	}
	go p.reap()
	return nil
}
func (p *PacketAdapter) ReadPacket(_ []byte) ([]byte, error) {
	select {
	case b := <-p.received:
		return b, nil
	case <-p.done:
		return nil, io.EOF
	}
}
func (p *PacketAdapter) WritePacket(header, payload []byte) error {
	packet := make([]byte, 0, len(header)+len(payload))
	packet = append(packet, header...)
	packet = append(packet, payload...)
	key, flags, err := decodeTCP(packet)
	if err != nil {
		return err
	}
	handle, err := p.flowFor(key)
	if err != nil {
		return err
	}
	// Mark after last data/ACK; don't release immediately: peer may retransmit.
	if flags&0x05 != 0 {
		p.mu.Lock()
		if h := p.flows[key]; h != nil {
			h.closing = true
			h.last = time.Now()
		}
		p.mu.Unlock()
	}
	var addr [80]byte // WINDIVERT_ADDRESS (v2.2, 80 bytes, network layer)
	// bitfields at offset 8: Outbound bit 17. Network layer=0, NETWORK_PACKET event=0.
	binary.LittleEndian.PutUint32(addr[8:], 1<<17)
	var sent uint32
	r, _, e := sendFn.Call(uintptr(handle), uintptr(unsafe.Pointer(&packet[0])), uintptr(len(packet)), uintptr(unsafe.Pointer(&sent)), uintptr(unsafe.Pointer(&addr[0])))
	runtime.KeepAlive(packet)
	runtime.KeepAlive(addr)
	if r == 0 {
		return fmt.Errorf("WinDivertSend: %w", e)
	}
	if int(sent) != len(packet) {
		return io.ErrShortWrite
	}
	return nil
}

// Bind a TCP port WITHOUT listening, to prevent Winsock choosing the same
// source IP+port for an unrelated native TCP flow during this user-mode flow.
func reservePort(src netip.AddrPort) (syscall.Handle, error) {
	family := syscall.AF_INET
	if src.Addr().Is6() {
		family = syscall.AF_INET6
	}
	s, err := syscall.Socket(family, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return 0, err
	}
	var sa syscall.Sockaddr
	if family == syscall.AF_INET {
		sa = &syscall.SockaddrInet4{Port: int(src.Port()), Addr: src.Addr().As4()}
	} else {
		sa = &syscall.SockaddrInet6{Port: int(src.Port()), Addr: src.Addr().As16()}
	}
	if err = syscall.Bind(s, sa); err != nil {
		syscall.Closesocket(s)
		return 0, err
	}
	return s, nil
}
func (p *PacketAdapter) flowFor(key tuple) (syscall.Handle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	if h := p.flows[key]; h != nil {
		h.last = time.Now()
		return h.handle, nil
	}
	if len(p.flows) >= maxFlows {
		return 0, fmt.Errorf("WinDivert flow limit %d reached", maxFlows)
	}
	reservation, err := reservePort(key.src)
	if err != nil {
		return 0, fmt.Errorf("reserve source TCP port %s: %w", key.src, err)
	}
	proto := "ip"
	if key.src.Addr().Is6() {
		proto = "ipv6"
	}
	// Filter is EXACT reverse TCP tuple. Never steal unrelated inbound TCP.
	filter := fmt.Sprintf("inbound and %s and tcp and %s.SrcAddr == %s and %s.DstAddr == %s and tcp.SrcPort == %d and tcp.DstPort == %d", proto, proto, key.dst.Addr(), proto, key.src.Addr(), key.dst.Port(), key.src.Port())
	bytes := append([]byte(filter), 0)
	h, _, e := openFn.Call(uintptr(unsafe.Pointer(&bytes[0])), 0, 0, 0)
	runtime.KeepAlive(bytes)
	if h == 0 || h == ^uintptr(0) {
		syscall.Closesocket(reservation)
		return 0, fmt.Errorf("WinDivertOpen(%s): %w", filter, e)
	}
	fh := &flowHandle{handle: syscall.Handle(h), reservation: reservation, last: time.Now()}
	p.flows[key] = fh
	go p.capture(key, fh)
	return fh.handle, nil
}
func (p *PacketAdapter) capture(key tuple, fh *flowHandle) {
	defer func() {
		p.mu.Lock()
		if p.flows[key] == fh {
			delete(p.flows, key)
		}
		p.mu.Unlock()
		closeFn.Call(uintptr(fh.handle))
		syscall.Closesocket(fh.reservation)
	}()
	for {
		buf := make([]byte, packetCapacity)
		var addr [80]byte
		var n uint32
		r, _, _ := recvFn.Call(uintptr(fh.handle), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&addr[0])))
		runtime.KeepAlive(buf)
		if r == 0 {
			return
		}
		if n == 0 || n > packetCapacity {
			continue
		}
		p.mu.Lock()
		if p.flows[key] == fh {
			fh.last = time.Now()
		}
		p.mu.Unlock()
		select {
		case p.received <- buf[:n]:
		case <-p.done:
			return
		default: // bounded queue: TCP retransmission handles dropped packets
		}
	}
}
func (p *PacketAdapter) reap() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now()
			for _, fh := range p.flows {
				idle := now.Sub(fh.last)
				if idle > 5*time.Minute || (fh.closing && idle > 45*time.Second) {
					closeFn.Call(uintptr(fh.handle))
				}
			}
			p.mu.Unlock()
		}
	}
}
func (p *PacketAdapter) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.done)
	for _, fh := range p.flows {
		closeFn.Call(uintptr(fh.handle))
	}
	p.mu.Unlock()
	return nil
}

// decodeTCP recognizes *nonfragmented* IPv4/IPv6 TCP packets and the two
// endpoint addresses. IPv6 extension headers and IP fragmentation are not yet
// implemented; we fail closed rather than misroute them.
func decodeTCP(b []byte) (tuple, byte, error) {
	var key tuple
	if len(b) < 40 {
		return key, 0, errors.New("short IP packet")
	}
	offset := 0
	var src, dst netip.Addr
	switch b[0] >> 4 {
	case 4:
		if len(b) < 20 {
			return key, 0, errors.New("short IPv4")
		}
		offset = int(b[0]&15) * 4
		if offset < 20 || offset+20 > len(b) || b[9] != 6 {
			return key, 0, errors.New("not an IPv4 TCP packet")
		}
		if binary.BigEndian.Uint16(b[6:8])&0x3fff != 0 {
			return key, 0, errors.New("IPv4 fragments unsupported")
		}
		src = netip.AddrFrom4([4]byte(b[12:16]))
		dst = netip.AddrFrom4([4]byte(b[16:20]))
	case 6:
		offset = 40
		if b[6] != 6 || len(b) < 60 {
			return key, 0, errors.New("IPv6 extension headers unsupported")
		}
		src = netip.AddrFrom16([16]byte(b[8:24]))
		dst = netip.AddrFrom16([16]byte(b[24:40]))
	default:
		return key, 0, errors.New("unsupported IP version")
	}
	key = tuple{src: netip.AddrPortFrom(src, binary.BigEndian.Uint16(b[offset:])), dst: netip.AddrPortFrom(dst, binary.BigEndian.Uint16(b[offset+2:]))}
	return key, b[offset+13], nil
}
