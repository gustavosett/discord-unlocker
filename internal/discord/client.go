package discord

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultBootstrapWindow is how long a directly launched Discord process
	// must remain alive before LaunchWithProxy reports success.
	DefaultBootstrapWindow = 3 * time.Second
)

var (
	// ErrDiscordRunning is returned when a proxy launch is attempted while a
	// Discord Stable process is already running. The client never terminates it.
	ErrDiscordRunning = errors.New("Discord Stable is already running")
	// ErrBootstrapFailed means that the directly launched Discord process exited
	// before the requested bootstrap survival window elapsed.
	ErrBootstrapFailed = errors.New("Discord exited during bootstrap")
	// ErrUnsupportedPlatform marks Windows-only operations on other platforms.
	ErrUnsupportedPlatform = errors.New("Discord process integration is only supported on Windows")
)

// ProcessInfo identifies a Discord Stable process discovered on Windows.
type ProcessInfo struct {
	PID        uint32
	ParentPID  uint32
	Executable string
	ImagePath  string
}

// LaunchResult records exactly what the launcher started. Arguments never
// include the executable itself.
type LaunchResult struct {
	PID        uint32
	Executable string
	Arguments  []string
	ViaUpdater bool
}

// Client performs explicit lifecycle operations for one Discord Stable
// installation. Creating a Client has no process or network side effects.
type Client struct {
	installation Installation
}

// NewClient validates an Installation and returns a side-effect-free client.
func NewClient(installation Installation) (*Client, error) {
	if strings.TrimSpace(installation.RootDir) == "" {
		return nil, errors.New("create Discord client: installation RootDir is empty")
	}
	if strings.TrimSpace(installation.AppDir) == "" {
		return nil, errors.New("create Discord client: installation AppDir is empty")
	}
	if strings.TrimSpace(installation.DiscordExe) == "" {
		return nil, errors.New("create Discord client: installation DiscordExe is empty")
	}
	if !pathWithin(installation.AppDir, installation.RootDir) {
		return nil, fmt.Errorf(
			"create Discord client: app directory %q is outside root %q",
			installation.AppDir,
			installation.RootDir,
		)
	}
	return &Client{installation: installation}, nil
}

// Installation returns the immutable paths used by this Client.
func (client *Client) Installation() Installation {
	return client.installation
}

func pathWithin(path, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if strings.EqualFold(cleanPath, cleanRoot) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(cleanPath), strings.ToLower(cleanRoot+string(filepath.Separator)))
}

func isStableDiscordImage(imagePath, root string) bool {
	if !strings.EqualFold(filepath.Base(imagePath), "Discord.exe") || !pathWithin(imagePath, root) {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(imagePath))
	if err != nil {
		return false
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 2 || !strings.EqualFold(parts[1], "Discord.exe") {
		return false
	}
	if len(parts[0]) <= len("app-") || !strings.EqualFold(parts[0][:len("app-")], "app-") {
		return false
	}
	_, validVersion := parseNumericVersion(parts[0][len("app-"):])
	return validVersion
}
