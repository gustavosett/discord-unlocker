//go:build !windows

package winutil

// PrepareConsole is a no-op outside Windows so portable test and build tools do
// not need platform branches.
func PrepareConsole(bool) error {
	return nil
}
