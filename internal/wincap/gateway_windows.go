//go:build windows

// Package wincap implements Windows device-only routing: instead of hijacking
// the host default route, it captures ONLY forwarded packets (the downstream
// device's traffic) via WinDivert's network-forward layer and feeds them to a
// userspace TCP/IP stack (tun2socks/gVisor) that dials out direct or via a
// proxy. The host's own traffic never touches the forward layer, so it is left
// completely untouched -- the Windows equivalent of Linux policy routing.
package wincap

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strings"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device/iobased"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

const mtu = 1500

// Options configures the Windows device-only gateway.
type Options struct {
	DevSubnet  string // downstream subnet CIDR, e.g. "192.168.88.0/24"
	DevIfIndex uint32 // physical NIC index facing the device (reply injection)
	Uplink     string // uplink NIC name (unused here; host routing handles egress)
	Proxy      string // "" = direct; socks5://host:port ; http://host:port
	Log        func(string, ...any)
}

// Gateway owns the WinDivert handle + gVisor stack lifecycle.
type Gateway struct {
	h       *Handle
	stack   *stack.Stack
	ep      *iobased.Endpoint
	started bool
}

func New() *Gateway { return &Gateway{} }

// Supported reports whether this platform has the device-only capture path.
func Supported() bool { return true }

func (g *Gateway) Start(o Options) error {
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	lo, hi, err := subnetRange(o.DevSubnet)
	if err != nil {
		return err
	}
	px, err := buildProxy(o.Proxy)
	if err != nil {
		return err
	}

	// Enable runtime IP forwarding so the device's packets reach the forward
	// layer (no reboot / RemoteAccess needed with per-interface flags + global).
	_ = run("netsh", "interface", "ipv4", "set", "global", "forwarding=enabled")
	_ = run("netsh", "interface", "ipv4", "set", "interface", fmt.Sprint(o.DevIfIndex), "forwarding=enabled")

	filter := fmt.Sprintf("ip and ip.SrcAddr >= %s and ip.SrcAddr <= %s", lo, hi)
	h, err := openForward(filter, o.DevIfIndex)
	if err != nil {
		return err
	}

	ep, err := iobased.New(h, mtu, 0)
	if err != nil {
		_ = h.Close()
		return fmt.Errorf("iobased endpoint: %w", err)
	}

	tunnel.T().SetDialer(px)
	st, err := core.CreateStack(&core.Config{
		LinkEndpoint:     ep,
		TransportHandler: tunnel.T(),
	})
	if err != nil {
		ep.Close()
		_ = h.Close()
		return fmt.Errorf("create stack: %w", err)
	}

	g.h, g.ep, g.stack, g.started = h, ep, st, true
	out := "direct"
	if o.Proxy != "" {
		out = o.Proxy
	}
	o.Log("device-only capture up (WinDivert forward), %s -> %s -> internet", o.DevSubnet, out)
	o.Log("host traffic untouched; only forwarded device packets are proxied")
	return nil
}

func (g *Gateway) Stop() error {
	if !g.started {
		return nil
	}
	g.started = false
	if g.h != nil {
		_ = g.h.Close()
	}
	if g.ep != nil {
		g.ep.Close()
	}
	if g.stack != nil {
		g.stack.Close()
	}
	return nil
}

// subnetRange returns the first and last IPv4 address of a CIDR as strings.
func subnetRange(cidr string) (lo, hi string, err error) {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", fmt.Errorf("bad DevSubnet %q: %w", cidr, err)
	}
	ip4 := n.IP.To4()
	if ip4 == nil {
		return "", "", fmt.Errorf("DevSubnet %q is not IPv4", cidr)
	}
	base := binary.BigEndian.Uint32(ip4)
	mask := binary.BigEndian.Uint32(net.IP(n.Mask).To4())
	last := base | ^mask
	return u32IP(base), u32IP(last), nil
}

func u32IP(v uint32) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return net.IP(b[:]).String()
}

func buildProxy(p string) (proxy.Proxy, error) {
	if p == "" || strings.HasPrefix(p, "direct") {
		return proxy.NewDirect(), nil
	}
	u, err := url.Parse(p)
	if err != nil {
		return nil, fmt.Errorf("bad proxy %q: %w", p, err)
	}
	user, pass := "", ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	switch {
	case strings.HasPrefix(p, "socks5://") || strings.HasPrefix(p, "socks://"):
		return proxy.NewSocks5(u.Host, user, pass)
	case strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://"):
		return proxy.NewHTTP(u.Host, user, pass)
	}
	return nil, fmt.Errorf("unsupported proxy scheme (use socks5:// or http://)")
}

func run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
