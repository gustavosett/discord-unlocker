//go:build windows

package winutil

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const errorAlreadyExists = syscall.Errno(183)

var (
	kernel32Mutex    = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32Mutex.NewProc("CreateMutexW")
	procCloseMutex   = kernel32Mutex.NewProc("CloseHandle")
)

// NamedMutex is a process-lifetime singleton lease backed by a named Windows
// mutex object. The implementation intentionally does not take thread
// ownership; keeping the kernel handle open is sufficient for single-instance
// detection and avoids goroutine/OS-thread ownership problems.
type NamedMutex struct {
	mu     sync.Mutex
	handle syscall.Handle
	name   string
}

// AcquireNamedMutex creates a named Windows mutex object. If an object with the
// same name already exists, it closes the newly returned handle and reports
// ErrAlreadyRunning. Use a Local\\ name for a per-session launcher singleton.
func AcquireNamedMutex(name string) (*NamedMutex, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("acquire named mutex: name is empty")
	}
	namePointer, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("encode named mutex %q: %w", name, err)
	}

	handle, _, createErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePointer)))
	if handle == 0 {
		return nil, fmt.Errorf("create named mutex %q: %w", name, normalizeWinutilError(createErr))
	}
	if errors.Is(createErr, errorAlreadyExists) {
		procCloseMutex.Call(handle)
		return nil, fmt.Errorf("%w: mutex %q already exists", ErrAlreadyRunning, name)
	}

	return &NamedMutex{handle: syscall.Handle(handle), name: name}, nil
}

// Close releases the singleton lease. It is safe to call more than once.
func (mutex *NamedMutex) Close() error {
	if mutex == nil {
		return nil
	}
	mutex.mu.Lock()
	defer mutex.mu.Unlock()
	if mutex.handle == 0 {
		return nil
	}

	handle := mutex.handle
	mutex.handle = 0
	result, _, closeErr := procCloseMutex.Call(uintptr(handle))
	if result == 0 {
		return fmt.Errorf("close named mutex %q: %w", mutex.name, normalizeWinutilError(closeErr))
	}
	return nil
}
