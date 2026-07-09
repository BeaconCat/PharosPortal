package tungw

import (
	"fmt"
	"os/exec"
)

func execRun(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v: %s", name, args, err, string(out))
	}
	return nil
}

// Linux: 策略路由, 只把下游设备网段的流量导入 TUN, 主机自身流量不受影响。
func (g *Gateway) routeLinux(add bool) error {
	tun, addr, cidr, sub := g.opts.TunName, g.opts.TunAddr, g.opts.TunCIDR, g.opts.DevSubnet
	const table = "8888"
	if add {
		_ = execRun("sysctl", "-w", "net.ipv4.ip_forward=1")
		_ = execRun("ip", "addr", "add", fmt.Sprintf("%s/%d", addr, cidr), "dev", tun)
		if e := execRun("ip", "link", "set", tun, "up"); e != nil {
			return e
		}
		_ = execRun("ip", "rule", "del", "from", sub, "table", table) // 幂等
		if e := execRun("ip", "rule", "add", "from", sub, "table", table, "priority", table); e != nil {
			return e
		}
		_ = execRun("ip", "route", "flush", "table", table)
		return execRun("ip", "route", "add", "default", "dev", tun, "table", table)
	}
	_ = execRun("ip", "rule", "del", "from", sub, "table", table)
	_ = execRun("ip", "route", "flush", "table", table)
	return nil
}

// Windows: 无源路由能力 (route/netsh 只按目的地), 无法像 Linux 那样只导下游网段。
// 默认【不动】主机路由 —— 只准备好 tun (地址+转发), 避免接管主机自身流量。
// 仅当 WholeSystem=true 时才把整机默认路由改走 tun (整机代理模式, 会接管主机流量)。
func (g *Gateway) routeWindows(add bool) error {
	tun, addr := g.opts.TunName, g.opts.TunAddr
	if add {
		_ = execRun("netsh", "interface", "ip", "set", "address", "name="+tun, "static", addr, "255.192.0.0")
		_ = execRun("netsh", "interface", "ipv4", "set", "interface", tun, "forwarding=enabled")
		_ = execRun("netsh", "interface", "ipv4", "set", "interface", g.opts.DevSubnet, "forwarding=enabled") // best-effort
		if !g.opts.WholeSystem {
			g.opts.Log("[!] Windows: whole-system route OFF -> host traffic untouched, but the downstream")
			g.opts.Log("    device has NO internet (Windows routing cannot split by source). Enable")
			g.opts.Log("    whole-system mode to route the whole host (incl. its own traffic) via the tun.")
			return nil
		}
		g.opts.Log("[!] Windows whole-system mode: ALL host traffic now routes through the tun.")
		_ = execRun("route", "delete", "0.0.0.0", "mask", "0.0.0.0", addr)
		return execRun("route", "add", "0.0.0.0", "mask", "0.0.0.0", addr, "metric", "1")
	}
	_ = execRun("route", "delete", "0.0.0.0", "mask", "0.0.0.0", addr)
	return nil
}
