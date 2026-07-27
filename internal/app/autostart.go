//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32          = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey   = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keySetValue     = 0x0002
	keyQueryValue   = 0x0001
	regSz           = 1 // REG_SZ type

	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	appName    = "GoWallpaper"
)

// EnableAutostart registers the application to start on user logon by writing
// to HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
// The registered command includes --autostart so the app can start minimized.
func EnableAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	cmdLine := fmt.Sprintf(`"%s" --autostart`, exe)
	return setRegistryString(runKeyPath, appName, cmdLine)
}

// DisableAutostart removes the autostart registry entry.
func DisableAutostart() error {
	return deleteRegistryValue(runKeyPath, appName)
}

// IsAutostartEnabled reports whether the autostart registry entry exists
// and points to the current executable.
func IsAutostartEnabled() bool {
	val, err := getRegistryString(runKeyPath, appName)
	if err != nil || val == "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(val, filepath.Base(exe))
}

// ── Registry helpers ─────────────────────────────────────────────────────

func openKey(subKey string, access uint32) (syscall.Handle, error) {
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return 0, err
	}
	var hKey syscall.Handle
	ret, _, callErr := procRegOpenKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		uintptr(access),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return 0, fmt.Errorf("RegOpenKeyEx(%s): %w", subKey, callErr)
	}
	return hKey, nil
}

func setRegistryString(subKey, name, value string) error {
	hKey, err := openKey(subKey, keySetValue)
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	namePtr, _ := syscall.UTF16PtrFromString(name)
	valueUTF16, err := syscall.UTF16FromString(value)
	if err != nil {
		return fmt.Errorf("encode value: %w", err)
	}
	valueBytes := len(valueUTF16) * 2 // includes null terminator from UTF16FromString

	ret, _, callErr := procRegSetValueExW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(namePtr)),
		0,
		regSz,
		uintptr(unsafe.Pointer(&valueUTF16[0])),
		uintptr(valueBytes),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueEx(%s): %w", name, callErr)
	}
	return nil
}

func deleteRegistryValue(subKey, name string) error {
	hKey, err := openKey(subKey, keySetValue)
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	namePtr, _ := syscall.UTF16PtrFromString(name)
	ret, _, callErr := procRegDeleteValueW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(namePtr)),
	)
	if ret != 0 && ret != 2 { // ERROR_FILE_NOT_FOUND is OK (already removed)
		return fmt.Errorf("RegDeleteValue(%s): %w", name, callErr)
	}
	return nil
}

func getRegistryString(subKey, name string) (string, error) {
	hKey, err := openKey(subKey, keyQueryValue)
	if err != nil {
		return "", err
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	namePtr, _ := syscall.UTF16PtrFromString(name)
	buf := make([]uint16, 1024)
	bufSize := uint32(len(buf) * 2)
	var valType uint32

	ret, _, _ := procRegQueryValueExW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(namePtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueEx(%s): error %d", name, ret)
	}
	return syscall.UTF16ToString(buf), nil
}
