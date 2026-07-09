package wincap

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinDivert layer / bit constants.
const (
	layerNetworkForward = 1       // WINDIVERT_LAYER_NETWORK_FORWARD
	flagOutbound        = 1 << 17 // WINDIVERT_ADDRESS.Outbound bit
	invalidHandle       = ^uintptr(0)
)

// address mirrors WINDIVERT_ADDRESS (80 bytes). The bitfields are packed into
// Bits; the 64-byte union holds WINDIVERT_DATA_NETWORK {IfIdx, SubIfIdx} on the
// network-forward layer.
type address struct {
	Timestamp int64
	Bits      uint32 // Layer:8, Event:8, Sniffed:1, Outbound:1, ...
	Reserved2 uint32
	Union     [64]byte
}

func (a *address) setLayer(l uint32)   { a.Bits = (a.Bits &^ 0xFF) | (l & 0xFF) }
func (a *address) setOutbound()        { a.Bits |= flagOutbound }
func (a *address) setIfIdx(idx uint32) { *(*uint32)(unsafe.Pointer(&a.Union[0])) = idx }

var (
	dll            *windows.LazyDLL
	pOpen          *windows.LazyProc
	pRecv          *windows.LazyProc
	pSend          *windows.LazyProc
	pClose         *windows.LazyProc
	pCalcChecksums *windows.LazyProc
	loadOnce       sync.Once
	loadErr        error
)

func loadDLL() error {
	loadOnce.Do(func() {
		if loadErr = ensureWinDivert(); loadErr != nil { // extract WinDivert.dll + .sys next to exe
			return
		}
		dll = windows.NewLazyDLL("WinDivert.dll")
		if loadErr = dll.Load(); loadErr != nil {
			loadErr = fmt.Errorf("load WinDivert.dll: %w", loadErr)
			return
		}
		pOpen = dll.NewProc("WinDivertOpen")
		pRecv = dll.NewProc("WinDivertRecv")
		pSend = dll.NewProc("WinDivertSend")
		pClose = dll.NewProc("WinDivertClose")
		pCalcChecksums = dll.NewProc("WinDivertHelperCalcChecksums")
	})
	return loadErr
}

// Handle is an open WinDivert handle presented as an io.ReadWriter of raw IP
// packets, suitable for tun2socks' iobased endpoint.
type Handle struct {
	h        uintptr
	sendAddr address // template for injected (gVisor -> device) packets
	closed   bool
	mu       sync.Mutex
}

// openForward opens a network-forward-layer handle matching filter, injecting
// replies out the device NIC (ifIdx).
func openForward(filter string, ifIdx uint32) (*Handle, error) {
	if err := loadDLL(); err != nil {
		return nil, err
	}
	cf, err := windows.BytePtrFromString(filter)
	if err != nil {
		return nil, err
	}
	r, _, e := pOpen.Call(
		uintptr(unsafe.Pointer(cf)),
		uintptr(layerNetworkForward),
		0, // priority
		0, // flags (recv + send)
	)
	if r == invalidHandle {
		return nil, fmt.Errorf("WinDivertOpen(%q): %w", filter, e)
	}
	d := &Handle{h: r}
	d.sendAddr.setLayer(layerNetworkForward)
	d.sendAddr.setOutbound()
	d.sendAddr.setIfIdx(ifIdx)
	return d, nil
}

// Read receives one forwarded IP packet (device -> internet).
func (d *Handle) Read(p []byte) (int, error) {
	var recvLen uint32
	var addr address
	r, _, e := pRecv.Call(
		d.h,
		uintptr(unsafe.Pointer(&p[0])),
		uintptr(len(p)),
		uintptr(unsafe.Pointer(&recvLen)),
		uintptr(unsafe.Pointer(&addr)),
	)
	if r == 0 {
		d.mu.Lock()
		closed := d.closed
		d.mu.Unlock()
		if closed {
			return 0, fmt.Errorf("windivert closed")
		}
		return 0, fmt.Errorf("WinDivertRecv: %w", e)
	}
	return int(recvLen), nil
}

// Write injects one IP packet (internet -> device) back onto the forward path.
func (d *Handle) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	// gVisor writes valid checksums, but recompute defensively (also sets any
	// pseudo-header fields WinDivert expects for injection).
	pCalcChecksums.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&d.sendAddr)), 0)
	var sendLen uint32
	r, _, e := pSend.Call(
		d.h,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&sendLen)),
		uintptr(unsafe.Pointer(&d.sendAddr)),
	)
	if r == 0 {
		return 0, fmt.Errorf("WinDivertSend: %w", e)
	}
	return len(p), nil
}

func (d *Handle) Close() error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	if pClose != nil && d.h != 0 && d.h != invalidHandle {
		pClose.Call(d.h)
	}
	return nil
}
