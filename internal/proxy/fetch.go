package proxy

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultSourceURL = "https://api.proxyscrape.com/v4/free-proxy-list/get?request=displayproxies&protocol=socks5&proxy_format=ipport&format=text&timeout=5000&limit=2000"
	DefaultMaxBody   = int64(256 << 10)
	HardMaxBody      = int64(1 << 20)
	MaxCandidates    = 40
	MaxSourceHeaders = int64(32 << 10)
)

var ErrResponseTooLarge = errors.New("proxy: response body exceeds limit")

type ShuffleFunc func([]Endpoint) error

// ParseProxyScrape parses ProxyScrape's text ip:port format. Malformed and
// unsafe rows are ignored and duplicates are removed. The complete sanitized
// set is shuffled before it is capped, preventing clients from concentrating
// on the same first addresses returned by the provider.
func ParseProxyScrape(data []byte, limit int) ([]Endpoint, error) {
	return parseProxyScrape(data, limit, secureShuffle)
}

func parseProxyScrape(data []byte, limit int, shuffle ShuffleFunc) ([]Endpoint, error) {
	if limit <= 0 || limit > MaxCandidates {
		limit = MaxCandidates
	}
	result := make([]Endpoint, 0)
	seen := make(map[string]struct{})

	for _, line := range bytes.Split(data, []byte{'\n'}) {
		endpoint, ok := SanitizeEndpoint(string(bytes.TrimSuffix(line, []byte{'\r'})))
		if !ok {
			continue
		}
		address := endpoint.Address()
		if _, duplicate := seen[address]; duplicate {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, endpoint)
	}
	if shuffle == nil {
		return nil, errors.New("proxy: nil candidate shuffler")
	}
	if err := shuffle(result); err != nil {
		return nil, fmt.Errorf("proxy: shuffle candidates: %w", err)
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// FetchProxyScrape downloads and parses a bounded ProxyScrape response.
// The request lifetime is controlled by ctx; client may be nil.
func FetchProxyScrape(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	maxBody int64,
	maxCandidates int,
) ([]Endpoint, error) {
	return fetchProxyScrape(ctx, client, sourceURL, maxBody, maxCandidates, secureShuffle)
}

func fetchProxyScrape(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	maxBody int64,
	maxCandidates int,
	shuffle ShuffleFunc,
) ([]Endpoint, error) {
	if ctx == nil {
		return nil, errors.New("proxy: nil context")
	}
	if client == nil {
		client = http.DefaultClient
	}
	if maxBody <= 0 {
		maxBody = DefaultMaxBody
	}
	if maxBody > HardMaxBody {
		maxBody = HardMaxBody
	}

	parsedURL, err := url.Parse(sourceURL)
	if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") ||
		parsedURL.Host == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
		return nil, fmt.Errorf("proxy: invalid source URL")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("proxy: create source request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "discord-unlocker/1")

	boundedClient, err := sourceHTTPClient(client)
	if err != nil {
		return nil, err
	}
	defer boundedClient.CloseIdleConnections()
	boundedClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("proxy: source redirect refused")
	}
	response, err := boundedClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("proxy: fetch source: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy: source returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxBody {
		return nil, ErrResponseTooLarge
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("proxy: read source: %w", err)
	}
	if int64(len(body)) > maxBody {
		return nil, ErrResponseTooLarge
	}

	endpoints, err := parseProxyScrape(body, maxCandidates, shuffle)
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, ErrNoCandidates
	}
	return endpoints, nil
}

func sourceHTTPClient(client *http.Client) (*http.Client, error) {
	result := *client
	var transport *http.Transport
	switch configured := client.Transport.(type) {
	case nil:
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("proxy: unsupported default HTTP transport")
		}
		transport = defaultTransport.Clone()
	case *http.Transport:
		transport = configured.Clone()
	default:
		return nil, errors.New("proxy: source HTTP transport cannot enforce header limit")
	}
	transport.MaxResponseHeaderBytes = MaxSourceHeaders
	transport.DisableKeepAlives = true
	result.Transport = transport
	return &result, nil
}

func parseCountryTrace(body []byte) (string, error) {
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		key, value, ok := bytes.Cut(bytes.TrimSpace(line), []byte{'='})
		if !ok || !bytes.Equal(key, []byte("loc")) {
			continue
		}
		country := strings.ToUpper(strings.TrimSpace(string(value)))
		if !isISOAlpha2(country) {
			return "", errors.New("proxy: invalid Cloudflare country")
		}
		if country == "BR" {
			return "", errors.New("proxy: Brazilian exit")
		}
		return country, nil
	}
	return "", errors.New("proxy: Cloudflare trace has no location")
}

func secureShuffle(endpoints []Endpoint) error {
	for i := len(endpoints) - 1; i > 0; i-- {
		value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		j := int(value.Int64())
		endpoints[i], endpoints[j] = endpoints[j], endpoints[i]
	}
	return nil
}

var isoAlpha2 = func() map[string]struct{} {
	codes := strings.Fields(`
		AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ
		BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ
		CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ
		DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR
		GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY
		HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP
		KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY
		MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ
		NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY
		QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ
		TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ
		VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW
	`)
	result := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		result[code] = struct{}{}
	}
	return result
}()

func isISOAlpha2(country string) bool {
	_, ok := isoAlpha2[country]
	return ok
}
