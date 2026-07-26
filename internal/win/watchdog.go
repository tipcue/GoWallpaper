//go:build windows

package win

import (
	"log"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procOpenProcess             = kernel32.NewProc("OpenProcess")
	procCloseHandle             = kernel32.NewProc("CloseHandle")
)

// EventKind identifies the type of desktop environment change detected.
type EventKind int

const (
	// ExplorerRestarted indicates explorer.exe crashed or was restarted,
	// invalidating the WorkerW window hierarchy.
	ExplorerRestarted EventKind = iota
	// DisplayChanged indicates the virtual screen geometry changed
	// (monitor added/removed, resolution or DPI change).
	DisplayChanged
)

// Event is a notification from the Watchdog about a desktop change.
type Event struct {
	Kind EventKind
}

// Watchdog monitors the desktop environment for changes that require the
// wallpaper window to be reattached or resized. It runs a polling loop in
// a background goroutine and delivers events on the Events channel.
type Watchdog struct {
	// Events receives desktop change notifications. The engine should
	// select on this channel and call Reattach or resize as appropriate.
	Events <-chan Event

	events chan Event
	stop   chan struct{}
	done   chan struct{}
}

// StartWatchdog begins monitoring. The caller must call Stop when done.
func StartWatchdog(interval time.Duration) *Watchdog {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	w := &Watchdog{
		events: make(chan Event, 4),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	w.Events = w.events

	go w.loop(interval)
	return w
}

// Stop terminates the watchdog goroutine and waits for it to exit.
func (w *Watchdog) Stop() {
	close(w.stop)
	<-w.done
}

func (w *Watchdog) loop(interval time.Duration) {
	defer close(w.done)

	lastPID := getExplorerPID()
	lastW, lastH := getVirtualScreenSize()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
		}

		// Check Explorer restart.
		pid := getExplorerPID()
		if pid != 0 && lastPID != 0 && pid != lastPID {
			log.Printf("[INFO] watchdog: explorer PID changed %d → %d", lastPID, pid)
			w.emit(Event{Kind: ExplorerRestarted})
		}
		if pid != 0 {
			lastPID = pid
		}

		// Check display geometry change.
		cw, ch := getVirtualScreenSize()
		if cw != lastW || ch != lastH {
			log.Printf("[INFO] watchdog: virtual screen %dx%d → %dx%d", lastW, lastH, cw, ch)
			w.emit(Event{Kind: DisplayChanged})
			lastW, lastH = cw, ch
		}
	}
}

func (w *Watchdog) emit(e Event) {
	select {
	case w.events <- e:
	default:
		// Drop if the consumer is not keeping up; the next tick will
		// re-detect the condition if it still matters.
	}
}

// getExplorerPID returns the PID that owns the Progman window (i.e. explorer.exe).
// Returns 0 if Progman is not found (e.g. during Explorer restart).
func getExplorerPID() uint32 {
	progman, _, _ := procFindWindow.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Progman"))),
		0,
	)
	if progman == 0 {
		return 0
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(progman, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// getVirtualScreenSize returns the virtual screen width and height in pixels.
func getVirtualScreenSize() (w, h uintptr) {
	smCxVirtualScreen := uintptr(78)
	smCyVirtualScreen := uintptr(79)
	w, _, _ = procGetSystemMetrics.Call(smCxVirtualScreen)
	h, _, _ = procGetSystemMetrics.Call(smCyVirtualScreen)
	return
}

// Reattach re-establishes the wallpaper window's parent-child relationship
// with the WorkerW desktop layer. Call this after an Explorer restart or
// display change to restore the wallpaper without restarting the engine.
//
// hwnd is the GLFW window handle. Reattach performs the full sequence:
// find/create WorkerW → SetParent → MakeFullscreen → PlaceAtBottom.
func Reattach(hwnd syscall.Handle) error {
	workerW, err := FindOrCreateWorkerW()
	if err != nil {
		return err
	}
	if err := SetParentToWorkerW(hwnd, workerW); err != nil {
		return err
	}
	if err := MakeFullscreen(hwnd); err != nil {
		return err
	}
	PlaceAtBottom(hwnd)
	MoveToOrigin(hwnd)
	log.Printf("[INFO] win: reattached hwnd 0x%X to WorkerW 0x%X", hwnd, workerW)
	return nil
}
