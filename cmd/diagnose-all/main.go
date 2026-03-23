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
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

func main() {
	fmt.Println("=== ALL WINDOWS DIAGNOSTIC ===\n")

	// Enumerate all windows
	windowCount := 0
	var allWindows []syscall.Handle

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		windowCount++
		allWindows = append(allWindows, hwnd)

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

		// Show classes of interest
		if class == "WorkerW" || class == "GLFW30" || class == "Progman" || text == "livewallpaper" {
			visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))

			var rect RECT
			procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))

			fmt.Printf("[%04d] HWND: 0x%X | Class: %-15s | Text: '%s' | Visible: %v | Pos: (%d,%d) Size: %dx%d\n",
				windowCount, hwnd, class, text, visible != 0,
				rect.Left, rect.Top, rect.Right-rect.Left, rect.Bottom-rect.Top)
		}

		return 1
	})

	procEnumWindows.Call(cb, 0)

	fmt.Printf("\n=== Checking children of all WorkerW windows ===\n\n")

	// Now check children of each WorkerW
	for _, hwnd := range allWindows {
		className := make([]uint16, 256)
		classLen, _, _ := procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&className[0])), 256)
		if classLen > 0 && syscall.UTF16ToString(className[:classLen]) == "WorkerW" {
			fmt.Printf("WorkerW 0x%X children:\n", hwnd)

			childCount := 0
			cbChild := syscall.NewCallback(func(child syscall.Handle, lParam uintptr) uintptr {
				childCount++

				className := make([]uint16, 256)
				classLen, _, _ := procGetClassName.Call(uintptr(child), uintptr(unsafe.Pointer(&className[0])), 256)
				class := ""
				if classLen > 0 {
					class = syscall.UTF16ToString(className[:classLen])
				}

				windowText := make([]uint16, 256)
				textLen, _, _ := procGetWindowText.Call(uintptr(child), uintptr(unsafe.Pointer(&windowText[0])), 256)
				text := ""
				if textLen > 0 {
					text = syscall.UTF16ToString(windowText[:textLen])
				}

				visible, _, _ := procIsWindowVisible.Call(uintptr(child))

				var rect RECT
				procGetWindowRect.Call(uintptr(child), uintptr(unsafe.Pointer(&rect)))

				fmt.Printf("  - HWND: 0x%X | Class: %-15s | Text: '%s' | Visible: %v | Size: %dx%d\n",
					child, class, text, visible != 0,
					rect.Right-rect.Left, rect.Bottom-rect.Top)

				return 1
			})

			procEnumChildWindows.Call(uintptr(hwnd), cbChild, 0)

			if childCount == 0 {
				fmt.Printf("  [No children]\n")
			}
			fmt.Println()
		}
	}
}
