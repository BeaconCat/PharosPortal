package portal

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

func (m *Manager) serveDHCP(ctx context.Context, iface *net.Interface) error {
	// 用平台原生 setsockopt 把收发限制在指定网卡:
	//  Linux : SO_BINDTODEVICE (精确)  /  Windows: IP_UNICAST_IF (应答只从该网卡发出, 安全)
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var e error
		if err := c.Control(func(fd uintptr) { e = configureDHCPSocket(fd, iface.Name, iface.Index) }); err != nil {
			return err
		}
		return e
	}}
	pc, err := lc.ListenPacket(ctx, "udp4", ":67")
	if err != nil {
		return fmt.Errorf("listen :67 failed (port busy? admin?): %w", err)
	}
	defer pc.Close()
	go func() { <-ctx.Done(); pc.Close() }()

	bcast := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	buf := make([]byte, 1500)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		req, err := parseDHCP(buf[:n])
		if err != nil {
			continue
		}
		reply := m.handle(req)
		if reply != nil {
			_, _ = pc.WriteTo(reply, bcast)
		}
	}
}

type dhcpReq struct {
	xid    []byte
	flags  []byte
	chaddr net.HardwareAddr
	mtype  byte
}

func parseDHCP(b []byte) (*dhcpReq, error) {
	if len(b) < 240 || b[0] != 1 || b[236] != 99 || b[237] != 130 || b[238] != 83 || b[239] != 99 {
		return nil, fmt.Errorf("not a dhcp request")
	}
	r := &dhcpReq{
		xid:    append([]byte{}, b[4:8]...),
		flags:  append([]byte{}, b[10:12]...),
		chaddr: net.HardwareAddr(append([]byte{}, b[28:34]...)),
	}
	o := b[240:]
	for len(o) >= 2 {
		if o[0] == 255 {
			break
		}
		if o[0] == 0 {
			o = o[1:]
			continue
		}
		l := int(o[1])
		if len(o) < 2+l {
			break
		}
		if o[0] == 53 && l >= 1 {
			r.mtype = o[2]
		}
		o = o[2+l:]
	}
	if r.mtype != 1 && r.mtype != 3 {
		return nil, fmt.Errorf("ignore msg type %d", r.mtype)
	}
	return r, nil
}

func (m *Manager) handle(req *dhcpReq) []byte {
	mac := req.chaddr.String()
	// MAC 白名单: 非空时, 只服务名单内设备 (同段其它设备一律不理)。
	m.mu.Lock()
	blocked := len(m.allow) > 0 && !m.allow[normalizeMAC(mac)]
	m.mu.Unlock()
	if blocked {
		return nil
	}

	yi := m.alloc(mac)
	if yi == nil {
		m.logf("[!] address pool exhausted for %s", req.chaddr)
		return nil
	}
	var respType byte = 2 // OFFER
	if req.mtype == 3 {
		respType = 5 // ACK
		// 计算是否需要打日志 (锁内), 但日志本身放到锁外 —— m.logf 会再取 m.mu, 锁内调用会死锁。
		m.mu.Lock()
		doLog := false
		if l := m.leases[mac]; l != nil {
			if !l.logged || l.IP != yi.String() { // 首次 ACK 或 IP 变化才打, 避免续租刷屏
				l.logged = true
				doLog = true
			}
			l.Ack = true
		}
		m.mu.Unlock()
		if doLog {
			m.logf(">> device  MAC=%s  IP=%s", mac, yi)
		}
	}
	return m.buildReply(req, respType, yi)
}

func normalizeMAC(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == '-':
			out = append(out, ':')
		case r >= 'A' && r <= 'F':
			out = append(out, r+32)
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func (m *Manager) buildReply(req *dhcpReq, mtype byte, yi net.IP) []byte {
	b := make([]byte, 240, 300)
	b[0], b[1], b[2] = 2, 1, 6
	copy(b[4:8], req.xid)
	copy(b[10:12], req.flags)
	copy(b[16:20], yi.To4())
	copy(b[20:24], m.serverIP.To4())
	copy(b[28:34], req.chaddr)
	b[236], b[237], b[238], b[239] = 99, 130, 83, 99
	opt := func(code byte, data []byte) { b = append(b, code, byte(len(data))); b = append(b, data...) }
	opt(53, []byte{mtype})
	opt(54, m.serverIP.To4())
	opt(51, u32b(uint32(m.cfg.LeaseMin*60)))
	opt(1, m.mask.To4())
	opt(3, m.serverIP.To4())
	opt(6, net.ParseIP(m.cfg.DNS).To4())
	return append(b, 255)
}

func (m *Manager) alloc(mac string) net.IP {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.leases[mac]; l != nil {
		return net.ParseIP(l.IP)
	}
	for u := m.lo; u <= m.hi; u++ {
		ip := u2ip(u)
		if !m.used[ip.String()] {
			m.used[ip.String()] = true
			m.leases[mac] = &leaseInfo{MAC: mac, IP: ip.String(), Seen: time.Now()}
			return ip
		}
	}
	return nil
}
