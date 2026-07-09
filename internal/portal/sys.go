package portal

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
)

// IsAdmin 是否以管理员/root 运行。
func IsAdmin() bool {
	if runtime.GOOS == "windows" {
		return exec.Command("net", "session").Run() == nil
	}
	return os.Geteuid() == 0
}

// ScanIfaces 列出非回环网卡 (供 UI 下拉)。
func ScanIfaces() []map[string]any {
	var out []map[string]any
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		var addrs []string
		as, _ := i.Addrs()
		for _, a := range as {
			addrs = append(addrs, a.String())
		}
		out = append(out, map[string]any{
			"name": i.Name, "mac": i.HardwareAddr.String(),
			"up": i.Flags&net.FlagUp != 0, "addrs": addrs,
		})
	}
	return out
}

func setStaticIP(iface string, ip, mask net.IP, ones int) error {
	if runtime.GOOS == "windows" {
		return run("netsh", "interface", "ip", "set", "address", fmt.Sprintf("name=%s", iface), "static", ip.String(), mask.String())
	}
	_ = run("ip", "addr", "flush", "dev", iface)
	if err := run("ip", "addr", "add", fmt.Sprintf("%s/%d", ip, ones), "dev", iface); err != nil {
		return err
	}
	return run("ip", "link", "set", iface, "up")
}

func unsetStaticIP(iface string) error {
	if runtime.GOOS == "windows" {
		return run("netsh", "interface", "ip", "set", "address", fmt.Sprintf("name=%s", iface), "dhcp")
	}
	return run("ip", "addr", "flush", "dev", iface)
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, string(out))
	}
	return nil
}
