//go:build windows

// Package win manages the Windows desktop WorkerW window hierarchy so that
// OpenGL content appears as a live wallpaper behind desktop icons.
package win

import (
	"fmt"
	"log"
	"syscall"
	"time"
	"unsafe"
)

// Windows API constants used for WorkerW manipulation.
const (
	wmSetParent    = 0x0001 // WM_SETPARENT (used via SetParent API)
	sendMsgTimeout = 0x0002 // SMTO_ABORTIFHUNG flag for SendMessageTimeout

	// Window style constants.
	wsChild   = 0x40000000 // WS_CHILD
	wsVisible = 0x10000000 // WS_VISIBLE
	wsPopup   = 0x80000000 // WS_POPUP
	swShowNA  = 8          // SW_SHOWNA

	// Extended window styles
	wsExNoActivate  = 0x08000000 // WS_EX_NOACTIVATE - don't activate on click
	wsExTransparent = 0x00000020 // WS_EX_TRANSPARENT - make clicks pass through
	wsExToolWindow  = 0x00000080 // WS_EX_TOOLWINDOW

	// ShowWindow / SetWindowPos flags.
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	// WM_USER constant
	wmUser = 0x0400
)

var (
	// Keep these as typed variables (int32) so converting to uintptr
	// doesn't trigger compile-time overflow for negative values.
	gwlStyle   int32 = -16 // GWL_STYLE index
	gwlExStyle int32 = -20 // GWL_EXSTYLE index
	hwndBottom int64 = 1   // HWND_BOTTOM for SetWindowPos

	user32 = syscall.NewLazyDLL("user32.dll")

	procFindWindow         = user32.NewProc("FindWindowW")
	procFindWindowEx       = user32.NewProc("FindWindowExW")
	procEnumWindows        = user32.NewProc("EnumWindows")
	procEnumChildWindows   = user32.NewProc("EnumChildWindows")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
	procSetParent          = user32.NewProc("SetParent")
	procSetWindowLong      = user32.NewProc("SetWindowLongW")
	procGetWindowLong      = user32.NewProc("GetWindowLongW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procMoveWindow         = user32.NewProc("MoveWindow")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procGetClassName       = user32.NewProc("GetClassNameW")
	procIsWindowVisible    = user32.NewProc("IsWindowVisible")
	procInvalidateRect     = user32.NewProc("InvalidateRect")
	procRedrawWindow       = user32.NewProc("RedrawWindow")
)

// workerWState holds state discovered during WorkerW enumeration.
type workerWState struct {
	progman syscall.Handle
	workerW syscall.Handle
}

// FindOrCreateWorkerW locates the WorkerW window that sits behind desktop icons.
// If not found, attempts to create it by sending the magic Progman message.
func FindOrCreateWorkerW() (syscall.Handle, error) {
	progman, _, err := procFindWindow.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Progman"))),
		0,
	)
	if progman == 0 {
		return 0, fmt.Errorf("win: FindWindow(Progman) failed: %w", err)
	}
	log.Printf("[DEBUG] Found Progman window: 0x%X", progman)

	// First, try to find a WorkerW in Progman's direct children
	// This is the most reliable method
	var progmanWorkerW syscall.Handle

	cbProgmanChild := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := make([]uint16, 256)
		classLen, _, _ := procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&className[0])), 256)

		if classLen > 0 {
			name := syscall.UTF16ToString(className[:classLen])
			if name == "WorkerW" {
				visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
				if visible != 0 {
					progmanWorkerW = hwnd
					log.Printf("[DEBUG] Found VISIBLE WorkerW as direct child of Progman: 0x%X", hwnd)
					return 0 // Found it, stop enumeration
				}
			}
		}
		return 1
	})

	procEnumChildWindows.Call(uintptr(progman), cbProgmanChild, 0)

	if progmanWorkerW != 0 {
		return progmanWorkerW, nil
	}

	// If no WorkerW found in Progman's children, send the magic message to create one
	log.Printf("[DEBUG] No WorkerW found in Progman children; sending magic message 0x052C")
	ret, _, sendErr := procSendMessageTimeout.Call(
		progman,
		0x052C,
		0, 0,
		sendMsgTimeout,
		5000, // 5 second timeout
		0,
	)
	log.Printf("[DEBUG] SendMessageTimeout returned: %v (err: %v)", ret, sendErr)

	// Give Explorer time to create WorkerW
	time.Sleep(100 * time.Millisecond)

	// Now try again to find WorkerW in Progman's children
	progmanWorkerW = 0
	procEnumChildWindows.Call(uintptr(progman), cbProgmanChild, 0)

	if progmanWorkerW != 0 {
		log.Printf("[DEBUG] Found created WorkerW in Progman children: 0x%X", progmanWorkerW)
		return progmanWorkerW, nil
	}

	// Fallback to global search
	log.Printf("[DEBUG] Fallback: searching globally for any WorkerW window")
	var foundWorkerWs []syscall.Handle
	var visibleWorkerW syscall.Handle

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := make([]uint16, 256)
		classLen, _, _ := procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&className[0])), 256)

		if classLen > 0 {
			className = className[:classLen]
			name := syscall.UTF16ToString(className)
			if name == "WorkerW" {
				foundWorkerWs = append(foundWorkerWs, hwnd)

				// Prefer visible WorkerW
				visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
				if visible != 0 && visibleWorkerW == 0 {
					visibleWorkerW = hwnd
					log.Printf("[DEBUG] Found VISIBLE WorkerW globally: 0x%X", hwnd)
				}
			}
		}
		return 1
	})

	procEnumWindows.Call(cb, 0)

	if visibleWorkerW != 0 {
		log.Printf("[DEBUG] Using globally found visible WorkerW: 0x%X", visibleWorkerW)
		return visibleWorkerW, nil
	}

	if len(foundWorkerWs) > 0 {
		firstWorkerW := foundWorkerWs[0]
		log.Printf("[DEBUG] Using globally found first WorkerW: 0x%X", firstWorkerW)
		return firstWorkerW, nil
	}

	// Last resort: use Progman itself
	log.Printf("[WARNING] No WorkerW found, using Progman as fallback: 0x%X", progman)
	return syscall.Handle(progman), nil
}

// SetParentToWorkerW re-parents hwnd so that it becomes a child of the
// WorkerW desktop background window.
func SetParentToWorkerW(hwnd, workerW syscall.Handle) error {
	ret, _, err := procSetParent.Call(uintptr(hwnd), uintptr(workerW))
	log.Printf("[DEBUG] SetParent(0x%X, 0x%X) returned: 0x%X (err: %v)", hwnd, workerW, ret, err)
	if ret == 0 {
		return fmt.Errorf("win: SetParent failed: %w", err)
	}

	// After SetParent, explicitly ensure WS_VISIBLE style is set
	style, _, _ := procGetWindowLong.Call(uintptr(hwnd), uintptr(gwlStyle))
	newStyle := style | uintptr(wsVisible)
	procSetWindowLong.Call(uintptr(hwnd), uintptr(gwlStyle), newStyle)
	log.Printf("[DEBUG] SetParentToWorkerW: ensured WS_VISIBLE after SetParent (old=0x%X, new=0x%X)", style, newStyle)

	// Try multiple ShowWindow calls with different flags
	procShowWindow.Call(uintptr(hwnd), 5) // SW_SHOW
	procShowWindow.Call(uintptr(hwnd), 9) // SW_RESTORE
	procShowWindow.Call(uintptr(hwnd), 1) // SW_NORMAL
	log.Printf("[DEBUG] SetParentToWorkerW: called ShowWindow with SW_SHOW, SW_RESTORE, SW_NORMAL")

	return nil
}

// MakeFullscreen resizes hwnd to cover the full virtual screen (all monitors).
func MakeFullscreen(hwnd syscall.Handle) error {
	smCxVirtualScreen := uintptr(78) // SM_CXVIRTUALSCREEN
	smCyVirtualScreen := uintptr(79) // SM_CYVIRTUALSCREEN

	w, _, _ := procGetSystemMetrics.Call(smCxVirtualScreen)
	h, _, _ := procGetSystemMetrics.Call(smCyVirtualScreen)

	log.Printf("[DEBUG] MakeFullscreen: resizing to %d x %d", w, h)
	ret, _, err := procMoveWindow.Call(uintptr(hwnd), 0, 0, w, h, 1)
	if ret == 0 {
		return fmt.Errorf("win: MoveWindow failed: %w", err)
	}
	return nil
}

// ApplyChildStyle sets the WS_CHILD | WS_VISIBLE window style on hwnd so it
// integrates cleanly as a child of WorkerW.
func ApplyChildStyle(hwnd syscall.Handle) {
	// Set main window styles: WS_CHILD | WS_VISIBLE
	style, _, _ := procGetWindowLong.Call(uintptr(hwnd), uintptr(gwlStyle))
	newStyle := style | uintptr(wsChild|wsVisible)
	procSetWindowLong.Call(uintptr(hwnd), uintptr(gwlStyle), newStyle)
	log.Printf("[DEBUG] ApplyChildStyle: set style to WS_CHILD|WS_VISIBLE (old=0x%X, new=0x%X)", style, newStyle)

	// Set extended window styles for wallpaper behavior
	// WS_EX_NOACTIVATE: Prevents the window from being activated
	// WS_EX_TRANSPARENT: Makes input events pass through to window below
	exStyle, _, _ := procGetWindowLong.Call(uintptr(hwnd), uintptr(gwlExStyle))
	newExStyle := exStyle | uintptr(wsExNoActivate|wsExTransparent)
	procSetWindowLong.Call(uintptr(hwnd), uintptr(gwlExStyle), newExStyle)
	log.Printf("[DEBUG] ApplyChildStyle: set extended style WS_EX_NOACTIVATE|WS_EX_TRANSPARENT (old=0x%X, new=0x%X)", exStyle, newExStyle)

	// Show the window
	procShowWindow.Call(uintptr(hwnd), 5) // SW_SHOW
	log.Printf("[DEBUG] ApplyChildStyle: called ShowWindow(SW_SHOW)")
}

// PlaceAtBottom positions hwnd at the very bottom of Z-order so desktop icons remain visible.
func PlaceAtBottom(hwnd syscall.Handle) {
	procSetWindowPos.Call(
		uintptr(hwnd),
		uintptr(hwndBottom),
		0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpNoActivate|swpShowWindow),
	)
	log.Printf("[DEBUG] PlaceAtBottom(0x%X) called with swpShowWindow flag", hwnd)
}

// MoveToOrigin ensures the window is at screen position (0, 0).
func MoveToOrigin(hwnd syscall.Handle) {
	ret, _, _ := procMoveWindow.Call(uintptr(hwnd), 0, 0, 1920, 1080, 1)
	log.Printf("[DEBUG] MoveToOrigin: moved to (0, 0), result: %v", ret != 0)
}

// RefreshDesktop forces Windows to redraw the desktop by refreshing Progman and WorkerW.
func RefreshDesktop() {
	log.Printf("[DEBUG] RefreshDesktop: beginning refresh sequence")

	// Find Progman
	progman, _, _ := procFindWindow.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Progman"))),
		0,
	)

	if progman != 0 {
		log.Printf("[DEBUG] Invalidating Progman (0x%X)", progman)
		// Invalidate the entire Progman window
		procInvalidateRect.Call(uintptr(progman), 0, 1)
		// Force redraw with multiple flags
		procRedrawWindow.Call(uintptr(progman), 0, 0, 0x0001|0x0002|0x0100) // RDW_INVALIDATE|RDW_ERASE|RDW_UPDATENOW

		// Send custom repaint message
		procSendMessageTimeout.Call(uintptr(progman), 15, 0, 0, sendMsgTimeout, 1000, 0) // WM_PAINT
	}

	// Find and refresh all WorkerW windows
	var workerWs []syscall.Handle
	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := make([]uint16, 256)
		classLen, _, _ := procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&className[0])), 256)
		if classLen > 0 && syscall.UTF16ToString(className[:classLen]) == "WorkerW" {
			workerWs = append(workerWs, hwnd)
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)

	for i, workerW := range workerWs {
		log.Printf("[DEBUG] Invalidating WorkerW #%d (0x%X)", i, workerW)
		procInvalidateRect.Call(uintptr(workerW), 0, 1)
		procRedrawWindow.Call(uintptr(workerW), 0, 0, 0x0001|0x0002|0x0100)
		procSendMessageTimeout.Call(uintptr(workerW), 15, 0, 0, sendMsgTimeout, 1000, 0) // WM_PAINT
	}

	log.Printf("[DEBUG] RefreshDesktop: refresh sequence complete")
}
