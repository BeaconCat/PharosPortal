//go:build !windows

package portal

import "syscall"

// Linux/Unix: SO_BROADCAST + SO_REUSEADDR + SO_BINDTODEVICE (只在指定网卡收发)。
func configureDHCPSocket(fd uintptr, ifaceName string, _ int) error {
	f := int(fd)
	if e := syscall.SetsockoptInt(f, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); e != nil {
		return e
	}
	if e := syscall.SetsockoptInt(f, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); e != nil {
		return e
	}
	return syscall.SetsockoptString(f, syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifaceName)
}
