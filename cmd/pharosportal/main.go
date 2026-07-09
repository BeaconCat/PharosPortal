// PharosPortal: take over a physical NIC to give a directly-wired network device
// an IP (built-in DHCP) and bridge it into your LAN/internet (NAT or Windows ICS).
//
//	GUI (default, run with no -iface): opens a local web page.
//	CLI:  pharosportal -iface eth1 -uplink eth0
//
// Requires administrator/root.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/BeaconCat/PharosPortal/internal/portal"
	"github.com/BeaconCat/PharosPortal/internal/webui"
)

func main() {
	var (
		fIface  = flag.String("iface", "", "NIC facing the device (set -> CLI mode)")
		fUplink = flag.String("uplink", "", "uplink NIC for NAT")
		fServer = flag.String("server-ip", "192.168.88.1", "gateway IP")
		fMask   = flag.String("mask", "255.255.255.0", "subnet mask")
		fStart  = flag.String("range-start", "192.168.88.50", "pool start")
		fEnd    = flag.String("range-end", "192.168.88.150", "pool end")
		fDNS    = flag.String("dns", "223.5.5.5", "DNS to hand out")
		fLease  = flag.Int("lease-min", 720, "lease minutes")
		fNoNAT  = flag.Bool("no-nat", false, "do not configure NAT")
		fNoIP   = flag.Bool("no-setip", false, "do not auto-configure NIC IP")
		fTUN    = flag.Bool("tun", false, "TUN gateway mode (userspace NAT, bypass WinNAT/ICS)")
		fProxy  = flag.String("proxy", "", "TUN downstream proxy, e.g. socks5://127.0.0.1:1080 (empty=direct)")
		fPort   = flag.Int("gui-port", 8765, "GUI local port")
	)
	flag.Parse()
	mgr := portal.NewManager()

	if *fIface == "" { // GUI
		if err := webui.Run(mgr, *fPort); err != nil {
			fmt.Println("[x]", err)
			os.Exit(1)
		}
		return
	}

	// CLI
	if !portal.IsAdmin() {
		fmt.Println("[x] run as administrator/root")
		os.Exit(1)
	}
	cfg := portal.Config{
		Iface: *fIface, Uplink: *fUplink, ServerIP: *fServer, Mask: *fMask,
		RangeStart: *fStart, RangeEnd: *fEnd, DNS: *fDNS, LeaseMin: *fLease,
		NAT: !*fNoNAT, SetIP: !*fNoIP, TUN: *fTUN, Proxy: *fProxy,
	}
	if err := mgr.Start(cfg); err != nil {
		fmt.Println("[x] start failed:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	mgr.Stop()
}
