//go:build windows

package wincap

import "testing"

// TestBinding validates the WinDivert DLL loads, procs resolve, and openForward
// reaches the driver. Without admin, WinDivertOpen fails with an access error --
// that still proves the syscall binding is correct (no panic / missing proc).
func TestBinding(t *testing.T) {
	if err := loadDLL(); err != nil {
		t.Fatalf("loadDLL: %v", err)
	}
	if pOpen == nil || pRecv == nil || pSend == nil {
		t.Fatal("procs not resolved")
	}
	h, err := openForward("ip and ip.SrcAddr >= 192.168.88.0 and ip.SrcAddr <= 192.168.88.255", 0)
	if err != nil {
		t.Logf("openForward (expected without admin): %v", err)
		return
	}
	t.Log("openForward succeeded (elevated)")
	_ = h.Close()
}
