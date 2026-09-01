package pac

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDeterministicAndScoped(t *testing.T) {
	t.Parallel()

	endpoints := []Endpoint{
		{IP: "2001:4860:4860::8888", Port: 1081},
		{IP: "8.8.8.8", Port: 1080},
		{IP: "8.8.8.8", Port: 1080},
	}
	first, err := Generate(endpoints)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := Generate(endpoints)
	if err != nil {
		t.Fatalf("Generate() repeated error = %v", err)
	}
	if first != second {
		t.Fatalf("Generate() differs for identical input:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	wantChain := `return "SOCKS5 [2001:4860:4860::8888]:1081; SOCKS5 8.8.8.8:1080; DIRECT";`
	if !strings.Contains(first, wantChain) {
		t.Errorf("Generate() lacks expected proxy chain; got:\n%s", first)
	}
	if strings.Count(first, "gateway.discord.gg") != 2 ||
		!strings.Contains(first, `shExpMatch(host, "gateway-*.discord.gg")`) {
		t.Errorf("Generate() should contain the two fixed hosts and regional gateway pattern; got:\n%s", first)
	}
	for _, forbidden := range []string{"api.discord.gg", "media.discordapp.net", "cdn.discordapp.com"} {
		if strings.Contains(first, forbidden) {
			t.Errorf("Generate() unexpectedly proxies %q; got:\n%s", forbidden, first)
		}
	}
	if strings.Count(first, `return "DIRECT";`) != 1 {
		t.Errorf("Generate() should have one default DIRECT return; got:\n%s", first)
	}
}

func TestGeneratePreservesEndpointPriority(t *testing.T) {
	t.Parallel()

	first, err := Generate([]Endpoint{
		{IP: "8.8.8.8", Port: 1080},
		{IP: "1.1.1.1", Port: 1081},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	reversed, err := Generate([]Endpoint{
		{IP: "1.1.1.1", Port: 1081},
		{IP: "8.8.8.8", Port: 1080},
	})
	if err != nil {
		t.Fatalf("Generate() reversed error = %v", err)
	}
	if first == reversed {
		t.Fatal("Generate() ignored endpoint priority")
	}
	if !strings.Contains(first, "SOCKS5 8.8.8.8:1080; SOCKS5 1.1.1.1:1081; DIRECT") {
		t.Fatalf("Generate() did not preserve endpoint order:\n%s", first)
	}
}

func TestGenerateRejectsUntrustedEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint Endpoint
		want     error
	}{
		{name: "hostname", endpoint: Endpoint{IP: "proxy.example", Port: 1080}, want: ErrInvalidIP},
		{name: "javascript injection", endpoint: Endpoint{IP: `8.8.8.8\"; alert(1); //`, Port: 1080}, want: ErrInvalidIP},
		{name: "private IPv4", endpoint: Endpoint{IP: "10.0.0.1", Port: 1080}, want: ErrInvalidIP},
		{name: "loopback IPv4", endpoint: Endpoint{IP: "127.0.0.1", Port: 1080}, want: ErrInvalidIP},
		{name: "shared IPv4", endpoint: Endpoint{IP: "100.64.0.1", Port: 1080}, want: ErrInvalidIP},
		{name: "documentation IPv4", endpoint: Endpoint{IP: "192.0.2.1", Port: 1080}, want: ErrInvalidIP},
		{name: "private IPv6", endpoint: Endpoint{IP: "fd00::1", Port: 1080}, want: ErrInvalidIP},
		{name: "zoned IPv6", endpoint: Endpoint{IP: "fe80::1%3", Port: 1080}, want: ErrInvalidIP},
		{name: "zero port", endpoint: Endpoint{IP: "8.8.8.8", Port: 0}, want: ErrInvalidPort},
		{name: "large port", endpoint: Endpoint{IP: "8.8.8.8", Port: 65536}, want: ErrInvalidPort},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Generate([]Endpoint{test.endpoint})
			if !errors.Is(err, test.want) {
				t.Fatalf("Generate() error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}

func TestGenerateRejectsEmptyList(t *testing.T) {
	t.Parallel()

	_, err := Generate(nil)
	if !errors.Is(err, ErrNoEndpoints) {
		t.Fatalf("Generate(nil) error = %v, want %v", err, ErrNoEndpoints)
	}
}

func TestGenerateRejectsMoreThanThreeEndpoints(t *testing.T) {
	t.Parallel()

	_, err := Generate([]Endpoint{
		{IP: "1.1.1.1", Port: 1080},
		{IP: "8.8.8.8", Port: 1080},
		{IP: "9.9.9.9", Port: 1080},
		{IP: "208.67.222.222", Port: 1080},
	})
	if !errors.Is(err, ErrTooManyEndpoints) {
		t.Fatalf("Generate() error = %v, want %v", err, ErrTooManyEndpoints)
	}
}

func TestNewEndpointCanonicalizesAddress(t *testing.T) {
	t.Parallel()

	endpoint, err := NewEndpoint("2001:4860:4860:0:0:0:0:8888", 1080)
	if err != nil {
		t.Fatalf("NewEndpoint() error = %v", err)
	}
	if endpoint.IP != "2001:4860:4860::8888" {
		t.Fatalf("NewEndpoint() IP = %q", endpoint.IP)
	}
}

func TestWriteFileReplacesDestinationAndCleansTemporaryFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "discord unlocker.pac")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}

	if err := WriteFile(path, []Endpoint{{IP: "8.8.8.8", Port: 1080}}); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	want, _ := Generate([]Endpoint{{IP: "8.8.8.8", Port: 1080}})
	if string(contents) != want {
		t.Fatalf("destination contents differ:\ngot:\n%s\nwant:\n%s", contents, want)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("temporary file remained after commit: %v", entries)
	}
}

func TestWriteFileDoesNotTouchDestinationForInvalidEndpoint(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "proxy.pac")
	const original = "keep me"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}

	err := WriteFile(path, []Endpoint{{IP: "localhost", Port: 1080}})
	if !errors.Is(err, ErrInvalidIP) {
		t.Fatalf("WriteFile() error = %v, want %v", err, ErrInvalidIP)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(contents) != original {
		t.Fatalf("destination changed to %q", contents)
	}
}

func TestFileDataURLRoundTripsPACWithoutFileScheme(t *testing.T) {
	t.Parallel()

	contents := []byte("function FindProxyForURL(){return 'DIRECT';}\n")
	path := filepath.Join(t.TempDir(), "discord #1.pac")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := FileDataURL(path)
	if err != nil {
		t.Fatalf("FileDataURL() error = %v", err)
	}
	if !strings.HasPrefix(got, dataURLPrefix) {
		t.Fatalf("FileDataURL() = %q, want PAC data URL", got)
	}
	if strings.Contains(got, "file:") || strings.Contains(got, filepath.Base(path)) {
		t.Fatalf("FileDataURL() exposed a file URL or path: %q", got)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, dataURLPrefix))
	if err != nil {
		t.Fatalf("decode FileDataURL(): %v", err)
	}
	if string(decoded) != string(contents) {
		t.Fatalf("decoded PAC = %q, want %q", decoded, contents)
	}
}

func TestDataURLRejectsUnsafeOrOversizedContent(t *testing.T) {
	t.Parallel()

	for _, contents := range [][]byte{nil, {}, {'x', 0}, {0xff}} {
		if _, err := DataURL(contents); !errors.Is(err, ErrInvalidPACContent) {
			t.Fatalf("DataURL(%v) error = %v, want ErrInvalidPACContent", contents, err)
		}
	}
	if _, err := DataURL(make([]byte, maxEmbeddedPACBytes+1)); !errors.Is(err, ErrPACTooLarge) {
		t.Fatalf("oversized DataURL() error = %v, want ErrPACTooLarge", err)
	}
}

func TestFileDataURLRejectsDirectory(t *testing.T) {
	t.Parallel()

	if _, err := FileDataURL(t.TempDir()); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("FileDataURL(directory) error = %v, want ErrInvalidPath", err)
	}
	path := filepath.Join(t.TempDir(), "large.pac")
	if err := os.WriteFile(path, make([]byte, maxEmbeddedPACBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FileDataURL(path); !errors.Is(err, ErrPACTooLarge) {
		t.Fatalf("FileDataURL(large) error = %v, want ErrPACTooLarge", err)
	}
}

func TestEmptyPathRejected(t *testing.T) {
	t.Parallel()

	if _, err := FileDataURL(""); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("FileDataURL(\"\") error = %v, want %v", err, ErrInvalidPath)
	}
	if err := WriteFile("", []Endpoint{{IP: "8.8.8.8", Port: 1080}}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("WriteFile(\"\") error = %v, want %v", err, ErrInvalidPath)
	}
}
