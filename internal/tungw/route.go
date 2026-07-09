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

// Windows: 整机 TUN (默认路由走 tun, 引擎出站已绑定上行网卡防环回), 类似 Clash TUN 模式。
// 主机流量在 direct 下透明通过; 下游设备经主机转发命中默认路由进 tun 被 NAT。
func (g *Gateway) routeWindows(add bool) error {
	tun, addr, sub := g.opts.TunName, g.opts.TunAddr, g.opts.DevSubnet
	if add {
		_ = execRun("netsh", "interface", "ip", "set", "address", "name="+tun, "static", addr, "255.192.0.0")
		_ = execRun("netsh", "interface", "ipv4", "set", "interface", tun, "forwarding=enabled")
		_ = execRun("netsh", "interface", "ipv4", "set", "interface", sub, "forwarding=enabled") // best-effort
		_ = execRun("route", "delete", "0.0.0.0", "mask", "0.0.0.0", addr)
		return execRun("route", "add", "0.0.0.0", "mask", "0.0.0.0", addr, "metric", "1")
	}
	_ = execRun("route", "delete", "0.0.0.0", "mask", "0.0.0.0", addr)
	return nil
}
