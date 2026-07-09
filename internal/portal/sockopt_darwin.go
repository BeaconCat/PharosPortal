//go:build darwin

package portal

import "golang.org/x/sys/unix"

// macOS: SO_BROADCAST + SO_REUSEADDR + IP_BOUND_IF (按 ifindex 绑定收发网卡,
// 等价于 Linux 的 SO_BINDTODEVICE)。
func configureDHCPSocket(fd uintptr, _ string, ifIndex int) error {
	f := int(fd)
	if e := unix.SetsockoptInt(f, unix.SOL_SOCKET, unix.SO_BROADCAST, 1); e != nil {
		return e
	}
	if e := unix.SetsockoptInt(f, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); e != nil {
		return e
	}
	return unix.SetsockoptInt(f, unix.IPPROTO_IP, unix.IP_BOUND_IF, ifIndex)
}
