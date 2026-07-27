//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const taskName = "GoWallpaper"

// RegisterRecovery creates a Task Scheduler task that starts the app on logon
// and restarts it 1 minute after it terminates unexpectedly.
// This provides crash recovery without a separate watchdog process.
func RegisterRecovery() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Delete existing task first (ignore errors if it doesn't exist).
	_ = exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()

	// Create the task:
	//   /SC ONLOGON   — trigger on user logon
	//   /RL LIMITED   — run without elevation (no UAC prompt)
	//   /RI 1         — restart interval: 1 minute after failure
	//   /TR           — command to run
	//   /F            — force create (overwrite)
	args := []string{
		"/Create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s" --autostart`, exe),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/RI", "1",
		"/F",
	}

	cmd := exec.Command("schtasks", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks create: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// UnregisterRecovery removes the Task Scheduler recovery task.
func UnregisterRecovery() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "task not found" errors.
		if strings.Contains(string(out), "2670") || strings.Contains(string(out), "not found") {
			return nil
		}
		return fmt.Errorf("schtasks delete: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// IsRecoveryRegistered checks whether the recovery task exists.
func IsRecoveryRegistered() bool {
	cmd := exec.Command("schtasks", "/Query", "/TN", taskName)
	return cmd.Run() == nil
}
