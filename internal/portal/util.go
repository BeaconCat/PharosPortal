package portal

import (
	"encoding/binary"
	"fmt"
	"net"
)

func ip2u(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip)
}

func u2ip(u uint32) net.IP { b := make([]byte, 4); binary.BigEndian.PutUint32(b, u); return net.IP(b) }
func u32b(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

func subnetCIDR(ip net.IP, ones int) string {
	m := net.CIDRMask(ones, 32)
	return fmt.Sprintf("%s/%d", ip.Mask(m), ones)
}
