package winutil

import "errors"

var (
	// ErrAlreadyRunning means another process still holds the named singleton
	// object. The mutex handle is automatically released if that process exits.
	ErrAlreadyRunning = errors.New("another discord-unlocker instance is already running")
	// ErrUnsupportedPlatform marks Windows-only functionality on another OS.
	ErrUnsupportedPlatform = errors.New("Windows integration is unavailable on this platform")
)
