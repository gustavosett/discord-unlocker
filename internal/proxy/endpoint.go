package proxy

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

// Endpoint is a SOCKS5 proxy which has passed all of the checks performed by
// Find or Verify. Host is always a canonical, literal, public IP address.
type Endpoint struct {
	Host       string
	Port       uint16
	Country    string
	Latency    time.Duration
	VerifiedAt time.Time
}

var (
	// ErrInvalidEndpoint means that an address was not a literal, publicly
	// routable IP address with a non-zero TCP port.
	ErrInvalidEndpoint = errors.New("proxy: invalid endpoint")
	// ErrNoCandidates means that ProxyScrape returned no safe candidate.
	ErrNoCandidates = errors.New("proxy: no safe SOCKS5 candidates")
	// ErrNoVerifiedProxies means that no candidate passed every verification.
	ErrNoVerifiedProxies = errors.New("proxy: no verified non-Brazilian SOCKS5 proxies")
)

var nonPublicNetworks = mustNetworks(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001::/23",
	"2001:2::/48",
	"2001:10::/28",
	"2001:20::/28",
	"2001:db8::/32",
	"2002::/16",
	"3fff::/20",
	"5f00::/16",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
)

// Address returns the endpoint in the form accepted by net.Dial.
func (e Endpoint) Address() string {
	return net.JoinHostPort(e.Host, strconv.FormatUint(uint64(e.Port), 10))
}

// Validate checks the endpoint without performing network I/O.
func (e Endpoint) Validate() error {
	if e.Port == 0 || !isPublicLiteralIP(e.Host) {
		return ErrInvalidEndpoint
	}
	return nil
}

// SanitizeEndpoint parses one ProxyScrape ip:port record. It deliberately
// rejects hostnames, credentials, protocol prefixes and non-public addresses.
func SanitizeEndpoint(record string) (Endpoint, bool) {
	record = strings.TrimSpace(record)
	if record == "" || strings.ContainsAny(record, "\x00\r\n\t ") {
		return Endpoint{}, false
	}

	host, portText, err := net.SplitHostPort(record)
	if err != nil {
		return Endpoint{}, false
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return Endpoint{}, false
	}

	ip := net.ParseIP(host)
	if ip == nil || !isPublicIP(ip) {
		return Endpoint{}, false
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		host = ipv4.String()
	} else {
		host = ip.String()
	}

	return Endpoint{Host: host, Port: uint16(port)}, true
}

func isPublicLiteralIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && isPublicIP(ip)
}

func isPublicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, network := range nonPublicNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func mustNetworks(values ...string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		result = append(result, network)
	}
	return result
}
