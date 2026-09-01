//go:build windows

package discord

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/gustavosett/discord-unlocker/internal/pac"
)

const (
	processSynchronize = 0x00100000
	waitObject0        = 0x00000000
	waitTimeout        = 0x00000102
	waitFailed         = 0xffffffff
)

var (
	procWaitForSingleObject = kernel32Process.NewProc("WaitForSingleObject")
	procGetExitCodeProcess  = kernel32Process.NewProc("GetExitCodeProcess")
)

// LaunchWithPAC starts Discord.exe directly with an embedded data: PAC URL and waits
// until that exact process has survived bootstrapWindow. It refuses to start
// while Stable is already running and never terminates an existing process.
//
// Cancelling ctx only cancels the survival check; it does not kill the newly
// started Discord process. The returned LaunchResult identifies that process
// even when the wait is cancelled.
func (client *Client) LaunchWithPAC(
	ctx context.Context,
	pacPath string,
	bootstrapWindow time.Duration,
) (LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return LaunchResult{}, fmt.Errorf("launch Discord with PAC: %w", err)
	}

	running, err := client.RunningProcesses()
	if err != nil {
		return LaunchResult{}, fmt.Errorf("launch Discord with PAC: detect running processes: %w", err)
	}
	if len(running) != 0 {
		return LaunchResult{}, fmt.Errorf(
			"%w (PIDs %s); call TerminateAll explicitly before PAC launch",
			ErrDiscordRunning,
			formatProcessPIDs(running),
		)
	}

	pacURL, err := pac.FileDataURL(pacPath)
	if err != nil {
		return LaunchResult{}, fmt.Errorf("launch Discord with PAC: %w", err)
	}
	if bootstrapWindow <= 0 {
		bootstrapWindow = DefaultBootstrapWindow
	}

	arguments := []string{"--proxy-pac-url=" + pacURL}
	// Do not set STARTF_USESHOWWINDOW/SW_HIDE for Discord.exe: that also hides
	// Electron's main window. A windowsgui launcher does not create a child
	// console that needs suppressing here.
	command := exec.Command(client.installation.DiscordExe, arguments...)
	command.Dir = client.installation.AppDir
	if err := command.Start(); err != nil {
		return LaunchResult{}, fmt.Errorf(
			"start Discord executable %q with embedded PAC: %w",
			client.installation.DiscordExe,
			err,
		)
	}

	result := LaunchResult{
		PID:        uint32(command.Process.Pid),
		Executable: client.installation.DiscordExe,
		Arguments:  append([]string(nil), arguments...),
		ViaUpdater: false,
	}

	processHandle, _, openErr := procOpenProcess.Call(
		processSynchronize|processQueryLimitedInformation,
		0,
		uintptr(result.PID),
	)
	if processHandle == 0 {
		_ = command.Process.Kill()
		_ = command.Wait()
		return result, fmt.Errorf(
			"open newly started Discord PID %d for bootstrap monitoring: %w",
			result.PID,
			normalizedWindowsError(openErr),
		)
	}
	defer procCloseHandleProcess.Call(processHandle)

	bootstrapTimer := time.NewTimer(bootstrapWindow)
	defer bootstrapTimer.Stop()
	pollTicker := time.NewTicker(100 * time.Millisecond)
	defer pollTicker.Stop()

	for {
		exited, exitCode, waitErr := processExitState(processHandle)
		if waitErr != nil {
			_ = command.Process.Release()
			return result, fmt.Errorf("monitor Discord PID %d during bootstrap: %w", result.PID, waitErr)
		}
		if exited {
			waitProcessErr := command.Wait()
			if waitProcessErr == nil {
				waitProcessErr = errors.New("process exited without an operating-system error")
			}
			return result, fmt.Errorf(
				"%w: PID %d exited with code %d before surviving %s: %v",
				ErrBootstrapFailed,
				result.PID,
				exitCode,
				bootstrapWindow,
				waitProcessErr,
			)
		}

		select {
		case <-ctx.Done():
			releaseErr := command.Process.Release()
			if releaseErr != nil {
				return result, fmt.Errorf(
					"launch Discord PID %d wait cancelled (%v) and process handle release failed: %w",
					result.PID,
					ctx.Err(),
					releaseErr,
				)
			}
			return result, fmt.Errorf("wait for Discord PID %d bootstrap: %w", result.PID, ctx.Err())
		case <-bootstrapTimer.C:
			// Check once more after the timer to avoid declaring success when the
			// process exited concurrently with the survival deadline.
			exited, exitCode, finalErr := processExitState(processHandle)
			if finalErr != nil {
				_ = command.Process.Release()
				return result, fmt.Errorf("final bootstrap check for Discord PID %d: %w", result.PID, finalErr)
			}
			if exited {
				waitProcessErr := command.Wait()
				return result, fmt.Errorf(
					"%w: PID %d exited with code %d at the %s survival deadline: %v",
					ErrBootstrapFailed,
					result.PID,
					exitCode,
					bootstrapWindow,
					waitProcessErr,
				)
			}
			if err := command.Process.Release(); err != nil {
				return result, fmt.Errorf("release surviving Discord PID %d process handle: %w", result.PID, err)
			}
			return result, nil
		case <-pollTicker.C:
		}
	}
}

// LaunchDirect starts Discord normally, preferring Squirrel's
// Update.exe --processStart Discord.exe. If Update.exe is absent or not a
// regular file, it starts the selected Discord.exe directly. This method does
// not wait for bootstrap because the updater is expected to exit after spawning
// Discord.
func (client *Client) LaunchDirect(ctx context.Context) (LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return LaunchResult{}, fmt.Errorf("launch Discord directly: %w", err)
	}

	executable := client.installation.DiscordExe
	arguments := []string(nil)
	workingDirectory := client.installation.AppDir
	viaUpdater := false

	updaterExists, err := regularFileExists(client.installation.UpdateExe)
	if err != nil {
		return LaunchResult{}, fmt.Errorf("inspect Discord updater %q: %w", client.installation.UpdateExe, err)
	}
	if updaterExists {
		executable = client.installation.UpdateExe
		arguments = []string{"--processStart", "Discord.exe"}
		workingDirectory = client.installation.RootDir
		viaUpdater = true
	} else {
		discordExists, discordErr := regularFileExists(client.installation.DiscordExe)
		if discordErr != nil {
			return LaunchResult{}, fmt.Errorf("inspect Discord executable %q: %w", client.installation.DiscordExe, discordErr)
		}
		if !discordExists {
			return LaunchResult{}, fmt.Errorf(
				"launch Discord directly: neither updater %q nor executable %q is a regular file",
				client.installation.UpdateExe,
				client.installation.DiscordExe,
			)
		}
	}

	var command *exec.Cmd
	if viaUpdater {
		// Update.exe is a short-lived Squirrel helper. Hiding the helper prevents
		// a console flash while its spawned Discord GUI remains visible.
		command = newHiddenCommand(executable, arguments...)
	} else {
		command = exec.Command(executable, arguments...)
	}
	command.Dir = workingDirectory
	if err := command.Start(); err != nil {
		return LaunchResult{}, fmt.Errorf("start Discord command %q %q: %w", executable, arguments, err)
	}
	result := LaunchResult{
		PID:        uint32(command.Process.Pid),
		Executable: executable,
		Arguments:  append([]string(nil), arguments...),
		ViaUpdater: viaUpdater,
	}
	if err := command.Process.Release(); err != nil {
		return result, fmt.Errorf("release Discord launcher PID %d process handle: %w", result.PID, err)
	}
	return result, nil
}

func newHiddenCommand(executable string, arguments ...string) *exec.Cmd {
	command := exec.Command(executable, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command
}

func regularFileExists(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func processExitState(handle uintptr) (exited bool, exitCode uint32, err error) {
	result, _, waitErr := procWaitForSingleObject.Call(handle, 0)
	switch result {
	case waitTimeout:
		return false, 0, nil
	case waitObject0:
		var code uint32
		ok, _, codeErr := procGetExitCodeProcess.Call(handle, uintptr(unsafe.Pointer(&code)))
		if ok == 0 {
			return true, 0, normalizedWindowsError(codeErr)
		}
		return true, code, nil
	case waitFailed:
		return false, 0, normalizedWindowsError(waitErr)
	default:
		return false, 0, fmt.Errorf("WaitForSingleObject returned unexpected status %#x", result)
	}
}
