//go:build windows

package discord

import (
	"errors"
	"os"
	"testing"
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
