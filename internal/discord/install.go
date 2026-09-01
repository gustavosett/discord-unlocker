package discord

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotInstalled means that a complete Discord Stable installation could not
// be found. A complete app directory contains a regular Discord.exe file.
var ErrNotInstalled = errors.New("Discord Stable is not installed")

// Installation describes the Squirrel layout used by Discord Stable on
// Windows. UpdateExe is the expected updater path and may not exist; callers
// that launch Discord directly must fall back to DiscordExe when it is absent.
type Installation struct {
	RootDir    string
	AppDir     string
	DiscordExe string
	UpdateExe  string
	Version    string
}

// FindStable locates Discord Stable below localAppData, normally the value of
// %LOCALAPPDATA%. It selects the highest numeric app-* version that contains a
// regular Discord.exe and ignores partially installed newer versions.
func FindStable(localAppData string) (Installation, error) {
	if strings.TrimSpace(localAppData) == "" {
		return Installation{}, fmt.Errorf("%w: LOCALAPPDATA is empty", ErrNotInstalled)
	}

	absLocalAppData, err := filepath.Abs(localAppData)
	if err != nil {
		return Installation{}, fmt.Errorf("resolve LOCALAPPDATA %q: %w", localAppData, err)
	}
	return findStableInRoot(filepath.Join(absLocalAppData, "Discord"))
}

// FindStableFromEnvironment locates Discord Stable using %LOCALAPPDATA%.
func FindStableFromEnvironment() (Installation, error) {
	return FindStable(os.Getenv("LOCALAPPDATA"))
}

type versionCandidate struct {
	dirName string
	version string
	parts   []string
}

func findStableInRoot(rootDir string) (Installation, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Installation{}, fmt.Errorf("%w: root directory %q does not exist", ErrNotInstalled, rootDir)
		}
		return Installation{}, fmt.Errorf("read Discord Stable root %q: %w", rootDir, err)
	}

	var candidates []versionCandidate
	var skipped []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "app-") {
			continue
		}

		version := strings.TrimPrefix(entry.Name(), "app-")
		parts, ok := parseNumericVersion(version)
		if !ok {
			skipped = append(skipped, entry.Name()+" (invalid version)")
			continue
		}

		exePath := filepath.Join(rootDir, entry.Name(), "Discord.exe")
		info, statErr := os.Stat(exePath)
		if statErr != nil || !info.Mode().IsRegular() {
			skipped = append(skipped, entry.Name()+" (missing Discord.exe)")
			continue
		}

		candidates = append(candidates, versionCandidate{
			dirName: entry.Name(),
			version: version,
			parts:   parts,
		})
	}

	if len(candidates) == 0 {
		detail := ""
		if len(skipped) != 0 {
			sort.Strings(skipped)
			detail = "; skipped: " + strings.Join(skipped, ", ")
		}
		return Installation{}, fmt.Errorf(
			"%w: no complete app-* directory below %q%s",
			ErrNotInstalled,
			rootDir,
			detail,
		)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		comparison := compareNumericVersions(candidates[i].parts, candidates[j].parts)
		if comparison == 0 {
			return candidates[i].dirName < candidates[j].dirName
		}
		return comparison < 0
	})
	selected := candidates[len(candidates)-1]
	appDir := filepath.Join(rootDir, selected.dirName)

	return Installation{
		RootDir:    rootDir,
		AppDir:     appDir,
		DiscordExe: filepath.Join(appDir, "Discord.exe"),
		UpdateExe:  filepath.Join(rootDir, "Update.exe"),
		Version:    selected.version,
	}, nil
}

// parseNumericVersion returns normalized decimal components. Comparing the
// component lengths avoids integer overflow for unexpectedly large versions.
func parseNumericVersion(version string) ([]string, bool) {
	if version == "" {
		return nil, false
	}

	rawParts := strings.Split(version, ".")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if part == "" {
			return nil, false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return nil, false
			}
		}
		part = strings.TrimLeft(part, "0")
		if part == "" {
			part = "0"
		}
		parts = append(parts, part)
	}
	return parts, true
}

func compareNumericVersions(left, right []string) int {
	componentCount := len(left)
	if len(right) > componentCount {
		componentCount = len(right)
	}
	for index := 0; index < componentCount; index++ {
		leftPart := "0"
		if index < len(left) {
			leftPart = left[index]
		}
		rightPart := "0"
		if index < len(right) {
			rightPart = right[index]
		}

		if len(leftPart) < len(rightPart) {
			return -1
		}
		if len(leftPart) > len(rightPart) {
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}
	return 0
}
