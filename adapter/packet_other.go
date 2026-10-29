//go:build !windows

package adapter

import "errors"

func New() *PacketAdapter { return &PacketAdapter{} }

type PacketAdapter struct{}

func (p *PacketAdapter) Start() error                      { return errors.New("WinDivert adapter requires Windows") }
func (p *PacketAdapter) Close() error                      { return nil }
func (p *PacketAdapter) Name() (string, error)             { return "", errors.New("Windows only") }
func (p *PacketAdapter) Index() (int, error)               { return 0, errors.New("Windows only") }
func (p *PacketAdapter) ReadPacket([]byte) ([]byte, error) { return nil, errors.New("Windows only") }
func (p *PacketAdapter) WritePacket([]byte, []byte) error  { return errors.New("Windows only") }
