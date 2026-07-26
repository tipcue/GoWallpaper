//go:build windows

package app

import (
	"log"
	"syscall"
)

var (
	kernel32Shutdown              = syscall.NewLazyDLL("kernel32.dll")
	procSetProcessShutdownParameters = kernel32Shutdown.NewProc("SetProcessShutdownParameters")
	procDisableProcessWindowsGhosting = kernel32Shutdown.NewProc("DisableProcessWindowsGhosting")
)

// PrepareShutdown configures the process for graceful system shutdown.
// Call this early in main() before creating any windows.
//
// It sets a high shutdown priority so the app is notified early during
// logoff/shutdown, and disables the "Not Responding" ghost overlay that
// Windows shows if cleanup takes more than a few seconds.
func PrepareShutdown() {
	// Level 0x4FF = high-priority shutdown (range 0x000–0x4FF).
	// Flags 0 = no special behavior.
	ret, _, err := procSetProcessShutdownParameters.Call(0x4FF, 0)
	if ret == 0 {
		log.Printf("[WARN] SetProcessShutdownParameters failed: %v", err)
	}

	// Prevent the "Not Responding" ghost window during GL/FFmpeg cleanup.
	procDisableProcessWindowsGhosting.Call()
}
