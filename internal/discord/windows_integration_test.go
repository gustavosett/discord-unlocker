//go:build windows

package discord

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTakeProcessSnapshotIncludesCurrentProcess(t *testing.T) {
	processes, err := takeProcessSnapshot()
	if err != nil {
		t.Fatalf("takeProcessSnapshot() error = %v", err)
	}
	wantPID := uint32(os.Getpid())
	for _, process := range processes {
		if process.pid == wantPID {
			return
		}
	}
	t.Fatalf("current PID %d was not present in process snapshot", wantPID)
}

func TestQueryProcessImagePathForCurrentProcess(t *testing.T) {
	path, err := queryProcessImagePath(uint32(os.Getpid()))
	if err != nil {
		t.Fatalf("queryProcessImagePath() error = %v", err)
	}
	if path == "" {
		t.Fatal("queryProcessImagePath() returned an empty path")
	}
}

func TestProcessSessionIDForCurrentProcess(t *testing.T) {
	if _, err := processSessionID(uint32(os.Getpid())); err != nil {
		t.Fatalf("processSessionID() error = %v", err)
	}
}

func TestRunningProcessesForInstalledStable(t *testing.T) {
	installation, err := FindStableFromEnvironment()
	if errors.Is(err, ErrNotInstalled) {
		t.Skip("Discord Stable is not installed on this test host")
	}
	if err != nil {
		t.Fatalf("FindStableFromEnvironment() error = %v", err)
	}
	client, err := NewClient(installation)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	processes, err := client.RunningProcesses()
	if err != nil {
		t.Fatalf("RunningProcesses() error = %v", err)
	}
	for _, process := range processes {
		if !isStableDiscordImage(process.ImagePath, installation.RootDir) {
			t.Fatalf("RunningProcesses() returned non-Stable image %q", process.ImagePath)
		}
	}
}

func TestTerminateAllHonorsCancelledContextBeforeDiscovery(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.TerminateAll(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("TerminateAll() error = %v, want context.Canceled", err)
	}
}

func TestWaitForProcessReleaseRetriesTransientInspectionError(t *testing.T) {
	transientErr := errors.New("process is disappearing")
	calls := 0
	err := waitForProcessRelease(context.Background(), time.Millisecond, func() ([]ProcessInfo, error) {
		calls++
		if calls == 1 {
			return nil, transientErr
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("waitForProcessRelease() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("waitForProcessRelease() calls = %d, want 2", calls)
	}
}

func TestWaitForProcessReleaseReportsPersistentInspectionErrorAtDeadline(t *testing.T) {
	inspectionErr := errors.New("access denied while process exits")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := waitForProcessRelease(ctx, time.Millisecond, func() ([]ProcessInfo, error) {
		return []ProcessInfo{{PID: 1234}}, inspectionErr
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForProcessRelease() error = %v, want context.DeadlineExceeded", err)
	}
	if !strings.Contains(err.Error(), "remaining PIDs 1234") ||
		!strings.Contains(err.Error(), inspectionErr.Error()) {
		t.Fatalf("waitForProcessRelease() error = %q, want PID and inspection detail", err)
	}
}

func TestProcessRevalidationRejectsChangedImage(t *testing.T) {
	client := newTestClient(t)
	handle, stable, err := client.openVerifiedStableProcess(uint32(os.Getpid()))
	if handle != 0 {
		procCloseHandleProcess.Call(handle)
		t.Fatalf("openVerifiedStableProcess() returned unexpected handle %#x", handle)
	}
	if stable || err == nil {
		t.Fatalf("openVerifiedStableProcess() = (%#x, %v, %v), want changed-image error", handle, stable, err)
	}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Discord")
	client, err := NewClient(Installation{
		RootDir:    root,
		AppDir:     filepath.Join(root, "app-1.0.0"),
		DiscordExe: filepath.Join(root, "app-1.0.0", "Discord.exe"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
