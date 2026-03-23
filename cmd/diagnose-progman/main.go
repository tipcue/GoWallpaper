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
	procFindWindow       = user32.NewProc("FindWindowW")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
)

func main() {
	fmt.Println("=== DESKTOP HIERARCHY ===\n")

	// Find Progman
	progman, _, _ := procFindWindow.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Progman"))),
		0,
	)

	fmt.Printf("Progman: 0x%X\n", progman)

	// Enumerate children of Progman
	fmt.Println("Children of Progman:")

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

		fmt.Printf("  [%d] HWND: 0x%X | Class: %s | Text: '%s' | Visible: %v\n",
			childCount, child, class, text, visible != 0)

		return 1
	})

	procEnumChildWindows.Call(uintptr(progman), cbChild, 0)

	if childCount == 0 {
		fmt.Println("  [No children found!]")
	}
}
