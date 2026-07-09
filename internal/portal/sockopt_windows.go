//go:build windows

package portal

import "syscall"

// IP_UNICAST_IF (Winsock, level IPPROTO_IP): 指定出站接口, 值为接口索引的网络字节序。
const ipUnicastIF = 31

// Windows: SO_BROADCAST + SO_REUSEADDR + IP_UNICAST_IF (应答只从指定网卡发出)。
func configureDHCPSocket(fd uintptr, _ string, ifIndex int) error {
	h := syscall.Handle(fd)
	if e := syscall.SetsockoptInt(h, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); e != nil {
		return e
	}
	if e := syscall.SetsockoptInt(h, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); e != nil {
		return e
	}
	return syscall.SetsockoptInt(h, syscall.IPPROTO_IP, ipUnicastIF, int(htonl(uint32(ifIndex))))
}

func htonl(n uint32) uint32 { return n<<24 | (n&0xff00)<<8 | (n>>8)&0xff00 | n>>24 }
