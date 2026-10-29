//go:build !windows

package portable

import (
	"net"
	"syscall"
)

type winOptConnectState struct{}

func winOptEnabled(network string) bool                                                           { return false }
func winOptApplyTCPOpts(raw syscall.RawConn, network, address string)                             {}
func winOptBeforeConnect(raw syscall.RawConn, network, address string, state *winOptConnectState) {}
func winOptAfterConnect(conn net.Conn, network, address string, explicit bool, state *winOptConnectState) {
}
func winOptDialFailed(state *winOptConnectState) {}
