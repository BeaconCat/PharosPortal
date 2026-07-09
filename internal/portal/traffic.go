package portal

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/tunnel/statistic"
	"golang.org/x/net/proxy"
)

// DevTraffic 单设备流量。
type DevTraffic struct {
	IP    string `json:"ip"`
	MAC   string `json:"mac"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
	Conns int    `json:"conns"`
}

// Traffic 汇总 (仅 TUN 模式有意义)。
type Traffic struct {
	TotalUp   int64        `json:"totalUp"`
	TotalDown int64        `json:"totalDown"`
	Devices   []DevTraffic `json:"devices"`
}

// trafficStats 从 tun2socks 统计单例取连接快照, 按源 IP (=设备) 聚合。
func (m *Manager) trafficStats() Traffic {
	if statistic.DefaultManager == nil {
		return Traffic{}
	}
	b, _ := json.Marshal(statistic.DefaultManager.Snapshot())
	var s struct {
		UploadTotal   int64 `json:"uploadTotal"`
		DownloadTotal int64 `json:"downloadTotal"`
		Connections   []struct {
			Metadata struct {
				SourceIP string `json:"sourceIP"`
			} `json:"metadata"`
			Upload   int64 `json:"upload"`
			Download int64 `json:"download"`
		} `json:"connections"`
	}
	_ = json.Unmarshal(b, &s)

	agg := map[string]*DevTraffic{}
	for _, c := range s.Connections {
		ip := c.Metadata.SourceIP
		d := agg[ip]
		if d == nil {
			d = &DevTraffic{IP: ip}
			agg[ip] = d
		}
		d.Up += c.Upload
		d.Down += c.Download
		d.Conns++
	}
	m.mu.Lock()
	ipMac := map[string]string{}
	for _, l := range m.leases {
		ipMac[l.IP] = l.MAC
	}
	m.mu.Unlock()

	t := Traffic{TotalUp: s.UploadTotal, TotalDown: s.DownloadTotal}
	for ip, d := range agg {
		d.MAC = ipMac[ip]
		t.Devices = append(t.Devices, *d)
	}
	sort.Slice(t.Devices, func(i, j int) bool { return t.Devices[i].IP < t.Devices[j].IP })
	return t
}

// TestProxy 通过给定代理连一次目标, 返回毫秒延迟 (空/direct = 直连测试)。
func TestProxy(pxy string) (int64, error) {
	const target = "1.1.1.1:443"
	start := time.Now()
	switch {
	case pxy == "" || strings.HasPrefix(pxy, "direct"):
		c, err := net.DialTimeout("tcp", target, 8*time.Second)
		if err != nil {
			return 0, err
		}
		_ = c.Close()
	case strings.HasPrefix(pxy, "socks5://") || strings.HasPrefix(pxy, "socks://"):
		u, err := url.Parse(pxy)
		if err != nil {
			return 0, err
		}
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 8 * time.Second})
		if err != nil {
			return 0, err
		}
		c, err := d.Dial("tcp", target)
		if err != nil {
			return 0, err
		}
		_ = c.Close()
	case strings.HasPrefix(pxy, "http://") || strings.HasPrefix(pxy, "https://"):
		u, err := url.Parse(pxy)
		if err != nil {
			return 0, err
		}
		cl := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
		resp, err := cl.Head("https://1.1.1.1")
		if err != nil {
			return 0, err
		}
		_ = resp.Body.Close()
	default:
		return 0, fmt.Errorf("unsupported proxy scheme (use socks5:// or http://)")
	}
	return time.Since(start).Milliseconds(), nil
}
