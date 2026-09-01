package pac

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

const (
	dataURLPrefix       = "data:application/x-ns-proxy-autoconfig;base64,"
	maxEmbeddedPACBytes = 8 * 1024
)

var (
	ErrInvalidPath       = errors.New("path must name a file")
	ErrInvalidPACContent = errors.New("PAC content must be non-empty UTF-8 text without NUL bytes")
	ErrPACTooLarge       = errors.New("PAC content is too large to embed in the Windows command line")
)

// WriteFile atomically replaces path with a PAC generated from endpoints. The
// temporary file is created beside the destination so the final rename cannot
// cross filesystem boundaries.
func WriteFile(path string, endpoints []Endpoint) error {
	if path == "" {
		return ErrInvalidPath
	}

	contents, err := Generate(endpoints)
	if err != nil {
		return err
	}

	directory := filepath.Dir(path)
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return ErrInvalidPath
	}

	temporary, err := os.CreateTemp(directory, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.WriteString(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}

	committed = true
	return nil
}

// DataURL embeds a PAC program in a URL accepted by Chromium's PAC fetcher.
// Base64 prevents PAC text from being interpreted as command-line syntax.
// The size limit leaves ample room under Windows' command-line length limit.
func DataURL(contents []byte) (string, error) {
	if len(contents) > maxEmbeddedPACBytes {
		return "", ErrPACTooLarge
	}
	if len(contents) == 0 || !utf8.Valid(contents) || bytes.IndexByte(contents, 0) >= 0 {
		return "", ErrInvalidPACContent
	}
	return dataURLPrefix + base64.StdEncoding.EncodeToString(contents), nil
}

// FileDataURL reads a regular PAC file with a strict size bound and embeds it
// in a data: URL. Current Chromium rejects file:// PAC URLs, so callers should
// use this representation when launching Electron.
func FileDataURL(path string) (string, error) {
	if path == "" {
		return "", ErrInvalidPath
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open PAC file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect PAC file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", ErrInvalidPath
	}
	if info.Size() > maxEmbeddedPACBytes {
		return "", ErrPACTooLarge
	}

	contents, err := io.ReadAll(io.LimitReader(file, maxEmbeddedPACBytes+1))
	if err != nil {
		return "", fmt.Errorf("read PAC file: %w", err)
	}
	return DataURL(contents)
}
