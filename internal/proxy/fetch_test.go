package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseProxyScrapeShufflesSanitizedSetBeforeLimit(t *testing.T) {
	t.Parallel()
	lines := []string{"not-an-endpoint", "127.0.0.1:1", "8.0.0.1:1001"}
	for i := 2; i <= 50; i++ {
		lines = append(lines, fmt.Sprintf("8.0.0.%d:%d", i, 1000+i))
	}
	lines = append(lines, "8.0.0.50:1050")

	reverse := func(endpoints []Endpoint) error {
		for left, right := 0, len(endpoints)-1; left < right; left, right = left+1, right-1 {
			endpoints[left], endpoints[right] = endpoints[right], endpoints[left]
		}
		return nil
	}
	got, err := parseProxyScrape([]byte(strings.Join(lines, "\r\n")), 40, reverse)
	if err != nil {
		t.Fatalf("parseProxyScrape() error = %v", err)
	}
	if len(got) != 40 {
		t.Fatalf("parseProxyScrape() returned %d endpoints, want 40", len(got))
	}
	if got[0].Host != "8.0.0.50" || got[39].Host != "8.0.0.11" {
		t.Fatalf("limit was applied before shuffle: first=%s last=%s", got[0].Host, got[39].Host)
	}
}

func TestFetchProxyScrapeBoundsBodyAndRefusesRedirect(t *testing.T) {
	t.Parallel()
	t.Run("body limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Length", "1024")
			_, _ = writer.Write([]byte(strings.Repeat("x", 1024)))
		}))
		defer server.Close()
		_, err := FetchProxyScrape(context.Background(), server.Client(), server.URL, 32, 40)
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("FetchProxyScrape() error = %v, want %v", err, ErrResponseTooLarge)
		}
	})

	t.Run("redirect", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "/elsewhere", http.StatusFound)
		}))
		defer server.Close()
		_, err := FetchProxyScrape(context.Background(), server.Client(), server.URL, 1024, 40)
		if err == nil || !strings.Contains(err.Error(), "redirect refused") {
			t.Fatalf("FetchProxyScrape() error = %v, want redirect refusal", err)
		}
	})
}

func TestParseCountryTrace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "valid", body: "fl=abc\nloc=US\ntls=TLSv1.3\n", want: "US"},
		{name: "normalized", body: "loc=jp\n", want: "JP"},
		{name: "Brazil", body: "loc=BR\n", wantErr: true},
		{name: "unknown", body: "loc=XX\n", wantErr: true},
		{name: "non ISO", body: "loc=T1\n", wantErr: true},
		{name: "missing", body: "ip=1.2.3.4\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCountryTrace([]byte(test.body))
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("parseCountryTrace(%q) = %q, %v; want %q, error=%v", test.body, got, err, test.want, test.wantErr)
			}
		})
	}
}
