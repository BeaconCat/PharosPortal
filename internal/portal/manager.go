// Package portal 实现: 接管指定物理网卡, 为直连的下游设备分配 IP, 并 NAT/ICS 接入内网/外网。
package portal

import (
	"context"
	"fmt"
	"net"
	"runtime"
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
	NAT        bool   `json:"nat"`
	SetIP      bool   `json:"setIP"`
	TUN        bool   `json:"tun"`   // TUN 网关模式 (用户态 NAT, 绕开 WinNAT/ICS)
	Proxy      string `json:"proxy"` // TUN 出站: 空=direct; 或 socks5://host:port / http://host:port
}

// DefaultConfig 返回默认参数。
func DefaultConfig() Config {
	return Config{
		ServerIP: "192.168.88.1", Mask: "255.255.255.0",
		RangeStart: "192.168.88.50", RangeEnd: "192.168.88.150",
		DNS: "223.5.5.5", LeaseMin: 720, NAT: true, SetIP: true,
	}
}

type leaseInfo struct {
	MAC  string    `json:"mac"`
	IP   string    `json:"ip"`
	Seen time.Time `json:"seen"`
	Ack  bool      `json:"ack"`
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
	natOn    bool
	mode     string // "dhcp" | "ics" | "tun"
	gw       *tungw.Gateway
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
	m.natOn, m.mode = false, "dhcp"
	m.mu.Unlock()

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

	// Windows + NAT: New-NetNat 的 WMI provider 在很多机器上会 0x80041013, 改用系统 ICS。
	// ICS 自带 DHCP+NAT, 固定 192.168.137.x。
	if runtime.GOOS == "windows" && cfg.NAT && cfg.Uplink != "" {
		m.logf("Windows NAT via ICS: sharing %s -> %s (device gets 192.168.137.x + internet)", cfg.Uplink, cfg.Iface)
		if err := enableICS(cfg.Uplink, cfg.Iface); err != nil {
			m.logf("[!] ICS failed, falling back to DHCP-only (no NAT, host access still ok): %v", err)
		} else {
			ctx, cancel := context.WithCancel(context.Background())
			m.mu.Lock()
			m.cancel, m.running, m.mode = cancel, true, "ics"
			m.mu.Unlock()
			m.logf("ICS enabled. Discovering device via ARP (192.168.137.x) ...")
			go m.arpLoop(ctx)
			return nil
		}
	}

	if cfg.SetIP {
		m.logf("setting %s -> %s/%d", cfg.Iface, serverIP, ones)
		if err := setStaticIP(cfg.Iface, serverIP, mask, ones); err != nil {
			return fmt.Errorf("set interface IP failed: %v", err)
		}
	}
	if cfg.NAT && cfg.Uplink != "" && runtime.GOOS != "windows" {
		m.logf("enabling NAT: %s -> %s", subnetCIDR(serverIP, ones), cfg.Uplink)
		if err := enableNAT(subnetCIDR(serverIP, ones), cfg.Uplink); err != nil {
			m.logf("[!] NAT failed (host access still ok, device just won't reach internet): %v", err)
		} else {
			m.mu.Lock()
			m.natOn = true
			m.mu.Unlock()
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
	cancel, cfg, serverIP, mask, natOn, mode, gw := m.cancel, m.cfg, m.serverIP, m.mask, m.natOn, m.mode, m.gw
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
	if mode == "tun" && cfg.SetIP {
		_ = unsetStaticIP(cfg.Iface)
	}
	if mode == "ics" {
		if err := disableICS(cfg.Uplink, cfg.Iface); err != nil {
			m.logf("[!] disable ICS failed (undo manually in Network Connections): %v", err)
		}
	} else {
		ones, _ := net.IPMask(mask.To4()).Size()
		if natOn {
			_ = disableNAT(subnetCIDR(serverIP, ones), cfg.Uplink)
		}
		if cfg.SetIP {
			_ = unsetStaticIP(cfg.Iface)
		}
	}
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
}

// arpLoop: ICS 模式下轮询系统 ARP 表, 显示 192.168.137.x 的下游设备。
func (m *Manager) arpLoop(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		for _, e := range readARP("192.168.137.") {
			m.mu.Lock()
			if _, ok := m.leases[e[1]]; !ok {
				m.logf(">> device  MAC=%s  IP=%s  (ICS)", e[1], e[0])
			}
			m.leases[e[1]] = &leaseInfo{MAC: e[1], IP: e[0], Seen: time.Now(), Ack: true}
			m.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Status 当前状态快照 (供 UI)。
type Status struct {
	Running bool        `json:"running"`
	Admin   bool        `json:"admin"`
	Cfg     Config      `json:"cfg"`
	Leases  []leaseInfo `json:"leases"`
	Logs    []string    `json:"logs"`
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Running: m.running, Admin: IsAdmin(), Cfg: m.cfg, Logs: append([]string{}, m.logs...)}
	for _, l := range m.leases {
		st.Leases = append(st.Leases, *l)
	}
	sort.Slice(st.Leases, func(i, j int) bool { return st.Leases[i].IP < st.Leases[j].IP })
	return st
}
