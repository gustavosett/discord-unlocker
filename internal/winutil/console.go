package winutil

import "strings"

// IsAutostart reports whether args contains an enabled --autostart flag. It
// accepts both --autostart and --autostart=true; --autostart=false is manual.
func IsAutostart(args []string) bool {
	for _, argument := range args {
		if argument == "--autostart" || argument == "-autostart" {
			return true
		}
		for _, prefix := range []string{"--autostart=", "-autostart="} {
			if value, found := strings.CutPrefix(argument, prefix); found {
				return strings.EqualFold(value, "true") || value == "1"
			}
		}
	}
	return false
}

// HideConsoleForAutostart prepares process console behavior from command-line
// arguments. Despite its compatibility name, it also attaches or allocates a
// console in manual mode so a binary linked with -H=windowsgui can show progress.
func HideConsoleForAutostart(args []string) error {
	return PrepareConsole(IsAutostart(args))
}
