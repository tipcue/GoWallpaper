//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	ole32                   = syscall.NewLazyDLL("ole32.dll")
	procSHBrowseForFolderW  = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
)

// browseInfo mirrors the Win32 BROWSEINFOW structure.
type browseInfo struct {
	HwndOwner      uintptr
	PidlRoot       uintptr
	PszDisplayName uintptr
	LpszTitle      uintptr
	UlFlags        uint32
	Lpfn           uintptr
	LParam         uintptr
	IImage         int32
}

const (
	bifReturnOnlyFSDirs = 0x0001
	bifNewDialogStyle   = 0x0040
	maxPath             = 260
)

// PickFolder opens the native Windows folder browser dialog.
// hwnd is the parent window handle (from webview.Window()).
// Returns the selected directory path, or an empty string if cancelled.
func PickFolder(hwnd uintptr, title string) (string, error) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return "", fmt.Errorf("encode title: %w", err)
	}

	displayName := make([]uint16, maxPath)

	bi := browseInfo{
		HwndOwner:      hwnd,
		PszDisplayName: uintptr(unsafe.Pointer(&displayName[0])),
		LpszTitle:      uintptr(unsafe.Pointer(titlePtr)),
		UlFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle,
	}

	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil // user cancelled
	}
	defer procCoTaskMemFree.Call(pidl)

	pathBuf := make([]uint16, maxPath)
	ret, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&pathBuf[0])))
	if ret == 0 {
		return "", fmt.Errorf("SHGetPathFromIDList failed")
	}

	return syscall.UTF16ToString(pathBuf), nil
}
