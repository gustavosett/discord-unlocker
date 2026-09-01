package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRoundTripAndExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "cache.json")
	want := []Entry{{IP: "8.8.8.8", Port: 1080, Country: "us", Latency: 125400 * time.Microsecond, VerifiedAt: now.Add(-time.Hour)}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Country != "US" || got[0].Latency != 125*time.Millisecond {
		t.Fatalf("Load() = %#v", got)
	}
	expired, err := Load(path, now.Add(25*time.Hour), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 0 {
		t.Fatalf("cache expirado ainda retornou %#v", expired)
	}
}

func TestLoadDropsUnsafeEntries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "cache.json")
	contents := `{"version":1,"proxies":[{"ip":"127.0.0.1","port":1080,"country":"US","latency_ms":10,"verified_at":"2026-08-31T11:00:00Z"},{"ip":"8.8.8.8","port":1080,"country":"BR","latency_ms":10,"verified_at":"2026-08-31T11:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Load() aceitou entradas inseguras: %#v", got)
	}
}

func TestLoadRejectsMalformedOversizedAndUnknownFields(t *testing.T) {
	t.Parallel()
	for name, contents := range map[string]string{
		"malformed": `{`,
		"unknown":   `{"version":1,"other":true,"proxies":[]}`,
		"trailing":  `{"version":1,"proxies":[]} {}`,
		"oversized": strings.Repeat("x", maxCacheBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path, time.Now(), 24*time.Hour); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("Load() error = %v, want ErrInvalidCache", err)
			}
		})
	}
}

func TestSaveRejectsInvalidAndTooManyEntries(t *testing.T) {
	t.Parallel()
	now := time.Now()
	valid := Entry{IP: "8.8.8.8", Port: 1080, Country: "US", Latency: time.Second, VerifiedAt: now}
	if err := Save(filepath.Join(t.TempDir(), "cache.json"), []Entry{{IP: "localhost", Port: 1080, Country: "US", Latency: time.Second, VerifiedAt: now}}); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("Save(invalid) = %v", err)
	}
	if err := Save(filepath.Join(t.TempDir(), "cache.json"), []Entry{valid, valid, valid, valid}); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("Save(too many) = %v", err)
	}
}
