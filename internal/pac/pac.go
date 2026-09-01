// Package pac creates proxy auto-configuration files for Discord's gateway
// connections.
package pac

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const maxEndpoints = 3

var (
	// ErrNoEndpoints is returned when a PAC file cannot contain a SOCKS proxy.
	ErrNoEndpoints = errors.New("at least one proxy endpoint is required")
	// ErrInvalidIP is returned when an endpoint is not a public IP literal.
	ErrInvalidIP = errors.New("IP must be a public literal address")
	// ErrInvalidPort is returned when an endpoint port is outside 1..65535.
	ErrInvalidPort = errors.New("port must be between 1 and 65535")
	// ErrTooManyEndpoints is returned when the PAC fallback chain would exceed
	// its supported maximum.
	ErrTooManyEndpoints = errors.New("at most three proxy endpoints are allowed")
)

// Endpoint identifies a SOCKS5 proxy. IP must be an IP literal rather than a
// hostname, and Port must be between 1 and 65535.
type Endpoint struct {
	IP   string
	Port int
}

// NewEndpoint validates and canonicalizes a SOCKS5 proxy endpoint.
func NewEndpoint(ip string, port int) (Endpoint, error) {
	addr, err := validateEndpoint(Endpoint{IP: ip, Port: port})
	if err != nil {
		return Endpoint{}, err
	}

	return Endpoint{IP: addr.String(), Port: port}, nil
}

// Generate returns a deterministic PAC program which proxies only Discord's
// gateway hosts. Besides the public gateway name, Discord supplies regional
// resume URLs such as gateway-us-east1-d.discord.gg in READY payloads. Every
// other host is connected to directly.
func Generate(endpoints []Endpoint) (string, error) {
	normalized, err := normalize(endpoints)
	if err != nil {
		return "", err
	}

	proxies := make([]string, 0, len(normalized)+1)
	for _, endpoint := range normalized {
		proxies = append(proxies, "SOCKS5 "+net.JoinHostPort(endpoint.addr.String(), strconv.Itoa(endpoint.port)))
	}
	proxies = append(proxies, "DIRECT")

	return `function FindProxyForURL(url, host) {
    host = host.toLowerCase();
    if (host === "gateway.discord.gg" ||
        host === "remote-auth-gateway.discord.gg" ||
        shExpMatch(host, "gateway-*.discord.gg")) {
        return "` + strings.Join(proxies, "; ") + `";
    }
    return "DIRECT";
}
`, nil
}

type normalizedEndpoint struct {
	addr netip.Addr
	port int
}

func normalize(endpoints []Endpoint) ([]normalizedEndpoint, error) {
	if len(endpoints) == 0 {
		return nil, ErrNoEndpoints
	}
	if len(endpoints) > maxEndpoints {
		return nil, ErrTooManyEndpoints
	}

	normalized := make([]normalizedEndpoint, 0, len(endpoints))
	seen := make(map[normalizedEndpoint]struct{}, len(endpoints))
	for i, endpoint := range endpoints {
		addr, err := validateEndpoint(endpoint)
		if err != nil {
			return nil, fmt.Errorf("endpoint %d: %w", i, err)
		}
		candidate := normalizedEndpoint{addr: addr, port: endpoint.Port}
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		normalized = append(normalized, candidate)
	}

	return normalized, nil
}

func validateEndpoint(endpoint Endpoint) (netip.Addr, error) {
	if endpoint.Port < 1 || endpoint.Port > 65535 {
		return netip.Addr{}, ErrInvalidPort
	}

	addr, err := netip.ParseAddr(endpoint.IP)
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, ErrInvalidIP
	}
	addr = addr.Unmap()
	if !isPublic(addr) {
		return netip.Addr{}, ErrInvalidIP
	}

	return addr, nil
}

// isPublic excludes non-routable and special-purpose ranges. IsGlobalUnicast
// alone is insufficient because it includes documentation and benchmarking
// networks.
func isPublic(addr netip.Addr) bool {
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}

	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}

	return true
}

var nonPublicPrefixes = []netip.Prefix{
	// IPv4 special-purpose ranges.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),

	// IPv6 special-purpose ranges which IsGlobalUnicast does not reject.
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}
