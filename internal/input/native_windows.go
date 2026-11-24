//go:build windows

package input

import (
	"sync"
	"time"
	"unsafe"

	"gioui.org/app"
	"golang.org/x/sys/windows"
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procGetPointerType    = user32.NewProc("GetPointerType")
	procGetPointerPenInfo = user32.NewProc("GetPointerPenInfo")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW   = user32.NewProc("CallWindowProcW")
	procGetMessageTime    = user32.NewProc("GetMessageTime")
)

const (
	wmPointerUpdate = 0x0245
	wmPointerDown   = 0x0246
	wmPointerUp     = 0x0247
	ptPen           = 3
	penMaskPressure = 0x1
	penMaskTiltX    = 0x4
	penMaskTiltY    = 0x8
)

// pointerInfo mirrors POINTER_INFO (winuser.h); Go's natural alignment
// matches the C layout on amd64 and arm64.
type pointerInfo struct {
	PointerType           uint32
	PointerID             uint32
	FrameID               uint32
	PointerFlags          uint32
	SourceDevice          uintptr
	HwndTarget            uintptr
	PtPixelLocation       [2]int32
	PtHimetricLocation    [2]int32
	PtPixelLocationRaw    [2]int32
	PtHimetricLocationRaw [2]int32
	Time                  uint32
	HistoryCount          uint32
	InputData             int32
	KeyStates             uint32
	PerformanceCount      uint64
	ButtonChangeType      int32
}

// pointerPenInfo mirrors POINTER_PEN_INFO.
type pointerPenInfo struct {
	PointerInfo pointerInfo
	PenFlags    uint32
	PenMask     uint32
	Pressure    uint32 // 0..1024
	Rotation    uint32
	TiltX       int32 // -90..90 degrees
	TiltY       int32
}

// winNative subclasses Gio's window procedure. It reads pen data for each
// WM_POINTER* message and then forwards the message to Gio unchanged, so
// the reading is stored before Gio emits the matching pointer.Event.
type winNative struct {
	mu   sync.Mutex
	ring ring

	hwnd, prev, cb uintptr
}

func newNative() Native { return &winNative{} }

func (*winNative) Name() string { return "Windows Ink" }

func (n *winNative) Attach(e app.ViewEvent) {
	v, ok := e.(app.Win32ViewEvent)
	if !ok || !v.Valid() || v.HWND == n.hwnd {
		return
	}
	if n.cb == 0 {
		n.cb = windows.NewCallback(n.wndProc)
	}
	gwlpWndProc := -4
	prev, _, _ := procSetWindowLongPtrW.Call(v.HWND, uintptr(gwlpWndProc), n.cb)
	if prev != 0 {
		n.hwnd, n.prev = v.HWND, prev
	}
}

func (n *winNative) wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmPointerDown, wmPointerUpdate, wmPointerUp:
		id := uintptr(uint16(wParam))
		var typ uint32
		if r, _, _ := procGetPointerType.Call(id, uintptr(unsafe.Pointer(&typ))); r != 0 && typ == ptPen {
			var info pointerPenInfo
			if r, _, _ := procGetPointerPenInfo.Call(id, uintptr(unsafe.Pointer(&info))); r != 0 {
				// Gio stamps the pointer.Event for this same message with
				// GetMessageTime, so use that clock for matching.
				ms, _, _ := procGetMessageTime.Call()
				rd := Reading{Pressure: 0.5, T: time.Duration(uint32(ms)) * time.Millisecond}
				if info.PenMask&penMaskPressure != 0 {
					rd.Pressure = float32(info.Pressure) / 1024
				}
				if info.PenMask&penMaskTiltX != 0 {
					rd.TiltX = float32(info.TiltX) / 90
				}
				if info.PenMask&penMaskTiltY != 0 {
					rd.TiltY = float32(info.TiltY) / 90
				}
				n.mu.Lock()
				n.ring.add(rd)
				n.mu.Unlock()
			}
		}
	}
	r, _, _ := procCallWindowProcW.Call(n.prev, hwnd, msg, wParam, lParam)
	return r
}

func (n *winNative) At(t time.Duration) (Reading, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ring.at(t)
}
