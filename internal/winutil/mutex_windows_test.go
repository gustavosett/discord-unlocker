//go:build windows

package winutil

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestNamedMutexPreventsConcurrentAcquisitionAndCanBeReacquired(t *testing.T) {
	name := fmt.Sprintf(`Local\discord-unlocker-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	first, err := AcquireNamedMutex(name)
	if err != nil {
		t.Fatalf("first AcquireNamedMutex() error = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	second, err := AcquireNamedMutex(name)
	if !errors.Is(err, ErrAlreadyRunning) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second AcquireNamedMutex() error = %v, want ErrAlreadyRunning", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	reacquired, err := AcquireNamedMutex(name)
	if err != nil {
		t.Fatalf("AcquireNamedMutex() after Close error = %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatalf("reacquired Close() error = %v", err)
	}
}
