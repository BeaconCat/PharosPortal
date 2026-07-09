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

// enableNAT: Linux iptables MASQUERADE (Windows 走 ICS, 见 nat_ics)。
func enableNAT(cidr, uplink string) error {
	_ = run("sysctl", "-w", "net.ipv4.ip_forward=1")
	_ = run("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", cidr, "-o", uplink, "-j", "MASQUERADE")
	_ = run("iptables", "-A", "FORWARD", "-s", cidr, "-o", uplink, "-j", "ACCEPT")
	return run("iptables", "-A", "FORWARD", "-d", cidr, "-i", uplink, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")
}

func disableNAT(cidr, uplink string) error {
	_ = run("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", cidr, "-o", uplink, "-j", "MASQUERADE")
	_ = run("iptables", "-D", "FORWARD", "-s", cidr, "-o", uplink, "-j", "ACCEPT")
	return run("iptables", "-D", "FORWARD", "-d", cidr, "-i", uplink, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")
}

// enableICS: Windows Internet 连接共享 (HNetCfg COM), 共享 uplink 给 private 网卡。
func enableICS(uplink, private string) error {
	ps := fmt.Sprintf(`$ErrorActionPreference='Stop'
$s=New-Object -ComObject HNetCfg.HNetShare
$pub=$null;$priv=$null
foreach($c in $s.EnumEveryConnection){$p=$s.NetConnectionProps($c)
 if($p.Name -eq '%[1]s'){$pub=$s.INetSharingConfigurationForINetConnection($c)}
 if($p.Name -eq '%[2]s'){$priv=$s.INetSharingConfigurationForINetConnection($c)}}
if(-not $pub){throw 'uplink not found: %[1]s'}
if(-not $priv){throw 'device NIC not found: %[2]s'}
$pub.EnableSharing(0)
$priv.EnableSharing(1)`, psEsc(uplink), psEsc(private))
	return run("powershell", "-NoProfile", "-Command", ps)
}

func disableICS(uplink, private string) error {
	ps := fmt.Sprintf(`$s=New-Object -ComObject HNetCfg.HNetShare
foreach($c in $s.EnumEveryConnection){$p=$s.NetConnectionProps($c)
 if($p.Name -eq '%[1]s' -or $p.Name -eq '%[2]s'){$s.INetSharingConfigurationForINetConnection($c).DisableSharing()}}`,
		psEsc(uplink), psEsc(private))
	return run("powershell", "-NoProfile", "-Command", ps)
}

func psEsc(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'')
		}
		out = append(out, r)
	}
	return string(out)
}

// readARP 读系统 ARP 表, 返回前缀匹配的 [ip, mac] (排除网关.1/广播.255/组播)。
func readARP(prefix string) [][2]string {
	out, err := exec.Command("arp", "-a").Output()
	if err != nil {
		return nil
	}
	var res [][2]string
	for _, line := range splitLines(string(out)) {
		f := fields(line)
		if len(f) < 2 {
			continue
		}
		ip, mac := f[0], normMAC(f[1])
		if ip == "" || mac == "" || len(ip) < len(prefix) || ip[:len(prefix)] != prefix {
			continue
		}
		last := ip[len(prefix):]
		if last == "1" || last == "255" || mac == "ff:ff:ff:ff:ff:ff" || (len(mac) >= 8 && mac[:8] == "01:00:5e") {
			continue
		}
		res = append(res, [2]string{ip, mac})
	}
	return res
}

func normMAC(s string) string {
	b := []rune{}
	for _, r := range s {
		switch {
		case r == '-':
			b = append(b, ':')
		case r >= 'A' && r <= 'Z':
			b = append(b, r+32)
		default:
			b = append(b, r)
		}
	}
	m := string(b)
	if len(m) == 17 && m[2] == ':' {
		return m
	}
	return ""
}

func splitLines(s string) []string {
	var l []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			l = append(l, cur)
			cur = ""
		} else if r != '\r' {
			cur += string(r)
		}
	}
	return append(l, cur)
}

func fields(s string) []string {
	var f []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if cur != "" {
				f = append(f, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		f = append(f, cur)
	}
	return f
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, string(out))
	}
	return nil
}
