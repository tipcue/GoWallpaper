//go:build windows

package engine

import (
	"log"
	"syscall"
	"time"
	"unsafe"
)

var (
	suspendUser32 = syscall.NewLazyDLL("user32.dll")

	procGetForegroundWindow = suspendUser32.NewProc("GetForegroundWindow")
	procGetWindowRect       = suspendUser32.NewProc("GetWindowRect")
	procGetSystemMetrics    = suspendUser32.NewProc("GetSystemMetrics")
	procGetWindowLongW      = suspendUser32.NewProc("GetWindowLongW")
	procIsWindowVisible     = suspendUser32.NewProc("IsWindowVisible")
)

// rect mirrors the Win32 RECT structure.
type rect struct {
	Left, Top, Right, Bottom int32
}

// FullscreenDetector polls the foreground window and reports whether a
// fullscreen application is covering the screen. When fullscreen is detected,
// the wallpaper engine can pause rendering to free GPU resources.
type FullscreenDetector struct {
	// Suspended receives true when a fullscreen app appears, false when it
	// disappears. Buffered so the detector never blocks.
	Suspended <-chan bool

	suspended chan bool
	stop      chan struct{}
	done      chan struct{}
}

// StartFullscreenDetector begins polling at the given interval.
// The caller must call Stop when done.
func StartFullscreenDetector(interval time.Duration) *FullscreenDetector {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	d := &FullscreenDetector{
		suspended: make(chan bool, 1),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	d.Suspended = d.suspended

	go d.loop(interval)
	return d
}

// Stop terminates the detector goroutine.
func (d *FullscreenDetector) Stop() {
	close(d.stop)
	<-d.done
}

func (d *FullscreenDetector) loop(interval time.Duration) {
	defer close(d.done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	wasSuspended := false

	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
		}

		isFS := isFullscreenAppActive()
		if isFS != wasSuspended {
			wasSuspended = isFS
			if isFS {
				log.Printf("[INFO] suspend: fullscreen app detected, pausing wallpaper")
			} else {
				log.Printf("[INFO] suspend: fullscreen app closed, resuming wallpaper")
			}
			select {
			case d.suspended <- isFS:
			default:
			}
		}
	}
}

// isFullscreenAppActive returns true if the current foreground window covers
// the entire virtual screen and is not the desktop shell itself.
func isFullscreenAppActive() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}

	// Ignore if the foreground window is not visible.
	vis, _, _ := procIsWindowVisible.Call(hwnd)
	if vis == 0 {
		return false
	}

	// Skip the desktop shell windows (Progman, WorkerW) — they are always
	// "fullscreen" but should not trigger suspension.
	if isShellWindow(hwnd) {
		return false
	}

	var r rect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return false
	}

	// Get virtual screen dimensions.
	smCxVirtualScreen := uintptr(78)
	smCyVirtualScreen := uintptr(79)
	screenW, _, _ := procGetSystemMetrics.Call(smCxVirtualScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyVirtualScreen)

	winW := int32(screenW)
	winH := int32(screenH)

	// The window must cover the full screen (allow small tolerance for
	// taskbar auto-hide rounding).
	return r.Left <= 0 && r.Top <= 0 &&
		r.Right >= winW && r.Bottom >= winH
}

// isShellWindow checks if hwnd belongs to the desktop shell by class name.
func isShellWindow(hwnd uintptr) bool {
	className := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&className[0])), 256)
	if n == 0 {
		return false
	}
	name := syscall.UTF16ToString(className[:n])
	switch name {
	case "Progman", "WorkerW", "SHELLDLL_DefView", "SysListView32":
		return true
	}
	return false
}

var procGetClassNameW = suspendUser32.NewProc("GetClassNameW")
