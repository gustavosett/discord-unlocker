//go:build !windows

package winutil

// NamedMutex is an unavailable Windows named mutex on non-Windows systems.
type NamedMutex struct{}

// AcquireNamedMutex is unavailable outside Windows.
func AcquireNamedMutex(string) (*NamedMutex, error) {
	return nil, ErrUnsupportedPlatform
}

// Close is a no-op for a nil/unavailable non-Windows mutex.
func (mutex *NamedMutex) Close() error {
	return nil
}
