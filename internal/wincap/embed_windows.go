//go:build windows

package wincap

import (
	_ "embed"
	"os"
	"path/filepath"
)

// WinDivert.dll + WinDivert64.sys (official amd64 v2.2.2) embedded and written
// next to the exe at startup. WinDivert.dll loads its driver (.sys) from its own
// directory, so both must sit together beside the exe.
//
//go:embed WinDivert.dll
var winDivertDLL []byte

//go:embed WinDivert64.sys
var winDivertSys []byte

func ensureWinDivert() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	for name, data := range map[string][]byte{
		"WinDivert.dll":   winDivertDLL,
		"WinDivert64.sys": winDivertSys,
	} {
		dst := filepath.Join(dir, name)
		if fi, err := os.Stat(dst); err == nil && fi.Size() == int64(len(data)) {
			continue
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
