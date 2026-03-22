//go:build windows

// Package win manages the Windows desktop WorkerW window hierarchy.
package win

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// HWNDFromGLFW extracts the native Win32 HWND from a GLFW window.
// It uses GLFW's native access API (glfwGetWin32Window) via CGo.
//
// The returned handle can be passed to SetParentToWorkerW and MakeFullscreen.
func HWNDFromGLFW(w *glfw.Window) (syscall.Handle, error) {
	// GetWin32Window() returns a C.HWND (a CGo pointer type).
	// Convert through unsafe.Pointer → uintptr → syscall.Handle.
	hwnd := w.GetWin32Window()
	h := uintptr(unsafe.Pointer(hwnd))
	if h == 0 {
		return 0, fmt.Errorf("win: GetWin32Window returned NULL")
	}
	return syscall.Handle(h), nil
}
