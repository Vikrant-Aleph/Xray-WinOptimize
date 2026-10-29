//go:build windows

package tun

import (
	"context"
	"fmt"
	gonet "net"
	"net/netip"
	"os"
	"sync"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
)

// This registration is deliberately opt-in. Inbounds remain SOCKS/HTTP etc.
// Only outbound TCP dials through the default system dialer are candidates.
func init() {
	if os.Getenv("XRAY_EXPERIMENTAL_TCP_STACK") != "1" {
		return
	}
	internet.UseAlternativeSystemDialer(&experimentalTCPDialer{
		stacks: make(map[netip.Addr]*experimentalStack),
	})
}

type experimentalStack struct {
	tun   *PacketAdapter
	stack Stack
}

type experimentalTCPDialer struct {
	mu     sync.Mutex
	stacks map[netip.Addr]*experimentalStack
	native internet.DefaultSystemDialer
}

func (*experimentalTCPDialer) DestIpAddress() xnet.IP { return nil }
func (d *experimentalTCPDialer) Dial(ctx context.Context, src xnet.Address, destination xnet.Destination, sockopt *internet.SocketConfig) (xnet.Conn, error) {
	if destination.Network != xnet.Network_TCP {
		return d.native.Dial(ctx, src, destination, sockopt)
	}
	host, port, err := gonet.SplitHostPort(destination.NetAddr())
	if err != nil {
		return nil, err
	}
	ips, err := gonet.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	var remote netip.Addr
	for _, ip := range ips {
		if ip.IsValid() && ip.IsGlobalUnicast() {
			remote = ip.Unmap()
			break
		}
	}
	if !remote.IsValid() {
		return d.native.Dial(ctx, src, destination, sockopt)
	} // loopback/local endpoints stay native
	// Current prototype does not implement source/interface socket options.
	if src != nil && src != xnet.AnyIP {
		return nil, fmt.Errorf("experimental TCP: explicit source binding is not supported yet")
	}
	if sockopt != nil && (sockopt.TcpMptcp || sockopt.TcpKeepAliveIdle != 0 || sockopt.TcpKeepAliveInterval != 0 || len(sockopt.CustomSockopt) > 0) {
		return nil, fmt.Errorf("experimental TCP: unsupported outbound socket options; remove them or disable experiment")
	}
	destPort, err := gonet.LookupPort("tcp", port)
	if err != nil {
		return nil, err
	}
	chosen, err := routeSource(remote, uint16(destPort))
	if err != nil {
		return nil, err
	}
	st, err := d.getStack(chosen)
	if err != nil {
		return nil, err
	}
	conn, err := st.stack.DialTCP(ctx, netip.AddrPortFrom(remote, uint16(destPort)))
	if err != nil {
		return nil, fmt.Errorf("experimental TCP dial %s: %w", destination, err)
	}
	return conn, nil
}
func routeSource(remote netip.Addr, port uint16) (netip.Addr, error) {
	network := "udp4"
	if remote.Is6() {
		network = "udp6"
	}
	c, err := gonet.DialUDP(network, nil, gonet.UDPAddrFromAddrPort(netip.AddrPortFrom(remote, port)))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve Windows outbound source IP: %w", err)
	}
	defer c.Close()
	return c.LocalAddr().(*gonet.UDPAddr).AddrPort().Addr().Unmap(), nil
}
func (d *experimentalTCPDialer) getStack(src netip.Addr) (*experimentalStack, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if existing := d.stacks[src]; existing != nil {
		return existing, nil
	}
	a := NewWinDivertPacketAdapter()
	if err := a.Start(); err != nil {
		return nil, err
	}
	mtu := uint32(1400)
	if src.Is6() {
		mtu = 1280
	}
	st, err := NewStack(context.Background(), StackOptions{
		Tun: a, MTU: mtu, Addresses: []netip.Addr{src}, Remote: true, AdaptiveReceive: true,
	}, nil)
	if err != nil {
		a.Close()
		return nil, err
	}
	if err = st.Start(); err != nil {
		st.Close()
		a.Close()
		return nil, err
	}
	entry := &experimentalStack{tun: a, stack: st}
	d.stacks[src] = entry
	return entry, nil
}
