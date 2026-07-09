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
- **Internet for the device via a built-in TUN gateway** (userspace NAT on gVisor netstack, like modern proxy apps) — reliable and identical on Windows/Linux, no WinNAT/ICS. Optionally chain the device's traffic through a **SOCKS5/HTTP proxy** on the host.
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

## How the TUN gateway works
The device's internet access is a userspace NAT built on a TUN device + gVisor netstack (via tun2socks) — the same technique modern proxy apps use. The built-in DHCP still hands the device 192.168.88.x (and shows it in the lease table); the TUN gateway forwards its traffic out to the internet, or through a proxy you specify.

Enable it in the GUI (advanced: "TUN gateway", on by default) or CLI:
```bash
sudo ./pharosportal -iface eth1 -uplink eth0                                # direct (via host)
sudo ./pharosportal -iface eth1 -uplink eth0 -proxy socks5://127.0.0.1:1080 # via host proxy
sudo ./pharosportal -iface eth1 -tun=false                                  # DHCP only (no internet)
```
- **Linux**: clean policy routing — only the device subnet goes through the TUN; host traffic untouched.
- **Windows**: whole-host TUN (default route via the TUN, engine binds outbound to the uplink to avoid loops), like Clash TUN mode. **wintun.dll is embedded** and written next to the exe automatically — no manual step.
- Firewall may block UDP 67/68 — allow the program. Status: engine + routing validated on Linux; the end-to-end path with real downstream hardware is still being tested.

## Safety
The built-in DHCP is restricted to the chosen NIC (Linux `SO_BINDTODEVICE`; Windows `IP_UNICAST_IF` so replies only egress that NIC). It will not disturb DHCP on your other networks.

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
- **内建 TUN 网关给设备上网**（gVisor 用户态 NAT，像现代代理软件）——Windows/Linux 一致、可靠，不依赖 WinNAT/ICS。可选把设备流量**串到主机的 SOCKS5/HTTP 代理**。
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

## TUN 网关怎么工作
设备上网靠内建 TUN 网关（TUN + gVisor 用户态协议栈 / tun2socks），和现代代理软件同一套。内建 DHCP 照样给设备派 192.168.88.x（进租约表）；TUN 网关把它的流量转发到外网，或走你指定的代理。

GUI 高级里"TUN 网关"（默认开），或 CLI：
```bash
sudo ./pharosportal -iface eth1 -uplink eth0                                # direct(经主机)
sudo ./pharosportal -iface eth1 -uplink eth0 -proxy socks5://127.0.0.1:1080 # 走主机代理
sudo ./pharosportal -iface eth1 -tun=false                                  # 仅 DHCP(不上网)
```
- **Linux**：干净的策略路由，只导设备网段进 TUN，主机流量不受影响。
- **Windows**：整机 TUN（默认路由走 TUN，引擎出站绑定上行网卡防环回），类似 Clash TUN。**wintun.dll 已内嵌**，启动自动释放到 exe 同目录，无需手放。
- 防火墙可能拦 UDP 67/68，放行本程序。状态：引擎+路由已在 Linux 验证；接真机的端到端仍在测。

## 许可
MIT。
