//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procEnumWindows      = user32.NewProc("EnumWindows")
	procEnumChildWindows = user32.NewProc("EnumChildWindows")
	procGetClassName     = user32.NewProc("GetClassNameW")
	procGetWindowText    = user32.NewProc("GetWindowTextW")
	procGetParent        = user32.NewProc("GetParent")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

func main() {
	fmt.Println("=== Enhanced WorkerW Diagnostic ===\n")

	// Find first WorkerW
	firstWorkerW := findFirstWorkerW()
	if firstWorkerW == 0 {
		fmt.Println("ERROR: No WorkerW window found!")
		return
	}

	fmt.Printf("First WorkerW: 0x%X\n\n", firstWorkerW)
	fmt.Println("Children of WorkerW:")

	// Enumerate children of first WorkerW
	childCount := 0
	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		childCount++

		className := make([]uint16, 256)
		classLen, _, _ := procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&className[0])), 256)
		class := ""
		if classLen > 0 {
			class = syscall.UTF16ToString(className[:classLen])
		}

		windowText := make([]uint16, 256)
		textLen, _, _ := procGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowText[0])), 256)
		text := ""
		if textLen > 0 {
			text = syscall.UTF16ToString(windowText[:textLen])
		}

		visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))

		var rect RECT
		rectPtr := uintptr(unsafe.Pointer(&rect))
		procGetWindowRect.Call(uintptr(hwnd), rectPtr)

		fmt.Printf("  HWND: 0x%X\n", hwnd)
		fmt.Printf("    Class: %s\n", class)
		fmt.Printf("    Text: '%s'\n", text)
		fmt.Printf("    Visible: %v\n", visible != 0)
		fmt.Printf("    Position: (%d, %d), Size: %dx%d\n",
			rect.Left, rect.Top,
			rect.Right-rect.Left,
			rect.Bottom-rect.Top)
		fmt.Println()

		return 1
	})

	procEnumChildWindows.Call(uintptr(firstWorkerW), cb, 0)

	if childCount == 0 {
		fmt.Println("  [No children found - WorkerW has no child windows]")
	}
	fmt.Printf("\nTotal children: %d\n", childCount)
}

func findFirstWorkerW() syscall.Handle {
	var result syscall.Handle

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := make([]uint16, 256)
		classLen, _, _ := procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&className[0])), 256)

		if classLen > 0 {
			name := syscall.UTF16ToString(className[:classLen])
			if name == "WorkerW" && result == 0 {
				result = hwnd
				return 0 // Stop enumeration
			}
		}
		return 1
	})

	procEnumWindows.Call(cb, 0)
	return result
}
