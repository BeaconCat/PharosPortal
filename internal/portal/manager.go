// Package portal 实现: 接管指定物理网卡, 为直连的下游设备分配 IP, 并 NAT/ICS 接入内网/外网。
package portal

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/BeaconCat/PharosPortal/internal/tungw"
)

// Config 一次运行的配置。
type Config struct {
	Iface      string `json:"iface"`
	Uplink     string `json:"uplink"`
	ServerIP   string `json:"serverIP"`
	Mask       string `json:"mask"`
	RangeStart string `json:"rangeStart"`
	RangeEnd   string `json:"rangeEnd"`
	DNS        string `json:"dns"`
	LeaseMin   int    `json:"leaseMin"`
	SetIP      bool     `json:"setIP"`
	TUN        bool     `json:"tun"`   // TUN 网关: 给设备上网 (用户态 NAT)
	Proxy      string   `json:"proxy"` // TUN 出站: 空=direct(经主机); 或 socks5://host:port / http://host:port
	Allow      []string `json:"allow"` // MAC 白名单 (非空则只服务名单内设备)
}

// DefaultConfig 返回默认参数。
func DefaultConfig() Config {
	return Config{
		ServerIP: "192.168.88.1", Mask: "255.255.255.0",
		RangeStart: "192.168.88.50", RangeEnd: "192.168.88.150",
		DNS: "223.5.5.5", LeaseMin: 720, SetIP: true, TUN: true,
	}
}

type leaseInfo struct {
	MAC    string    `json:"mac"`
	IP     string    `json:"ip"`
	Seen   time.Time `json:"seen"`
	Ack    bool      `json:"ack"`
	logged bool      `json:"-"`
}

// Manager 管理一次网卡接管的生命周期 (DHCP / NAT / ICS)。
type Manager struct {
	mu       sync.Mutex
	running  bool
	cfg      Config
	cancel   context.CancelFunc
	leases   map[string]*leaseInfo
	used     map[string]bool
	lo, hi   uint32
	serverIP net.IP
	mask     net.IP
	mode     string // "dhcp" | "tun"
	gw       *tungw.Gateway
	allow    map[string]bool // 规范化 MAC 白名单
	logs     []string
}

func NewManager() *Manager {
	return &Manager{leases: map[string]*leaseInfo{}, used: map[string]bool{}}
}

func (m *Manager) logf(f string, a ...any) {
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(f, a...)
	m.mu.Lock()
	m.logs = append(m.logs, line)
	if len(m.logs) > 200 {
		m.logs = m.logs[len(m.logs)-200:]
	}
	m.mu.Unlock()
	fmt.Println(line)
}

func (m *Manager) Start(cfg Config) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("already running")
	}
	m.mu.Unlock()

	iface, err := net.InterfaceByName(cfg.Iface)
	if err != nil {
		return fmt.Errorf("interface %q not found: %v", cfg.Iface, err)
	}
	serverIP := net.ParseIP(cfg.ServerIP).To4()
	mask := net.ParseIP(cfg.Mask).To4()
	lo := ip2u(net.ParseIP(cfg.RangeStart).To4())
	hi := ip2u(net.ParseIP(cfg.RangeEnd).To4())
	if serverIP == nil || mask == nil || lo == 0 || hi == 0 || lo > hi || net.ParseIP(cfg.DNS).To4() == nil {
		return fmt.Errorf("invalid IP/mask/range/DNS")
	}
	ones, _ := net.IPMask(mask).Size()

	m.mu.Lock()
	m.cfg, m.serverIP, m.mask, m.lo, m.hi = cfg, serverIP, mask, lo, hi
	m.leases, m.used = map[string]*leaseInfo{}, map[string]bool{}
	m.allow = map[string]bool{}
	for _, a := range cfg.Allow {
		if a = normalizeMAC(a); len(a) == 17 {
			m.allow[a] = true
		}
	}
	m.mode = "dhcp"
	nAllow := len(m.allow)
	m.mu.Unlock()
	if nAllow > 0 {
		m.logf("MAC allowlist active: %d device(s)", nAllow)
	}

	// TUN 网关模式: 保留内建 DHCP (设备拿 192.168.88.x), 另起 TUN 用户态 NAT 让设备上网。
	// 跨平台可靠, 绕开 WinNAT/ICS; Proxy 非空则下游走主机代理。
	if cfg.TUN && cfg.Uplink != "" {
		if cfg.SetIP {
			m.logf("setting %s -> %s/%d", cfg.Iface, serverIP, ones)
			if err := setStaticIP(cfg.Iface, serverIP, mask, ones); err != nil {
				return fmt.Errorf("set interface IP failed: %v", err)
			}
		}
		gw := tungw.New()
		if err := gw.Start(tungw.Options{
			TunName: "pptun0", TunAddr: "198.18.0.1", TunCIDR: 15,
			DevSubnet: subnetCIDR(serverIP, ones), Uplink: cfg.Uplink, Proxy: cfg.Proxy,
			Log: m.logf,
		}); err != nil {
			m.logf("[!] TUN gateway failed (device will get IP but no internet): %v", err)
		} else {
			m.mu.Lock()
			m.gw, m.mode = gw, "tun"
			m.mu.Unlock()
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.mu.Lock()
		m.cancel, m.running = cancel, true
		m.mu.Unlock()
		go func() {
			m.logf("DHCP serving on %s only (ifindex=%d). Waiting for device ...", cfg.Iface, iface.Index)
			if err := m.serveDHCP(ctx, iface); err != nil && ctx.Err() == nil {
				m.logf("[x] DHCP error: %v", err)
			}
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
		}()
		return nil
	}

	// 仅 DHCP (无 -tun 或未选上行网卡): 设备拿 IP + 本机可访问, 但不上网。
	if cfg.SetIP {
		m.logf("setting %s -> %s/%d", cfg.Iface, serverIP, ones)
		if err := setStaticIP(cfg.Iface, serverIP, mask, ones); err != nil {
			return fmt.Errorf("set interface IP failed: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel, m.running = cancel, true
	m.mu.Unlock()

	go func() {
		m.logf("DHCP serving on %s only (ifindex=%d). Waiting for device ...", cfg.Iface, iface.Index)
		if err := m.serveDHCP(ctx, iface); err != nil && ctx.Err() == nil {
			m.logf("[x] DHCP error: %v", err)
		}
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	cancel, cfg, gw := m.cancel, m.cfg, m.gw
	m.mu.Unlock()
	m.logf("stopping, cleaning up ...")
	if cancel != nil {
		cancel()
	}
	if gw != nil {
		_ = gw.Stop()
		m.mu.Lock()
		m.gw = nil
		m.mu.Unlock()
	}
	if cfg.SetIP {
		_ = unsetStaticIP(cfg.Iface)
	}
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
}

// Status 当前状态快照 (供 UI)。
type Status struct {
	Running bool        `json:"running"`
	Admin   bool        `json:"admin"`
	Mode    string      `json:"mode"`
	Cfg     Config      `json:"cfg"`
	Leases  []leaseInfo `json:"leases"`
	Traffic Traffic     `json:"traffic"`
	Logs    []string    `json:"logs"`
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	st := Status{Running: m.running, Admin: IsAdmin(), Mode: m.mode, Cfg: m.cfg, Logs: append([]string{}, m.logs...)}
	for _, l := range m.leases {
		st.Leases = append(st.Leases, *l)
	}
	tun := m.mode == "tun" && m.running
	m.mu.Unlock()
	sort.Slice(st.Leases, func(i, j int) bool { return st.Leases[i].IP < st.Leases[j].IP })
	if tun {
		st.Traffic = m.trafficStats()
	}
	return st
}
