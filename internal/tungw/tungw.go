// Package tungw 用 TUN + 用户态 TCP/IP 栈 (tun2socks/gVisor) 给下游设备做 NAT,
// 绕开 WinNAT/ICS。出站可 direct (经主机上行) 或走代理 (socks5/http), 即"下游挂主机代理"。
package tungw

import (
	"fmt"
	"runtime"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

type Options struct {
	TunName   string // e.g. "pptun0"
	TunAddr   string // e.g. "198.18.0.1"
	TunCIDR   int    // e.g. 15
	DevSubnet string // 下游设备网段 CIDR, e.g. "192.168.88.0/24"
	Uplink    string // 上行网卡名 (出站绑定, 防环回)
	Proxy     string // "" = direct; 或 socks5://host:port , http://host:port
	// WholeSystem 仅 Windows 有意义: Windows 路由表无源路由, 无法只导下游网段。
	// true 时把整机默认路由改走 tun (整机代理, 会接管主机自身流量); false 时不动主机路由
	// (下游设备在 Windows 上暂无法上网, 需整机模式或改用 Linux 策略路由)。Linux 始终按网段分流。
	WholeSystem bool
	Log         func(string, ...any)
}

type Gateway struct {
	opts    Options
	started bool
}

func New() *Gateway { return &Gateway{} }

func (g *Gateway) Start(o Options) error {
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	if err := ensureWintun(); err != nil { // Windows: 释放内嵌 wintun.dll; 其它平台 no-op
		return fmt.Errorf("prepare wintun.dll: %w", err)
	}
	proxy := o.Proxy
	if proxy == "" {
		proxy = "direct://"
	}
	key := &engine.Key{
		Device:     "tun://" + o.TunName,
		Proxy:      proxy,
		LogLevel:   "warn",
		UDPTimeout: 0,
	}
	// 仅 direct 模式绑定上行网卡 (Windows 整机 tun 防环回; Linux 策略路由下无害)。
	// 有代理时【不能】绑网卡 —— 会破坏对代理服务器的拨号 (尤其 127.0.0.1 本地代理)。
	via := "proxy"
	if o.Proxy == "" {
		key.Interface = o.Uplink
		via = orDirect(o.Uplink)
	}
	engine.Insert(key)
	engine.Start()
	g.opts = o
	g.started = true
	o.Log("TUN engine up (%s), outbound=%s via %s", o.TunName, proxy, via)

	if err := g.route(true); err != nil {
		_ = g.Stop()
		return fmt.Errorf("routing setup failed: %w", err)
	}
	o.Log("routing ready: device %s -> %s -> internet", o.DevSubnet, o.TunName)
	return nil
}

func (g *Gateway) Stop() error {
	if !g.started {
		return nil
	}
	_ = g.route(false)
	engine.Stop()
	g.started = false
	return nil
}

func orDirect(s string) string {
	if s == "" {
		return "default route"
	}
	return s
}

func runq(name string, args ...string) error { return execRun(name, args...) }

// route 配置/撤销把下游流量导入 TUN 的路由。
func (g *Gateway) route(add bool) error {
	if runtime.GOOS == "windows" {
		return g.routeWindows(add)
	}
	return g.routeLinux(add)
}
