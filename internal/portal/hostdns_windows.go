//go:build windows

package portal

import (
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hostDNS returns the host's first usable IPv4 DNS server (from an up adapter),
// so PharosPortal hands the device a resolver the local network actually allows.
// Networks that block public resolvers (223.5.5.5, 1.1.1.1) still work this way.
// Returns "" if none is found.
func hostDNS() string {
	size := uint32(16384)
	for range 3 {
		buf := make([]byte, size)
		aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_INET,
			windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST, 0, aa, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue // size now holds the required length; retry
		}
		if err != nil {
			return ""
		}
		for ; aa != nil; aa = aa.Next {
			if aa.OperStatus != windows.IfOperStatusUp {
				continue
			}
			for d := aa.FirstDnsServerAddress; d != nil; d = d.Next {
				sa, err := d.Address.Sockaddr.Sockaddr()
				if err != nil {
					continue
				}
				in4, ok := sa.(*syscall.SockaddrInet4)
				if !ok {
					continue
				}
				ip := net.IP(in4.Addr[:])
				if ip.IsLoopback() || ip.IsUnspecified() {
					continue
				}
				return ip.String()
			}
		}
		return ""
	}
	return ""
}
