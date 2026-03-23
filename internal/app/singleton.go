package app

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"unsafe"
)

var (
	kernel32DLL             = syscall.MustLoadDLL("kernel32.dll")
	procCreateMutexW        = kernel32DLL.MustFindProc("CreateMutexW")
	procWaitForSingleObject = kernel32DLL.MustFindProc("WaitForSingleObject")
	procReleaseMutex        = kernel32DLL.MustFindProc("ReleaseMutex")
	procCloseHandle         = kernel32DLL.MustFindProc("CloseHandle")
)

const (
	INFINITE      = 0xFFFFFFFF
	WAIT_OBJECT_0 = 0
	WAIT_TIMEOUT  = 258
)

// Mutex is a Windows named mutex for process synchronization.
type Mutex struct {
	handle syscall.Handle
}

// acquireMutex creates or opens a named mutex and locks it.
// Only one process can hold the lock at a time.
func acquireMutex(name string) (*Mutex, error) {
	mutexName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("encode mutex name: %w", err)
	}

	// CreateMutexW(lpMutexAttributes, bInitialOwner, lpName)
	// Returns: Handle (uintptr), Error
	ret, _, err := procCreateMutexW.Call(
		0,                                  // lpMutexAttributes (NULL)
		0,                                  // bInitialOwner (FALSE)
		uintptr(unsafe.Pointer(mutexName)), // lpName
	)

	if ret == 0 {
		return nil, fmt.Errorf("create mutex failed: %w", err)
	}

	handle := syscall.Handle(ret)

	// WaitForSingleObject(hHandle, dwMilliseconds)
	// Returns: WAIT_OBJECT_0 (0) = acquired, other = failed
	// Using 5000ms timeout to allow recovery from crashed instances
	waitRet, _, waitErr := procWaitForSingleObject.Call(uintptr(handle), 5000)

	if waitRet != WAIT_OBJECT_0 {
		procCloseHandle.Call(uintptr(handle))
		return nil, fmt.Errorf("mutex already held by another process (wait result: %d): %w", waitRet, waitErr)
	}

	log.Printf("[INFO] Singleton mutex acquired: %s", name)
	return &Mutex{handle: handle}, nil
}

// Release unlocks and closes the mutex.
func (m *Mutex) Release() error {
	if m.handle != 0 {
		procReleaseMutex.Call(uintptr(m.handle))
		procCloseHandle.Call(uintptr(m.handle))
		m.handle = 0
	}
	return nil
}

var (
	singletonMutex *Mutex
	mutexOnce      sync.Once
	mutexErr       error
)

// EnsureSingleInstance checks if another instance is already running.
// If so, returns an error. Must be called early in main().
func EnsureSingleInstance() error {
	mutexOnce.Do(func() {
		singletonMutex, mutexErr = acquireMutex("Global\\GoWallpaper_SingleInstance")
	})
	return mutexErr
}

// CloseSingleInstance releases the mutex (called at program exit).
func CloseSingleInstance() error {
	if singletonMutex != nil {
		return singletonMutex.Release()
	}
	return nil
}
