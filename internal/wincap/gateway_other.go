//go:build !windows

// Package wincap is a Windows-only device-only capture gateway (WinDivert).
// On other platforms it is a no-op stub so the manager can reference it without
// build tags; Linux uses policy routing instead (see internal/tungw).
package wincap

import "fmt"

type Options struct {
	DevSubnet  string
	DevIfIndex uint32
	Uplink     string
	Proxy      string
	Log        func(string, ...any)
}

type Gateway struct{}

func New() *Gateway { return &Gateway{} }

// Supported reports whether this platform has the device-only capture path.
func Supported() bool { return false }

func (g *Gateway) Start(Options) error {
	return fmt.Errorf("device-only capture is Windows-only")
}

func (g *Gateway) Stop() error { return nil }
