package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gustavosett/discord-unlocker/internal/proxy"
)

const (
	schemaVersion = 1
	maxCacheBytes = 64 << 10
	maxEntries    = 3
)

var ErrInvalidCache = errors.New("cache de proxies inválido")

// Entry is deliberately limited to the data required to revalidate a proxy.
// It never contains Discord data, credentials, or request contents.
type Entry struct {
	IP         string
	Port       int
	Country    string
	Latency    time.Duration
	VerifiedAt time.Time
}

type diskCache struct {
	Version int         `json:"version"`
	Proxies []diskEntry `json:"proxies"`
}

type diskEntry struct {
	IP         string    `json:"ip"`
	Port       int       `json:"port"`
	Country    string    `json:"country"`
	LatencyMS  int64     `json:"latency_ms"`
	VerifiedAt time.Time `json:"verified_at"`
}

// Load reads a bounded, versioned cache and drops entries which are stale or
// no longer structurally safe. A missing file is an empty cache.
func Load(path string, now time.Time, ttl time.Duration) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("abrir cache: %w", err)
	}
	defer f.Close()

	contents, err := io.ReadAll(io.LimitReader(f, maxCacheBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ler cache: %w", err)
	}
	if len(contents) > maxCacheBytes {
		return nil, fmt.Errorf("%w: arquivo excede %d bytes", ErrInvalidCache, maxCacheBytes)
	}

	var stored diskCache
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCache, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	if stored.Version != schemaVersion || len(stored.Proxies) > maxEntries {
		return nil, fmt.Errorf("%w: versão ou quantidade inesperada", ErrInvalidCache)
	}

	entries := make([]Entry, 0, len(stored.Proxies))
	seen := make(map[string]struct{}, len(stored.Proxies))
	for _, candidate := range stored.Proxies {
		entry, valid := fromDisk(candidate, now, ttl)
		if !valid {
			continue
		}
		key := fmt.Sprintf("%s:%d", entry.IP, entry.Port)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		entries = append(entries, entry)
	}
	return entries, nil
}

func Save(path string, entries []Entry) error {
	if len(entries) > maxEntries {
		return fmt.Errorf("%w: máximo de %d proxies", ErrInvalidCache, maxEntries)
	}

	stored := diskCache{Version: schemaVersion, Proxies: make([]diskEntry, 0, len(entries))}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		normalized, valid := normalize(entry)
		if !valid {
			return ErrInvalidCache
		}
		key := fmt.Sprintf("%s:%d", normalized.IP, normalized.Port)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		stored.Proxies = append(stored.Proxies, diskEntry{
			IP:         normalized.IP,
			Port:       normalized.Port,
			Country:    normalized.Country,
			LatencyMS:  normalized.Latency.Milliseconds(),
			VerifiedAt: normalized.VerifiedAt.UTC(),
		})
	}

	contents, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("codificar cache: %w", err)
	}
	contents = append(contents, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("criar pasta do cache: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".proxy-cache-*.tmp")
	if err != nil {
		return fmt.Errorf("criar cache temporário: %w", err)
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
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("gravar cache temporário: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sincronizar cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("fechar cache temporário: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("substituir cache: %w", err)
	}
	committed = true
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: dados após o documento", ErrInvalidCache)
		}
		return fmt.Errorf("%w: %v", ErrInvalidCache, err)
	}
	return nil
}

func fromDisk(stored diskEntry, now time.Time, ttl time.Duration) (Entry, bool) {
	entry := Entry{
		IP:         stored.IP,
		Port:       stored.Port,
		Country:    stored.Country,
		Latency:    time.Duration(stored.LatencyMS) * time.Millisecond,
		VerifiedAt: stored.VerifiedAt,
	}
	normalized, valid := normalize(entry)
	if !valid || ttl <= 0 {
		return Entry{}, false
	}
	age := now.Sub(normalized.VerifiedAt)
	if age < -5*time.Minute || age > ttl {
		return Entry{}, false
	}
	return normalized, true
}

func normalize(entry Entry) (Entry, bool) {
	if entry.Port < 1 || entry.Port > 65535 {
		return Entry{}, false
	}
	endpoint, valid := proxy.SanitizeEndpoint(net.JoinHostPort(entry.IP, strconv.Itoa(entry.Port)))
	if !valid {
		return Entry{}, false
	}
	country := strings.ToUpper(strings.TrimSpace(entry.Country))
	if len(country) != 2 || country == "BR" || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return Entry{}, false
	}
	if entry.Latency < time.Millisecond || entry.Latency > time.Minute || entry.VerifiedAt.IsZero() {
		return Entry{}, false
	}
	entry.IP = endpoint.Host
	entry.Port = int(endpoint.Port)
	entry.Country = country
	entry.Latency = entry.Latency.Round(time.Millisecond)
	entry.VerifiedAt = entry.VerifiedAt.UTC()
	return entry, true
}
