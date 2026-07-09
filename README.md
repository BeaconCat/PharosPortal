# PharosPortal

A tiny cross-platform tool to **debug a network device directly through a physical NIC**.
Plug the device (SBC, IP-KVM, router, BMC, embedded board...) straight into a spare
Ethernet port, and PharosPortal takes over that NIC, hands the device an IP (built-in
DHCP), and bridges it into your LAN/internet — no need to stand up a separate DHCP box.

Single binary, no external Go dependencies, clean web GUI, EN/中文.

[中文说明见下 / Chinese below](#中文)

---

## Features
- **Owns one physical NIC** and runs a built-in DHCP server **bound to that NIC only** — it will *not* hand out addresses on your corporate/home LAN (no rogue DHCP).
- **Clean local web GUI**: auto-scans and lists NICs, remembers your settings (localStorage), one-click Start/Stop, live device table + logs. Bilingual (English default, 中文 toggle).
- **NAT / internet for the device**: Linux `iptables MASQUERADE`; Windows uses **ICS** (Internet Connection Sharing) automatically, because `New-NetNat` (WinNAT) is unreliable on many machines.
- **CLI mode** for scripting / headless.

## Build
Go 1.22+ (`export GOPROXY=https://goproxy.cn,direct` in CN).
```bash
git clone https://github.com/BeaconCat/PharosPortal.git && cd PharosPortal
go build -o pharosportal ./cmd/pharosportal
# cross build:
GOOS=windows GOARCH=amd64 go build -o pharosportal.exe ./cmd/pharosportal
```

## Usage
Run as **administrator / root** (needed to configure NICs, listen on :67, set up NAT).

### GUI (default)
Run with no `-iface`; a local page opens in your browser.
```bash
sudo ./pharosportal                 # Linux
# Windows: right-click "Run as administrator", then:  pharosportal.exe
```
Pick the NIC facing the device + the uplink NIC, keep the defaults, click **Start**.
Power on the device — its MAC/IP shows up live in the table.

### CLI
```bash
sudo ./pharosportal -iface eth1 -uplink eth0
pharosportal.exe -iface "Ethernet 2" -uplink "Ethernet"
```
Flags: `-server-ip -mask -range-start -range-end -dns -lease-min -no-nat -no-setip -gui-port`.

## Access the device from other machines (port-forward)
The device sits on a private subnet reachable from the host. To reach it from your LAN:
- Windows: `netsh interface portproxy add v4tov4 listenport=8080 connectaddress=<dev-ip> connectport=80`
- Linux: `iptables -t nat -A PREROUTING -p tcp --dport 8080 -j DNAT --to <dev-ip>:80`

## Platform notes
- **Windows NAT = ICS (automatic).** `New-NetNat` often fails with `0x80041013 provider load failure`, so when NAT is enabled PharosPortal uses ICS via the HNetCfg COM API. ICS is system-managed: it sets the device NIC to **192.168.137.1**, runs its own DHCP, and does NAT — so in this mode the device gets **192.168.137.x** (not the built-in DHCP / your 192.168.88.x). PharosPortal discovers the device via ARP and shows it. Uncheck NAT to keep the built-in DHCP (192.168.88.x + lease table) — the device is still reachable from the host, it just won't reach the internet.
- Firewall may block UDP 67/68 — allow the program.

## Safety
The built-in DHCP is restricted to the chosen NIC (Linux `SO_BINDTODEVICE`; Windows `IP_UNICAST_IF` so replies only egress that NIC). It will not disturb DHCP on your other networks.

## TUN gateway mode (experimental)
A userspace NAT built on a TUN device + gVisor netstack (via tun2socks), like modern proxy apps.
It keeps the built-in DHCP (device still gets 192.168.88.x + shows in the lease table) and adds
reliable cross-platform NAT **without** WinNAT/ICS. Bonus: point it at a proxy to route the
downstream device's traffic through the host's SOCKS5/HTTP proxy or VPN.

Enable it in the GUI (advanced: "TUN gateway") or CLI:
```bash
sudo ./pharosportal -iface eth1 -uplink eth0 -tun                       # direct (via host)
sudo ./pharosportal -iface eth1 -uplink eth0 -tun -proxy socks5://127.0.0.1:1080  # via host proxy
```
- Linux: clean policy routing — only the device subnet goes through the TUN; host traffic untouched.
- Windows: whole-host TUN (default route via the TUN, engine binds outbound to the uplink to avoid loops), like Clash TUN mode; needs **wintun.dll** next to the exe ([download](https://www.wireguard.com/xplatform/#wintun)).
- Status: the tun2socks forwarding engine is validated; the end-to-end downstream path still needs testing against real hardware. Report issues.

## Project layout
```
cmd/pharosportal/     entry (CLI + GUI wiring)
internal/portal/      NIC takeover, DHCP, NAT/ICS, ARP  (pure stdlib)
internal/webui/       local web GUI (embedded, i18n)
```

## License
MIT. See [LICENSE](LICENSE).

---

<a name="中文"></a>
# PharosPortal（中文）

一个跨平台小工具，**通过物理网卡直连调试网络设备**。把设备（开发板 / IP-KVM / 路由器 / BMC / 嵌入式板…）用网线插到主机一块空闲网口，PharosPortal 接管该网卡、内建 DHCP 给设备派 IP，并把它桥接进你的内网/外网——不用再单独搭一台 DHCP 机器。

单文件、无外部 Go 依赖、简洁 Web 界面、中英双语。

## 特性
- **接管一块物理网卡**，内建 DHCP **只绑定该网卡**——绝不会在公司/家庭 LAN 上乱发地址。
- **简洁本地 Web 界面**：自动扫描列出网卡、记住你的设置（localStorage）、一键启停、实时设备表 + 日志。中英双语（默认英语，可切中文）。
- **给设备做 NAT 上网**：Linux 用 `iptables MASQUERADE`；Windows 自动改用 **ICS**（因为 `New-NetNat`/WinNAT 在很多机器上不可靠）。
- **CLI 模式**便于脚本/无头使用。

## 构建
Go 1.22+（国内 `export GOPROXY=https://goproxy.cn,direct`）。
```bash
go build -o pharosportal ./cmd/pharosportal
```

## 用法
需**管理员/root**（配网卡、监听 :67、开 NAT）。

- **GUI（默认）**：不带 `-iface` 直接运行，自动开浏览器。选“面向设备的网卡”+“上行网卡”，默认参数，点“启动”。设备上电后表格实时显示其 MAC/IP。
- **CLI**：`sudo ./pharosportal -iface eth1 -uplink eth0`

## 平台说明
- **Windows NAT = ICS（自动）**：`New-NetNat` 常报 `0x80041013 提供程序加载失败`，故启用 NAT 时改用系统 ICS（HNetCfg COM）。ICS 由系统托管：把设备网卡设为 **192.168.137.1**、自带 DHCP、做 NAT——此模式下设备拿到 **192.168.137.x**（不是内建 DHCP 的 192.168.88.x），工具经 ARP 发现并显示。想用内建 DHCP（88.x + 租约表）就**不勾 NAT**（设备仍可被本机访问，只是它自己不上网）。
- 防火墙可能拦 UDP 67/68，放行本程序。

## TUN 网关模式（实验）
基于 TUN + gVisor 用户态协议栈（tun2socks），像现代代理软件那样做 NAT。**保留内建 DHCP**（设备照拿 192.168.88.x、进租约表），并提供跨平台可靠 NAT，**摆脱 WinNAT/ICS**。附带：填代理即可让**下游设备流量走主机的 SOCKS5/HTTP 代理或 VPN**。

GUI 高级里勾"TUN 网关"，或 CLI：
```bash
sudo ./pharosportal -iface eth1 -uplink eth0 -tun                                  # direct(经主机)
sudo ./pharosportal -iface eth1 -uplink eth0 -tun -proxy socks5://127.0.0.1:1080   # 走主机代理
```
- Linux：干净的策略路由，只导设备网段进 TUN，主机流量不受影响。
- Windows：整机 TUN（默认路由走 TUN，引擎出站绑定上行网卡防环回），类似 Clash TUN；需把 **wintun.dll** 放到 exe 同目录（[下载](https://www.wireguard.com/xplatform/#wintun)）。
- 状态：tun2socks 转发引擎已验证；下游端到端链路仍需真机测试，欢迎反馈。

## 许可
MIT。
