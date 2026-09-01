package proxy

import "testing"

func TestSanitizeEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		record   string
		wantOK   bool
		wantHost string
		wantPort uint16
	}{
		{name: "IPv4", record: " 8.8.8.8:1080\r", wantOK: true, wantHost: "8.8.8.8", wantPort: 1080},
		{name: "IPv6", record: "[2001:4860:4860:0:0:0:0:8888]:443", wantOK: true, wantHost: "2001:4860:4860::8888", wantPort: 443},
		{name: "hostname", record: "proxy.example:1080"},
		{name: "scheme", record: "socks5://8.8.8.8:1080"},
		{name: "credentials", record: "user:pass@8.8.8.8:1080"},
		{name: "zero port", record: "8.8.8.8:0"},
		{name: "large port", record: "8.8.8.8:65536"},
		{name: "private", record: "10.0.0.1:1080"},
		{name: "loopback", record: "127.0.0.1:1080"},
		{name: "shared", record: "100.64.0.1:1080"},
		{name: "link local", record: "169.254.1.1:1080"},
		{name: "benchmark", record: "198.18.0.1:1080"},
		{name: "documentation IPv4", record: "203.0.113.1:1080"},
		{name: "documentation IPv6", record: "[2001:db8::1]:1080"},
		{name: "IPv6 translation", record: "[64:ff9b::808:808]:1080"},
		{name: "newline injection", record: "8.8.8.8:1080\n1.1.1.1:80"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := SanitizeEndpoint(test.record)
			if ok != test.wantOK {
				t.Fatalf("SanitizeEndpoint(%q) ok = %v, want %v; endpoint = %#v", test.record, ok, test.wantOK, got)
			}
			if ok && (got.Host != test.wantHost || got.Port != test.wantPort) {
				t.Fatalf("SanitizeEndpoint(%q) = %#v, want host %q port %d", test.record, got, test.wantHost, test.wantPort)
			}
		})
	}
}
