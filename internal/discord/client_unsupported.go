//go:build !windows

package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/gustavosett/discord-unlocker/internal/proxy"
)

// RunningProcesses is unavailable outside Windows.
func (client *Client) RunningProcesses() ([]ProcessInfo, error) {
	return nil, ErrUnsupportedPlatform
}

// LaunchWithProxy is unavailable outside Windows.
func (client *Client) LaunchWithProxy(context.Context, proxy.Endpoint, time.Duration) (LaunchResult, error) {
	return LaunchResult{}, ErrUnsupportedPlatform
}

// LaunchDirect is unavailable outside Windows.
func (client *Client) LaunchDirect(context.Context) (LaunchResult, error) {
	return LaunchResult{}, fmt.Errorf("launch Discord directly: %w", ErrUnsupportedPlatform)
}
