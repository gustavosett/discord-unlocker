//go:build windows

package discord

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	th32csSnapProcess              = 0x00000002
	processQueryLimitedInformation = 0x1000
	errorInvalidParameter          = syscall.Errno(87)
	errorNoMoreFiles               = syscall.Errno(18)
	errorNotFound                  = syscall.Errno(1168)
	invalidHandle                  = ^uintptr(0)
)

var (
	kernel32Process                = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot   = kernel32Process.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW            = kernel32Process.NewProc("Process32FirstW")
	procProcess32NextW             = kernel32Process.NewProc("Process32NextW")
	procOpenProcess                = kernel32Process.NewProc("OpenProcess")
	procQueryFullProcessImageNameW = kernel32Process.NewProc("QueryFullProcessImageNameW")
	procProcessIDToSessionID       = kernel32Process.NewProc("ProcessIdToSessionId")
	procCloseHandleProcess         = kernel32Process.NewProc("CloseHandle")
)

type processEntry32 struct {
	Size              uint32
	Usage             uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExecutableFile    [syscall.MAX_PATH]uint16
}

type snapshotProcess struct {
	pid        uint32
	parentPID  uint32
	executable string
}

// RunningProcesses returns every accessible Discord.exe in the current Windows
// session whose image path is below this client's Discord Stable root. PTB,
// Canary, and processes in another signed-in user's session are not matched.
func (client *Client) RunningProcesses() ([]ProcessInfo, error) {
	entries, err := takeProcessSnapshot()
	if err != nil {
		return nil, err
	}
	currentSession, err := processSessionID(uint32(os.Getpid()))
	if err != nil {
		return nil, fmt.Errorf("query current Windows session: %w", err)
	}

	processes := make([]ProcessInfo, 0)
	var queryErrors []error
	for _, entry := range entries {
		if !strings.EqualFold(entry.executable, "Discord.exe") {
			continue
		}
		sessionID, sessionErr := processSessionID(entry.pid)
		if sessionErr != nil {
			if processHasGone(sessionErr) {
				continue
			}
			queryErrors = append(queryErrors, fmt.Errorf(
				"query Windows session for Discord.exe PID %d: %w",
				entry.pid,
				sessionErr,
			))
			continue
		}
		if sessionID != currentSession {
			continue
		}

		imagePath, pathErr := queryProcessImagePath(entry.pid)
		if pathErr != nil {
			if processHasGone(pathErr) {
				continue
			}
			queryErrors = append(queryErrors, fmt.Errorf(
				"query image path for Discord.exe PID %d: %w",
				entry.pid,
				pathErr,
			))
			continue
		}
		if !isStableDiscordImage(imagePath, client.installation.RootDir) {
			continue
		}

		processes = append(processes, ProcessInfo{
			PID:        entry.pid,
			ParentPID:  entry.parentPID,
			Executable: entry.executable,
			ImagePath:  imagePath,
		})
	}

	sort.Slice(processes, func(i, j int) bool { return processes[i].PID < processes[j].PID })
	if len(queryErrors) != 0 {
		return processes, errors.Join(queryErrors...)
	}
	return processes, nil
}

func takeProcessSnapshot() ([]snapshotProcess, error) {
	handle, _, callErr := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if handle == invalidHandle {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %w", normalizedWindowsError(callErr))
	}
	defer procCloseHandleProcess.Call(handle)

	entry := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	result, _, firstErr := procProcess32FirstW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if result == 0 {
		if errors.Is(normalizedWindowsError(firstErr), errorNoMoreFiles) {
			return nil, nil
		}
		return nil, fmt.Errorf("Process32FirstW: %w", normalizedWindowsError(firstErr))
	}

	processes := make([]snapshotProcess, 0, 128)
	for {
		processes = append(processes, snapshotProcess{
			pid:        entry.ProcessID,
			parentPID:  entry.ParentProcessID,
			executable: syscall.UTF16ToString(entry.ExecutableFile[:]),
		})

		entry.Size = uint32(unsafe.Sizeof(processEntry32{}))
		result, _, nextErr := procProcess32NextW.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if result != 0 {
			continue
		}
		if errors.Is(normalizedWindowsError(nextErr), errorNoMoreFiles) {
			break
		}
		return nil, fmt.Errorf("Process32NextW: %w", normalizedWindowsError(nextErr))
	}
	return processes, nil
}

func queryProcessImagePath(pid uint32) (string, error) {
	handle, _, openErr := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return "", normalizedWindowsError(openErr)
	}
	defer procCloseHandleProcess.Call(handle)
	return queryProcessImagePathFromHandle(handle)
}

func queryProcessImagePathFromHandle(handle uintptr) (string, error) {
	buffer := make([]uint16, 32768)
	length := uint32(len(buffer))
	result, _, queryErr := procQueryFullProcessImageNameW.Call(
		handle,
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&length)),
	)
	if result == 0 {
		return "", normalizedWindowsError(queryErr)
	}
	return syscall.UTF16ToString(buffer[:length]), nil
}

func processSessionID(pid uint32) (uint32, error) {
	var sessionID uint32
	result, _, sessionErr := procProcessIDToSessionID.Call(
		uintptr(pid),
		uintptr(unsafe.Pointer(&sessionID)),
	)
	if result == 0 {
		return 0, normalizedWindowsError(sessionErr)
	}
	return sessionID, nil
}

func normalizedWindowsError(err error) error {
	if err == nil {
		return syscall.EINVAL
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return syscall.EINVAL
	}
	return err
}

func processHasGone(err error) bool {
	return errors.Is(err, errorInvalidParameter) || errors.Is(err, errorNotFound)
}

func formatProcessPIDs(processes []ProcessInfo) string {
	ids := make([]string, 0, len(processes))
	for _, process := range processes {
		ids = append(ids, strconv.FormatUint(uint64(process.PID), 10))
	}
	return strings.Join(ids, ",")
}
