//go:build windows

// Package win manages the Windows desktop WorkerW window hierarchy so that
// OpenGL content appears as a live wallpaper behind desktop icons.
package win

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Windows API constants used for WorkerW manipulation.
const (
	wmSetParent    = 0x0001 // WM_SETPARENT (used via SetParent API)
	sendMsgTimeout = 0x0002 // SMTO_ABORTIFHUNG flag for SendMessageTimeout

	// Window style constants.
	gwlStyle  = -16        // GWL_STYLE index
	wsChild   = 0x40000000 // WS_CHILD
	wsVisible = 0x10000000 // WS_VISIBLE
	swShowNA  = 8          // SW_SHOWNA

	// ShowWindow / SetWindowPos flags.
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procFindWindow         = user32.NewProc("FindWindowW")
	procFindWindowEx       = user32.NewProc("FindWindowExW")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
	procSetParent          = user32.NewProc("SetParent")
	procSetWindowLong      = user32.NewProc("SetWindowLongW")
	procGetWindowLong      = user32.NewProc("GetWindowLongW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procMoveWindow         = user32.NewProc("MoveWindow")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procEnumWindows        = user32.NewProc("EnumWindows")
)

// workerWState holds state discovered during WorkerW enumeration.
type workerWState struct {
	progman syscall.Handle
	workerW syscall.Handle
}

// FindOrCreateWorkerW locates the WorkerW window that sits behind the desktop
// icons and returns its HWND.  If it does not exist yet it is created by
// sending the magic 0x052C message to Progman.
//
// This technique is well documented in the desktop-wallpaper modding community:
//  1. Find "Progman" window.
//  2. Send 0x052C to spawn the WorkerW child.
//  3. Enumerate top-level windows to find the WorkerW that has a "SHELLDLL_DefView" child.
func FindOrCreateWorkerW() (syscall.Handle, error) {
	progman, _, err := procFindWindow.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Progman"))),
		0,
	)
	if progman == 0 {
		return 0, fmt.Errorf("win: FindWindow(Progman) failed: %w", err)
	}

	// Send the magic message that causes Explorer to create a WorkerW behind icons.
	procSendMessageTimeout.Call(
		progman,
		0x052C,
		0, 0,
		sendMsgTimeout,
		1000,
		0,
	)

	state := &workerWState{progman: syscall.Handle(progman)}

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		shell, _, _ := procFindWindowEx.Call(
			uintptr(hwnd),
			0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("SHELLDLL_DefView"))),
			0,
		)
		if shell != 0 {
			workerW, _, _ := procFindWindowEx.Call(
				0,
				uintptr(hwnd),
				uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("WorkerW"))),
				0,
			)
			state.workerW = syscall.Handle(workerW)
		}
		return 1 // continue enumeration
	})

	procEnumWindows.Call(cb, 0)

	if state.workerW == 0 {
		return 0, fmt.Errorf("win: WorkerW not found after sending 0x052C")
	}
	return state.workerW, nil
}

// SetParentToWorkerW re-parents hwnd so that it becomes a child of the
// WorkerW desktop background window.
func SetParentToWorkerW(hwnd, workerW syscall.Handle) error {
	ret, _, err := procSetParent.Call(uintptr(hwnd), uintptr(workerW))
	if ret == 0 {
		return fmt.Errorf("win: SetParent failed: %w", err)
	}
	return nil
}

// MakeFullscreen resizes hwnd to cover the full virtual screen (all monitors).
func MakeFullscreen(hwnd syscall.Handle) error {
	smCxVirtualScreen := uintptr(78) // SM_CXVIRTUALSCREEN
	smCyVirtualScreen := uintptr(79) // SM_CYVIRTUALSCREEN

	w, _, _ := procGetSystemMetrics.Call(smCxVirtualScreen)
	h, _, _ := procGetSystemMetrics.Call(smCyVirtualScreen)

	ret, _, err := procMoveWindow.Call(uintptr(hwnd), 0, 0, w, h, 1)
	if ret == 0 {
		return fmt.Errorf("win: MoveWindow failed: %w", err)
	}
	return nil
}

// ApplyChildStyle sets the WS_CHILD | WS_VISIBLE window style on hwnd so it
// integrates cleanly as a child of WorkerW.
func ApplyChildStyle(hwnd syscall.Handle) {
	style, _, _ := procGetWindowLong.Call(uintptr(hwnd), uintptr(gwlStyle))
	style |= wsChild | wsVisible
	procSetWindowLong.Call(uintptr(hwnd), uintptr(gwlStyle), style)
	procShowWindow.Call(uintptr(hwnd), swShowNA)
}
