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
	Log       func(string, ...any)
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
	proxy := o.Proxy
	if proxy == "" {
		proxy = "direct://"
	}
	key := &engine.Key{
		Device:    "tun://" + o.TunName,
		Proxy:     proxy,
		Interface: o.Uplink, // 出站绑定上行网卡 -> 引擎自身流量不再被 tun 默认路由抓走
		LogLevel:  "warn",
		UDPTimeout: 0,
	}
	engine.Insert(key)
	engine.Start()
	g.opts = o
	g.started = true
	o.Log("TUN engine up (%s), outbound=%s via %s", o.TunName, proxy, orDirect(o.Uplink))

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
