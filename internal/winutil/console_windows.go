//go:build windows

package winutil

import (
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"
)

const (
	attachParentProcess = ^uint32(0)
	errorAccessDenied   = syscall.Errno(5)
	errorInvalidHandle  = syscall.Errno(6)
	stdOutputHandle     = ^uint32(10) // (DWORD)-11
	stdErrorHandle      = ^uint32(11) // (DWORD)-12
)

var (
	kernel32Console      = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow = kernel32Console.NewProc("GetConsoleWindow")
	procAttachConsole    = kernel32Console.NewProc("AttachConsole")
	procAllocConsole     = kernel32Console.NewProc("AllocConsole")
	procFreeConsole      = kernel32Console.NewProc("FreeConsole")
	procSetStdHandle     = kernel32Console.NewProc("SetStdHandle")
)

// PrepareConsole configures a binary linked with -H=windowsgui. Silent mode
// detaches from any inherited console and redirects stdout/stderr to NUL.
// Manual mode attaches to the parent console when available, otherwise opens a
// new console, then reopens stdout/stderr on CONOUT$.
func PrepareConsole(silent bool) error {
	if silent {
		return prepareSilentConsole()
	}

	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow == 0 {
		attached, _, attachErr := procAttachConsole.Call(uintptr(attachParentProcess))
		if attached == 0 && !errors.Is(attachErr, errorAccessDenied) {
			allocated, _, allocErr := procAllocConsole.Call()
			if allocated == 0 {
				return fmt.Errorf(
					"attach to parent console (%v), then allocate console: %w",
					normalizeWinutilError(attachErr),
					normalizeWinutilError(allocErr),
				)
			}
		}
	}
	return reopenConsoleOutput()
}

func prepareSilentConsole() error {
	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow != 0 {
		detached, _, detachErr := procFreeConsole.Call()
		if detached == 0 && !errors.Is(detachErr, errorInvalidHandle) {
			return fmt.Errorf("detach autostart console: %w", normalizeWinutilError(detachErr))
		}
	}

	nullOutput, err := os.OpenFile("NUL", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open NUL for silent stdout/stderr: %w", err)
	}
	os.Stdout = nullOutput
	os.Stderr = nullOutput
	log.SetOutput(nullOutput)
	return nil
}

func reopenConsoleOutput() error {
	stdout, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open CONOUT$ for stdout: %w", err)
	}
	stderr, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		_ = stdout.Close()
		return fmt.Errorf("open CONOUT$ for stderr: %w", err)
	}

	if result, _, setErr := procSetStdHandle.Call(uintptr(stdOutputHandle), stdout.Fd()); result == 0 {
		_ = stdout.Close()
		_ = stderr.Close()
		return fmt.Errorf("set console stdout handle: %w", normalizeWinutilError(setErr))
	}
	if result, _, setErr := procSetStdHandle.Call(uintptr(stdErrorHandle), stderr.Fd()); result == 0 {
		_ = stdout.Close()
		_ = stderr.Close()
		return fmt.Errorf("set console stderr handle: %w", normalizeWinutilError(setErr))
	}

	os.Stdout = stdout
	os.Stderr = stderr
	log.SetOutput(stderr)
	return nil
}

func normalizeWinutilError(err error) error {
	if err == nil {
		return syscall.EINVAL
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return syscall.EINVAL
	}
	return err
}
