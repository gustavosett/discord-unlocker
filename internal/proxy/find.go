package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTraceURL            = "https://www.cloudflare.com/cdn-cgi/trace"
	DefaultGatewayAddress      = "gateway.discord.gg:443"
	DefaultDiscordAPIURL       = "https://discord.com/api/v9/gateway"
	DefaultWorkers             = 8
	MaxWorkers                 = 8
	DefaultResultLimit         = 1
	MaxResults                 = 3
	DefaultFetchTimeout        = 10 * time.Second
	DefaultValidationTimeout   = 8 * time.Second
	DefaultTraceBodyLimit      = int64(8 << 10)
	MaxTraceBodyLimit          = int64(64 << 10)
	DefaultDiscordAPIBodyLimit = int64(8 << 10)
	MaxDiscordAPIBodyLimit     = int64(64 << 10)
)

// Options controls discovery. Its zero value is ready for production use.
// The injectable fields make network behavior deterministic in tests without
// weakening TLS verification.
type Options struct {
	SourceURL           string
	TraceURL            string
	GatewayAddress      string
	DiscordAPIURL       string
	HTTPClient          *http.Client
	DialContext         DialContextFunc
	TLSConfig           *tls.Config
	Shuffle             ShuffleFunc
	MaxBodyBytes        int64
	TraceBodyBytes      int64
	DiscordAPIBodyBytes int64
	CandidateLimit      int
	WorkerCount         int
	ResultLimit         int
	FetchTimeout        time.Duration
	ValidationTimeout   time.Duration
	Now                 func() time.Time
}

type finderConfig struct {
	Options
	traceURL      *url.URL
	discordAPIURL *url.URL
	gatewayHost   string
	gatewayPort   uint16
}

type verificationResult struct {
	endpoint Endpoint
	verified bool
}

type ProgressStage string

const (
	ProgressCache ProgressStage = "cache"
	ProgressFetch ProgressStage = "fetch"
	ProgressFresh ProgressStage = "fresh"
)

// Progress is emitted serially. Checked is the number of completed network
// validations in the current stage, including failed candidates.
type Progress struct {
	Stage    ProgressStage
	Checked  int
	Total    int
	Verified int
}

type ProgressFunc func(Progress)

// Resolver exposes configurable discovery while keeping the package-level
// Resolve function convenient for the production defaults.
type Resolver struct {
	Options Options
}

// Find fetches at most 40 candidates, checks them using up to eight workers,
// and returns the fastest verified endpoint by default.
func Find(ctx context.Context, options Options) ([]Endpoint, error) {
	selected, _, err := (Resolver{Options: options}).Resolve(ctx, nil, nil)
	return selected, err
}

// Resolve uses production defaults. It revalidates cached endpoints first and
// fetches ProxyScrape at most once if the configured result count is not met.
func Resolve(ctx context.Context, cached []Endpoint, progress ProgressFunc) (
	selected []Endpoint,
	fetched bool,
	err error,
) {
	return (Resolver{}).Resolve(ctx, cached, progress)
}

// Resolve is the configurable form of the package-level Resolve function.
func (resolver Resolver) Resolve(ctx context.Context, cached []Endpoint, progress ProgressFunc) (
	selected []Endpoint,
	fetched bool,
	err error,
) {
	if ctx == nil {
		return nil, false, errors.New("proxy: nil context")
	}
	config, err := normalizeOptions(resolver.Options)
	if err != nil {
		return nil, false, err
	}

	cacheBudget := minInt(config.CandidateLimit, MaxResults)
	cached = sanitizeCandidates(cached, cacheBudget)
	selected, err = config.verifyMany(ctx, cached, ProgressCache, progress)
	selected = trimAndSort(selected, config.ResultLimit)
	if err != nil {
		return selected, false, err
	}
	if len(selected) >= config.ResultLimit {
		return selected, false, nil
	}
	remainingBudget := config.CandidateLimit - len(cached)
	if remainingBudget <= 0 {
		if len(selected) == 0 {
			return nil, false, ErrNoVerifiedProxies
		}
		return selected, false, nil
	}

	fetched = true
	if progress != nil {
		progress(Progress{Stage: ProgressFetch})
	}
	fetchContext, cancelFetch := context.WithTimeout(ctx, config.FetchTimeout)
	candidates, fetchErr := fetchProxyScrape(
		fetchContext,
		config.HTTPClient,
		config.SourceURL,
		config.MaxBodyBytes,
		remainingBudget,
		config.Shuffle,
	)
	cancelFetch()
	if fetchErr != nil {
		return selected, true, fetchErr
	}
	candidates = withoutEndpoints(candidates, cached)
	fresh, verifyErr := config.verifyMany(ctx, candidates, ProgressFresh, progress)
	selected = append(selected, fresh...)
	selected = trimAndSort(selected, config.ResultLimit)
	if verifyErr != nil {
		return selected, true, verifyErr
	}
	if len(selected) == 0 {
		return nil, true, ErrNoVerifiedProxies
	}
	return selected, true, nil
}

// Verify performs the same checks as Find for one caller-supplied endpoint.
func Verify(ctx context.Context, endpoint Endpoint, options Options) (Endpoint, error) {
	if ctx == nil {
		return Endpoint{}, errors.New("proxy: nil context")
	}
	config, err := normalizeOptions(options)
	if err != nil {
		return Endpoint{}, err
	}
	return config.verify(ctx, endpoint)
}

func (config finderConfig) verifyMany(
	ctx context.Context,
	candidates []Endpoint,
	stage ProgressStage,
	progress ProgressFunc,
) ([]Endpoint, error) {
	if len(candidates) == 0 {
		if progress != nil {
			progress(Progress{Stage: stage})
		}
		return nil, ctx.Err()
	}
	workerCount := config.WorkerCount
	if workerCount > len(candidates) {
		workerCount = len(candidates)
	}
	jobs := make(chan Endpoint)
	results := make(chan verificationResult, len(candidates))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case candidate, ok := <-jobs:
					if !ok {
						return
					}
					endpoint, err := config.verify(ctx, candidate)
					result := verificationResult{endpoint: endpoint, verified: err == nil}
					select {
					case results <- result:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, candidate := range candidates {
			select {
			case jobs <- candidate:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	verified := make([]Endpoint, 0, minInt(len(candidates), config.ResultLimit))
	checked := 0
	if progress != nil {
		progress(Progress{Stage: stage, Total: len(candidates)})
	}
	for result := range results {
		checked++
		if result.verified {
			verified = append(verified, result.endpoint)
		}
		if progress != nil {
			progress(Progress{
				Stage:    stage,
				Checked:  checked,
				Total:    len(candidates),
				Verified: len(verified),
			})
		}
	}
	return verified, ctx.Err()
}

func (config finderConfig) verify(ctx context.Context, endpoint Endpoint) (Endpoint, error) {
	canonical, ok := SanitizeEndpoint(endpoint.Address())
	if !ok {
		return Endpoint{}, ErrInvalidEndpoint
	}
	endpoint = canonical
	validationContext, cancel := context.WithTimeout(ctx, config.ValidationTimeout)
	defer cancel()
	started := time.Now()

	country, err := config.traceCountry(validationContext, endpoint)
	if err != nil {
		return Endpoint{}, err
	}
	if err := config.verifyGatewayTLS(validationContext, endpoint); err != nil {
		return Endpoint{}, err
	}
	if err := config.verifyDiscordAPI(validationContext, endpoint); err != nil {
		return Endpoint{}, err
	}

	endpoint.Country = country
	endpoint.Latency = time.Since(started)
	endpoint.VerifiedAt = config.Now().UTC()
	return endpoint, nil
}

func (config finderConfig) traceCountry(ctx context.Context, endpoint Endpoint) (string, error) {
	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      false,
		TLSClientConfig:        tlsConfigForHost(config.TLSConfig, config.traceURL.Hostname()),
		TLSHandshakeTimeout:    config.ValidationTimeout,
		ResponseHeaderTimeout:  config.ValidationTimeout,
		MaxResponseHeaderBytes: 16 << 10,
		DialContext: func(dialContext context.Context, _, address string) (net.Conn, error) {
			return dialSOCKS5(dialContext, config.DialContext, endpoint, address)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("proxy: Cloudflare trace redirect refused")
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.traceURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("proxy: create trace request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "discord-unlocker/1")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("proxy: Cloudflare trace: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy: Cloudflare trace returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > config.TraceBodyBytes {
		return "", ErrResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, config.TraceBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("proxy: read Cloudflare trace: %w", err)
	}
	if int64(len(body)) > config.TraceBodyBytes {
		return "", ErrResponseTooLarge
	}
	return parseCountryTrace(body)
}

func (config finderConfig) verifyGatewayTLS(ctx context.Context, endpoint Endpoint) error {
	address := net.JoinHostPort(config.gatewayHost, strconv.FormatUint(uint64(config.gatewayPort), 10))
	connection, err := dialSOCKS5(ctx, config.DialContext, endpoint, address)
	if err != nil {
		return err
	}
	defer connection.Close()
	tlsConnection := tls.Client(connection, tlsConfigForHost(config.TLSConfig, config.gatewayHost))
	if err := tlsConnection.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("proxy: Discord Gateway TLS: %w", err)
	}
	state := tlsConnection.ConnectionState()
	if !state.HandshakeComplete || len(state.VerifiedChains) == 0 {
		return errors.New("proxy: Discord Gateway TLS was not verified")
	}
	return verifyGatewayWebSocket(tlsConnection, config.gatewayHost)
}

func (config finderConfig) verifyDiscordAPI(ctx context.Context, endpoint Endpoint) error {
	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      false,
		TLSClientConfig:        tlsConfigForHost(config.TLSConfig, config.discordAPIURL.Hostname()),
		TLSHandshakeTimeout:    config.ValidationTimeout,
		ResponseHeaderTimeout:  config.ValidationTimeout,
		MaxResponseHeaderBytes: 16 << 10,
		DialContext: func(dialContext context.Context, _, address string) (net.Conn, error) {
			return dialSOCKS5(dialContext, config.DialContext, endpoint, address)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("proxy: Discord API redirect refused")
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.discordAPIURL.String(), nil)
	if err != nil {
		return fmt.Errorf("proxy: create Discord API request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "discord-unlocker/1")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("proxy: Discord API: %w", err)
	}
	defer response.Body.Close()
	if response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 {
		return errors.New("proxy: Discord API TLS was not verified")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy: Discord API returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > config.DiscordAPIBodyBytes {
		return ErrResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, config.DiscordAPIBodyBytes+1))
	if err != nil {
		return fmt.Errorf("proxy: read Discord API response: %w", err)
	}
	if int64(len(body)) > config.DiscordAPIBodyBytes {
		return ErrResponseTooLarge
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("proxy: invalid Discord API response: %w", err)
	}
	if payload.URL != "wss://gateway.discord.gg" {
		return errors.New("proxy: Discord API returned an unexpected Gateway URL")
	}
	return nil
}

// verifyGatewayWebSocket proves that the exit can reach the actual Gateway
// protocol, rather than merely a TLS terminator which later refuses the
// request. The response is bounded even though its certificate was verified.
func verifyGatewayWebSocket(connection net.Conn, host string) error {
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return fmt.Errorf("proxy: generate Discord Gateway WebSocket key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	request := "GET /?v=10&encoding=json HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"User-Agent: discord-unlocker/1\r\n\r\n"
	if err := writeAll(connection, []byte(request)); err != nil {
		return fmt.Errorf("proxy: write Discord Gateway WebSocket handshake: %w", err)
	}

	const maxGatewayHeaders = int64(32 << 10)
	response, err := http.ReadResponse(
		bufio.NewReader(io.LimitReader(connection, maxGatewayHeaders)),
		&http.Request{Method: http.MethodGet},
	)
	if err != nil {
		return fmt.Errorf("proxy: read Discord Gateway WebSocket handshake: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("proxy: Discord Gateway WebSocket returned HTTP %d", response.StatusCode)
	}
	if !strings.EqualFold(strings.TrimSpace(response.Header.Get("Upgrade")), "websocket") ||
		!headerContainsToken(response.Header.Values("Connection"), "upgrade") {
		return errors.New("proxy: Discord Gateway returned an invalid WebSocket upgrade")
	}

	digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) // #nosec G505 -- required by RFC 6455, not used for trust.
	wantAccept := base64.StdEncoding.EncodeToString(digest[:])
	if strings.TrimSpace(response.Header.Get("Sec-WebSocket-Accept")) != wantAccept {
		return errors.New("proxy: Discord Gateway returned an invalid WebSocket accept value")
	}
	return nil
}

func headerContainsToken(values []string, token string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func normalizeOptions(options Options) (finderConfig, error) {
	if options.SourceURL == "" {
		options.SourceURL = DefaultSourceURL
	}
	if options.TraceURL == "" {
		options.TraceURL = DefaultTraceURL
	}
	if options.GatewayAddress == "" {
		options.GatewayAddress = DefaultGatewayAddress
	}
	if options.DiscordAPIURL == "" {
		options.DiscordAPIURL = DefaultDiscordAPIURL
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.DialContext == nil {
		dialer := &net.Dialer{KeepAlive: -1}
		options.DialContext = dialer.DialContext
	}
	if options.TLSConfig == nil {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		options.TLSConfig = options.TLSConfig.Clone()
		if options.TLSConfig.InsecureSkipVerify {
			return finderConfig{}, errors.New("proxy: insecure TLS configuration refused")
		}
		if options.TLSConfig.MinVersion < tls.VersionTLS12 {
			options.TLSConfig.MinVersion = tls.VersionTLS12
		}
	}
	options.TLSConfig.ServerName = ""
	if options.Shuffle == nil {
		options.Shuffle = secureShuffle
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = DefaultMaxBody
	}
	if options.MaxBodyBytes > HardMaxBody {
		options.MaxBodyBytes = HardMaxBody
	}
	if options.TraceBodyBytes <= 0 {
		options.TraceBodyBytes = DefaultTraceBodyLimit
	}
	if options.TraceBodyBytes > MaxTraceBodyLimit {
		options.TraceBodyBytes = MaxTraceBodyLimit
	}
	if options.DiscordAPIBodyBytes <= 0 {
		options.DiscordAPIBodyBytes = DefaultDiscordAPIBodyLimit
	}
	if options.DiscordAPIBodyBytes > MaxDiscordAPIBodyLimit {
		options.DiscordAPIBodyBytes = MaxDiscordAPIBodyLimit
	}
	if options.CandidateLimit <= 0 || options.CandidateLimit > MaxCandidates {
		options.CandidateLimit = MaxCandidates
	}
	if options.WorkerCount <= 0 || options.WorkerCount > MaxWorkers {
		options.WorkerCount = DefaultWorkers
	}
	if options.ResultLimit <= 0 || options.ResultLimit > MaxResults {
		options.ResultLimit = DefaultResultLimit
	}
	if options.FetchTimeout <= 0 {
		options.FetchTimeout = DefaultFetchTimeout
	}
	if options.ValidationTimeout <= 0 {
		options.ValidationTimeout = DefaultValidationTimeout
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	traceURL, err := url.Parse(options.TraceURL)
	if err != nil || traceURL.Scheme != "https" || traceURL.Host == "" || traceURL.User != nil ||
		traceURL.Fragment != "" || !validDNSName(strings.TrimSuffix(traceURL.Hostname(), ".")) {
		return finderConfig{}, errors.New("proxy: invalid HTTPS trace URL")
	}
	discordAPIURL, err := url.Parse(options.DiscordAPIURL)
	if err != nil || discordAPIURL.Scheme != "https" || discordAPIURL.Host == "" || discordAPIURL.User != nil ||
		discordAPIURL.Fragment != "" || !validDNSName(strings.TrimSuffix(discordAPIURL.Hostname(), ".")) {
		return finderConfig{}, errors.New("proxy: invalid Discord API HTTPS URL")
	}
	gatewayHost, gatewayPortText, err := net.SplitHostPort(options.GatewayAddress)
	if err != nil || !validDNSName(strings.TrimSuffix(gatewayHost, ".")) {
		return finderConfig{}, errors.New("proxy: invalid Discord Gateway address")
	}
	gatewayPort, err := strconv.ParseUint(gatewayPortText, 10, 16)
	if err != nil || gatewayPort == 0 {
		return finderConfig{}, errors.New("proxy: invalid Discord Gateway port")
	}

	return finderConfig{
		Options:       options,
		traceURL:      traceURL,
		discordAPIURL: discordAPIURL,
		gatewayHost:   strings.TrimSuffix(strings.ToLower(gatewayHost), "."),
		gatewayPort:   uint16(gatewayPort),
	}, nil
}

func tlsConfigForHost(base *tls.Config, host string) *tls.Config {
	config := base.Clone()
	config.ServerName = strings.TrimSuffix(strings.ToLower(host), ".")
	if config.MinVersion < tls.VersionTLS12 {
		config.MinVersion = tls.VersionTLS12
	}
	return config
}

func sortEndpoints(endpoints []Endpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Latency != endpoints[j].Latency {
			return endpoints[i].Latency < endpoints[j].Latency
		}
		if endpoints[i].Host != endpoints[j].Host {
			return endpoints[i].Host < endpoints[j].Host
		}
		return endpoints[i].Port < endpoints[j].Port
	})
}

func sanitizeCandidates(candidates []Endpoint, limit int) []Endpoint {
	if limit <= 0 || limit > MaxCandidates {
		limit = MaxCandidates
	}
	result := make([]Endpoint, 0, minInt(len(candidates), limit))
	seen := make(map[string]struct{}, minInt(len(candidates), limit))
	for _, candidate := range candidates {
		if len(result) == limit {
			break
		}
		canonical, ok := SanitizeEndpoint(candidate.Address())
		if !ok {
			continue
		}
		if _, duplicate := seen[canonical.Address()]; duplicate {
			continue
		}
		seen[canonical.Address()] = struct{}{}
		result = append(result, canonical)
	}
	return result
}

func withoutEndpoints(candidates, excluded []Endpoint) []Endpoint {
	seen := make(map[string]struct{}, len(excluded))
	for _, endpoint := range excluded {
		seen[endpoint.Address()] = struct{}{}
	}
	result := make([]Endpoint, 0, len(candidates))
	for _, candidate := range candidates {
		if _, skip := seen[candidate.Address()]; skip {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func trimAndSort(endpoints []Endpoint, limit int) []Endpoint {
	sortEndpoints(endpoints)
	if len(endpoints) > limit {
		endpoints = endpoints[:limit]
	}
	return endpoints
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
