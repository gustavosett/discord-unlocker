package discord

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindStableSelectsHighestCompleteNumericVersion(t *testing.T) {
	localAppData := t.TempDir()
	root := filepath.Join(localAppData, "Discord")
	writeTestFile(t, filepath.Join(root, "Update.exe"))
	writeTestFile(t, filepath.Join(root, "app-1.0.9", "Discord.exe"))
	writeTestFile(t, filepath.Join(root, "app-1.0.10", "Discord.exe"))
	writeTestFile(t, filepath.Join(root, "app-1.0.0008", "Discord.exe"))
	writeTestFile(t, filepath.Join(root, "app-preview", "Discord.exe"))

	installation, err := FindStable(localAppData)
	if err != nil {
		t.Fatalf("FindStable() error = %v", err)
	}
	if installation.Version != "1.0.10" {
		t.Fatalf("Version = %q, want %q", installation.Version, "1.0.10")
	}
	if installation.AppDir != filepath.Join(root, "app-1.0.10") {
		t.Errorf("AppDir = %q", installation.AppDir)
	}
	if installation.DiscordExe != filepath.Join(root, "app-1.0.10", "Discord.exe") {
		t.Errorf("DiscordExe = %q", installation.DiscordExe)
	}
	if installation.UpdateExe != filepath.Join(root, "Update.exe") {
		t.Errorf("UpdateExe = %q", installation.UpdateExe)
	}
}

func TestFindStableSkipsIncompleteNewestVersion(t *testing.T) {
	localAppData := t.TempDir()
	root := filepath.Join(localAppData, "Discord")
	writeTestFile(t, filepath.Join(root, "app-1.0.9255", "Discord.exe"))
	if err := os.MkdirAll(filepath.Join(root, "app-1.0.9999"), 0o755); err != nil {
		t.Fatal(err)
	}

	installation, err := FindStable(localAppData)
	if err != nil {
		t.Fatalf("FindStable() error = %v", err)
	}
	if installation.Version != "1.0.9255" {
		t.Fatalf("Version = %q, want %q", installation.Version, "1.0.9255")
	}
}

func TestFindStableReportsIncompleteInstallation(t *testing.T) {
	localAppData := t.TempDir()
	root := filepath.Join(localAppData, "Discord")
	if err := os.MkdirAll(filepath.Join(root, "app-1.0.9999"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "app-not-a-version", "Discord.exe"))

	_, err := FindStable(localAppData)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("FindStable() error = %v, want ErrNotInstalled", err)
	}
	if !strings.Contains(err.Error(), "app-1.0.9999") || !strings.Contains(err.Error(), "app-not-a-version") {
		t.Fatalf("error does not identify skipped installations: %v", err)
	}
}

func TestFindStableKeepsExpectedUpdaterPathWhenUpdaterIsMissing(t *testing.T) {
	localAppData := t.TempDir()
	root := filepath.Join(localAppData, "Discord")
	writeTestFile(t, filepath.Join(root, "app-2.0.0", "Discord.exe"))

	installation, err := FindStable(localAppData)
	if err != nil {
		t.Fatalf("FindStable() error = %v", err)
	}
	if installation.UpdateExe != filepath.Join(root, "Update.exe") {
		t.Fatalf("UpdateExe = %q", installation.UpdateExe)
	}
}

func TestCompareNumericVersionsHandlesLargeComponentsAndTrailingZeroes(t *testing.T) {
	large, ok := parseNumericVersion("1.184467440737095516160")
	if !ok {
		t.Fatal("large version was rejected")
	}
	small, _ := parseNumericVersion("1.999")
	if compareNumericVersions(large, small) <= 0 {
		t.Fatal("large version component did not sort above small component")
	}

	left, _ := parseNumericVersion("1.2")
	right, _ := parseNumericVersion("1.2.0.0")
	if compareNumericVersions(left, right) != 0 {
		t.Fatal("trailing zero components should compare equal")
	}
}

func TestIsStableDiscordImageRequiresExecutableAndRootBoundary(t *testing.T) {
	root := filepath.Join("C:\\", "Users", "person", "AppData", "Local", "Discord")
	if !isStableDiscordImage(filepath.Join(root, "app-1.0.1", "Discord.exe"), root) {
		t.Fatal("valid Stable Discord image was rejected")
	}
	if isStableDiscordImage(filepath.Join(root, "app-1.0.1", "Update.exe"), root) {
		t.Fatal("non-Discord executable was accepted")
	}
	if isStableDiscordImage(filepath.Join(root+"Canary", "app-1.0.1", "Discord.exe"), root) {
		t.Fatal("sibling installation sharing the root prefix was accepted")
	}
	if isStableDiscordImage(filepath.Join(root, "tools", "Discord.exe"), root) {
		t.Fatal("Discord.exe outside a numeric app-* directory was accepted")
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}
