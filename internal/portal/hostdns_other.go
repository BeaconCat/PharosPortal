//go:build !windows

package portal

import (
	"net"
	"os"
	"strings"
)

// hostDNS returns the first usable IPv4 nameserver from /etc/resolv.conf, so
// PharosPortal hands the device a resolver the local network actually allows.
// Returns "" if none is found.
func hostDNS() string {
	b, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 || f[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(f[1])
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		return f[1]
	}
	return ""
}
