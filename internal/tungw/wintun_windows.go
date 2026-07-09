//go:build windows

package tungw

import (
	_ "embed"
	"os"
	"path/filepath"
)

// wintun.dll (官方 amd64) 嵌入二进制, 启动时写到 exe 同目录, 免用户手放。
// wintun 包用 LoadLibraryEx(APPLICATION_DIR|SYSTEM32) 加载, 故放 exe 目录即可。
//
//go:embed wintun.dll
var wintunDLL []byte

func ensureWintun() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if fi, err := os.Stat(dst); err == nil && fi.Size() == int64(len(wintunDLL)) {
		return nil // 已存在且大小一致
	}
	return os.WriteFile(dst, wintunDLL, 0o644)
}
