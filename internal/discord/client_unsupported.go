//go:build !windows

package discord

import (
	"context"
	"fmt"
	"time"
)

// RunningProcesses is unavailable outside Windows.
func (client *Client) RunningProcesses() ([]ProcessInfo, error) {
	return nil, ErrUnsupportedPlatform
}

// WaitForExit is unavailable outside Windows.
func (client *Client) WaitForExit(context.Context, time.Duration) error {
	return ErrUnsupportedPlatform
}

// TerminateAll is unavailable outside Windows.
func (client *Client) TerminateAll(context.Context, time.Duration) error {
	return ErrUnsupportedPlatform
}

// LaunchWithPAC is unavailable outside Windows.
func (client *Client) LaunchWithPAC(context.Context, string, time.Duration) (LaunchResult, error) {
	return LaunchResult{}, ErrUnsupportedPlatform
}

// LaunchDirect is unavailable outside Windows.
func (client *Client) LaunchDirect(context.Context) (LaunchResult, error) {
	return LaunchResult{}, fmt.Errorf("launch Discord directly: %w", ErrUnsupportedPlatform)
}
