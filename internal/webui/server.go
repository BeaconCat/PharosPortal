// Package webui 提供本地网页控制界面。
package webui

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/BeaconCat/PharosPortal/internal/portal"
)

//go:embed index.html
var indexHTML []byte

// Run 启动本地 GUI 服务并打开浏览器 (阻塞)。
func Run(mgr *portal.Manager, port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/api/ifaces", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ifaces": portal.ScanIfaces(), "defaults": portal.DefaultConfig()})
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, mgr.Status())
	})
	mux.HandleFunc("/api/start", func(w http.ResponseWriter, r *http.Request) {
		var cfg portal.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !portal.IsAdmin() {
			writeJSON(w, map[string]any{"ok": false, "err": "run as administrator/root"})
			return
		}
		if err := mgr.Start(cfg); err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		mgr.Stop()
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/proxytest", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Proxy string `json:"proxy"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ms, err := portal.TestProxy(body.Proxy)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "ms": ms})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("GUI port %d busy: %w (use -gui-port)", port, err)
	}
	url := "http://" + addr
	fmt.Printf("[*] PharosPortal GUI: %s  (admin=%v)\n", url, portal.IsAdmin())
	if !portal.IsAdmin() {
		fmt.Println("[!] not running as admin/root -- you can browse NICs, but Start will fail. Re-open elevated.")
	}
	go func() { time.Sleep(400 * time.Millisecond); openBrowser(url) }()
	defer mgr.Stop()
	return (&http.Server{Handler: mux}).Serve(ln)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}
