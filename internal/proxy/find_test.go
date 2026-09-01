package proxy

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFindAndResolveValidateTraceGatewayAndCache(t *testing.T) {
	t.Parallel()
	certificate, roots := testServerCertificate(t, "trace.test", "gateway.test", "api.test")
	traceUS := startTraceServer(t, certificate, "US")
	traceBR := startTraceServer(t, certificate, "BR")
	gateway := startGatewayServer(t, certificate)
	discordAPI := startDiscordAPIServer(t, certificate, http.StatusOK, `{"url":"wss://gateway.discord.gg"}`)

	type candidate struct {
		endpoint Endpoint
		proxy    string
	}
	candidates := []candidate{
		{endpoint: Endpoint{Host: "1.1.1.1", Port: 1001}, proxy: startSOCKS5Relay(t, traceUS, gateway, discordAPI, 5*time.Millisecond)},
		{endpoint: Endpoint{Host: "9.9.9.9", Port: 1002}, proxy: startSOCKS5Relay(t, traceUS, gateway, discordAPI, 30*time.Millisecond)},
		{endpoint: Endpoint{Host: "8.8.8.8", Port: 1003}, proxy: startSOCKS5Relay(t, traceUS, gateway, discordAPI, 60*time.Millisecond)},
		{endpoint: Endpoint{Host: "208.67.222.222", Port: 1004}, proxy: startSOCKS5Relay(t, traceBR, gateway, discordAPI, time.Millisecond)},
	}
	mapped := make(map[string]string, len(candidates))
	lines := []string{"127.0.0.1:9", "not-a-proxy"}
	for _, candidate := range candidates {
		mapped[candidate.endpoint.Address()] = candidate.proxy
		lines = append(lines, candidate.endpoint.Address())
	}
	lines = append(lines, candidates[0].endpoint.Address())
	lines = append(lines, "4.2.2.2:1005") // structurally valid, but deliberately down

	var sourceCalls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		sourceCalls.Add(1)
		_, _ = io.WriteString(writer, strings.Join(lines, "\r\n"))
	}))
	defer source.Close()

	fixedNow := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.FixedZone("test", -3*60*60))
	options := Options{
		SourceURL:         source.URL,
		TraceURL:          "https://trace.test/cdn-cgi/trace",
		GatewayAddress:    "gateway.test:443",
		DiscordAPIURL:     "https://api.test/api/v9/gateway",
		HTTPClient:        source.Client(),
		DialContext:       mappedDialer(mapped),
		TLSConfig:         &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		Shuffle:           func([]Endpoint) error { return nil },
		ValidationTimeout: 3 * time.Second,
		FetchTimeout:      time.Second,
		ResultLimit:       3,
		Now:               func() time.Time { return fixedNow },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := Find(ctx, options)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Find() returned %d endpoints, want 3: %#v", len(got), got)
	}
	wantOrder := []string{"1.1.1.1", "9.9.9.9", "8.8.8.8"}
	for i, endpoint := range got {
		if endpoint.Host != wantOrder[i] {
			t.Fatalf("Find()[%d].Host = %q, want %q; all=%#v", i, endpoint.Host, wantOrder[i], got)
		}
		if endpoint.Country != "US" {
			t.Errorf("Find()[%d].Country = %q, want US", i, endpoint.Country)
		}
		if !endpoint.VerifiedAt.Equal(fixedNow.UTC()) {
			t.Errorf("Find()[%d].VerifiedAt = %s, want %s", i, endpoint.VerifiedAt, fixedNow.UTC())
		}
		if endpoint.Latency <= 0 {
			t.Errorf("Find()[%d].Latency = %s", i, endpoint.Latency)
		}
	}
	if calls := sourceCalls.Load(); calls != 1 {
		t.Fatalf("source calls after Find = %d, want 1", calls)
	}

	var progress []Progress
	selected, fetched, err := (Resolver{Options: options}).Resolve(ctx, got, func(update Progress) {
		progress = append(progress, update)
	})
	if err != nil {
		t.Fatalf("Resolve(cache) error = %v", err)
	}
	if fetched {
		t.Fatal("Resolve(cache) fetched despite three valid cached endpoints")
	}
	if len(selected) != 3 || sourceCalls.Load() != 1 {
		t.Fatalf("Resolve(cache) selected=%d source calls=%d", len(selected), sourceCalls.Load())
	}
	if len(progress) == 0 || progress[len(progress)-1].Stage != ProgressCache || progress[len(progress)-1].Verified != 3 {
		t.Fatalf("Resolve(cache) progress = %#v", progress)
	}

	selected, fetched, err = (Resolver{Options: options}).Resolve(ctx, got[:2], nil)
	if err != nil {
		t.Fatalf("Resolve(partial cache) error = %v", err)
	}
	if !fetched || len(selected) != 3 {
		t.Fatalf("Resolve(partial cache) fetched=%v selected=%#v", fetched, selected)
	}
	if calls := sourceCalls.Load(); calls != 2 {
		t.Fatalf("Resolve(partial cache) made %d cumulative source calls, want 2", calls)
	}
}

func TestFindLimitsWorkersAndHonorsContext(t *testing.T) {
	t.Parallel()
	lines := make([]string, 0, MaxCandidates)
	for i := 1; i <= MaxCandidates; i++ {
		lines = append(lines, fmt.Sprintf("11.0.0.%d:%d", i, 1000+i))
	}
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, strings.Join(lines, "\n"))
	}))
	defer source.Close()

	var active atomic.Int32
	var maximum atomic.Int32
	dialer := func(ctx context.Context, _, _ string) (net.Conn, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := Find(ctx, Options{
		SourceURL:         source.URL,
		HTTPClient:        source.Client(),
		DialContext:       dialer,
		Shuffle:           func([]Endpoint) error { return nil },
		ValidationTimeout: time.Second,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Find() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Find() returned after %s", elapsed)
	}
	if got := maximum.Load(); got < 1 || got > MaxWorkers {
		t.Fatalf("maximum concurrent dials = %d, want 1..%d", got, MaxWorkers)
	}
}

func TestVerifyRefusesInsecureTLSConfiguration(t *testing.T) {
	t.Parallel()
	_, err := Verify(context.Background(), Endpoint{Host: "8.8.8.8", Port: 1080}, Options{
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec -- deliberate rejection test
	})
	if err == nil || !strings.Contains(err.Error(), "insecure TLS") {
		t.Fatalf("Verify() error = %v, want insecure TLS refusal", err)
	}
}

func TestVerifyRequiresRealGatewayWebSocketUpgrade(t *testing.T) {
	t.Parallel()
	certificate, roots := testServerCertificate(t, "trace.test", "gateway.test")
	trace := startTraceServer(t, certificate, "US")
	gateway := startRejectedGatewayServer(t, certificate)
	endpoint := Endpoint{Host: "8.8.8.8", Port: 1080}
	proxyAddress := startSOCKS5Relay(t, trace, gateway, "", time.Millisecond)

	_, err := Verify(context.Background(), endpoint, Options{
		TraceURL:          "https://trace.test/cdn-cgi/trace",
		GatewayAddress:    "gateway.test:443",
		DialContext:       mappedDialer(map[string]string{endpoint.Address(): proxyAddress}),
		TLSConfig:         &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		ValidationTimeout: 2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "Gateway WebSocket returned HTTP 403") {
		t.Fatalf("Verify() error = %v, want rejected WebSocket upgrade", err)
	}
}

func TestVerifyRequiresExpectedDiscordGatewayAPIResponse(t *testing.T) {
	t.Parallel()
	certificate, roots := testServerCertificate(t, "trace.test", "gateway.test", "api.test")
	trace := startTraceServer(t, certificate, "US")
	gateway := startGatewayServer(t, certificate)

	tests := []struct {
		name      string
		status    int
		body      string
		bodyLimit int64
		wantError string
	}{
		{
			name:      "non-200 status",
			status:    http.StatusForbidden,
			body:      `{"message":"forbidden"}`,
			wantError: "Discord API returned HTTP 403",
		},
		{
			name:      "malformed JSON",
			status:    http.StatusOK,
			body:      `{"url":`,
			wantError: "invalid Discord API response",
		},
		{
			name:      "unexpected gateway",
			status:    http.StatusOK,
			body:      `{"url":"wss://example.test"}`,
			wantError: "unexpected Gateway URL",
		},
		{
			name:      "oversized body",
			status:    http.StatusOK,
			body:      `{"url":"wss://gateway.discord.gg","padding":"0123456789"}`,
			bodyLimit: 16,
			wantError: ErrResponseTooLarge.Error(),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			discordAPI := startDiscordAPIServer(t, certificate, test.status, test.body)
			endpoint := Endpoint{Host: "8.8.8.8", Port: 1080}
			proxyAddress := startSOCKS5Relay(t, trace, gateway, discordAPI, time.Millisecond)
			_, err := Verify(context.Background(), endpoint, Options{
				TraceURL:            "https://trace.test/cdn-cgi/trace",
				GatewayAddress:      "gateway.test:443",
				DiscordAPIURL:       "https://api.test/api/v9/gateway",
				DiscordAPIBodyBytes: test.bodyLimit,
				DialContext:         mappedDialer(map[string]string{endpoint.Address(): proxyAddress}),
				TLSConfig:           &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
				ValidationTimeout:   2 * time.Second,
			})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Verify() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestVerifyRejectsUntrustedDiscordAPICertificate(t *testing.T) {
	t.Parallel()
	trustedCertificate, trustedRoots := testServerCertificate(t, "trace.test", "gateway.test")
	untrustedCertificate, _ := testServerCertificate(t, "api.test")
	trace := startTraceServer(t, trustedCertificate, "US")
	gateway := startGatewayServer(t, trustedCertificate)
	discordAPI := startDiscordAPIServer(t, untrustedCertificate, http.StatusOK, `{"url":"wss://gateway.discord.gg"}`)
	endpoint := Endpoint{Host: "8.8.4.4", Port: 1080}
	proxyAddress := startSOCKS5Relay(t, trace, gateway, discordAPI, time.Millisecond)

	_, err := Verify(context.Background(), endpoint, Options{
		TraceURL:          "https://trace.test/cdn-cgi/trace",
		GatewayAddress:    "gateway.test:443",
		DiscordAPIURL:     "https://api.test/api/v9/gateway",
		DialContext:       mappedDialer(map[string]string{endpoint.Address(): proxyAddress}),
		TLSConfig:         &tls.Config{RootCAs: trustedRoots, MinVersion: tls.VersionTLS12},
		ValidationTimeout: 2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "Discord API") {
		t.Fatalf("Verify() error = %v, want Discord API TLS rejection", err)
	}
}

func TestResolvePreservesValidCacheWhenFetchFailsAndRejectsUntrustedCertificate(t *testing.T) {
	t.Parallel()
	certificate, roots := testServerCertificate(t, "trace.test", "gateway.test", "api.test")
	trace := startTraceServer(t, certificate, "US")
	gateway := startGatewayServer(t, certificate)
	discordAPI := startDiscordAPIServer(t, certificate, http.StatusOK, `{"url":"wss://gateway.discord.gg"}`)
	endpoint := Endpoint{Host: "8.8.4.4", Port: 1080}
	proxyAddress := startSOCKS5Relay(t, trace, gateway, discordAPI, 2*time.Millisecond)
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer source.Close()
	base := Options{
		SourceURL:         source.URL,
		TraceURL:          "https://trace.test/cdn-cgi/trace",
		GatewayAddress:    "gateway.test:443",
		DiscordAPIURL:     "https://api.test/api/v9/gateway",
		HTTPClient:        source.Client(),
		DialContext:       mappedDialer(map[string]string{endpoint.Address(): proxyAddress}),
		TLSConfig:         &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		Shuffle:           func([]Endpoint) error { return nil },
		ValidationTimeout: 2 * time.Second,
		FetchTimeout:      time.Second,
		ResultLimit:       2,
	}
	selected, fetched, err := (Resolver{Options: base}).Resolve(context.Background(), []Endpoint{endpoint}, nil)
	if err == nil || !fetched {
		t.Fatalf("Resolve() fetched=%v error=%v, want fetch failure", fetched, err)
	}
	if len(selected) != 1 || selected[0].Address() != endpoint.Address() {
		t.Fatalf("Resolve() discarded valid cached endpoint: %#v", selected)
	}

	untrusted := base
	untrusted.TLSConfig = &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}
	if _, err := Verify(context.Background(), endpoint, untrusted); err == nil {
		t.Fatal("Verify() accepted a certificate outside the configured trust roots")
	}
}

func TestResolveSharesCandidateBudgetAndDoesNotRetryCachedEndpoints(t *testing.T) {
	t.Parallel()
	cached := []Endpoint{
		{Host: "1.1.1.1", Port: 1001},
		{Host: "8.8.8.8", Port: 1002},
		{Host: "9.9.9.9", Port: 1003},
	}

	t.Run("global budget", func(t *testing.T) {
		lines := make([]string, 0, MaxCandidates)
		for i := 1; i <= MaxCandidates; i++ {
			lines = append(lines, fmt.Sprintf("11.1.0.%d:%d", i, 2000+i))
		}
		source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, strings.Join(lines, "\n"))
		}))
		defer source.Close()

		var attempts atomic.Int32
		resolver := Resolver{Options: Options{
			SourceURL:  source.URL,
			HTTPClient: source.Client(),
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				attempts.Add(1)
				return nil, errors.New("deliberately unreachable")
			},
			Shuffle:           func([]Endpoint) error { return nil },
			ValidationTimeout: time.Second,
		}}
		_, fetched, err := resolver.Resolve(context.Background(), cached, nil)
		if !errors.Is(err, ErrNoVerifiedProxies) || !fetched {
			t.Fatalf("Resolve() fetched=%v error=%v, want exhausted discovery", fetched, err)
		}
		if got := attempts.Load(); got != MaxCandidates {
			t.Fatalf("network validation attempts = %d, want global limit %d", got, MaxCandidates)
		}
	})

	t.Run("failed cache excluded from fresh candidates", func(t *testing.T) {
		lines := make([]string, 0, MaxCandidates)
		for _, endpoint := range cached {
			lines = append(lines, endpoint.Address())
		}
		for i := 1; i <= MaxCandidates; i++ {
			lines = append(lines, fmt.Sprintf("12.1.0.%d:%d", i, 3000+i))
		}
		source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, strings.Join(lines, "\n"))
		}))
		defer source.Close()

		attempts := make(map[string]int)
		var attemptsMu sync.Mutex
		resolver := Resolver{Options: Options{
			SourceURL:  source.URL,
			HTTPClient: source.Client(),
			DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
				attemptsMu.Lock()
				attempts[address]++
				attemptsMu.Unlock()
				return nil, errors.New("deliberately unreachable")
			},
			Shuffle:           func([]Endpoint) error { return nil },
			ValidationTimeout: time.Second,
		}}
		_, _, err := resolver.Resolve(context.Background(), cached, nil)
		if !errors.Is(err, ErrNoVerifiedProxies) {
			t.Fatalf("Resolve() error = %v, want ErrNoVerifiedProxies", err)
		}
		attemptsMu.Lock()
		defer attemptsMu.Unlock()
		for _, endpoint := range cached {
			if got := attempts[endpoint.Address()]; got != 1 {
				t.Errorf("cached endpoint %s attempted %d times, want once", endpoint.Address(), got)
			}
		}
	})
}

func mappedDialer(mapping map[string]string) DialContextFunc {
	dialer := &net.Dialer{}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		mappedAddress, ok := mapping[address]
		if !ok {
			return nil, fmt.Errorf("unexpected endpoint %s", address)
		}
		return dialer.DialContext(ctx, network, mappedAddress)
	}
}

func startSOCKS5Relay(t *testing.T, traceAddress, gatewayAddress, discordAPIAddress string, delay time.Duration) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SOCKS5 relay: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go handleSOCKS5Relay(connection, traceAddress, gatewayAddress, discordAPIAddress, delay)
		}
	}()
	return listener.Addr().String()
}

func handleSOCKS5Relay(connection net.Conn, traceAddress, gatewayAddress, discordAPIAddress string, delay time.Duration) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	header := make([]byte, 2)
	if _, err := io.ReadFull(connection, header); err != nil || header[0] != 0x05 || header[1] == 0 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(connection, methods); err != nil {
		return
	}
	if err := writeAll(connection, []byte{0x05, 0x00}); err != nil {
		return
	}
	requestHeader := make([]byte, 4)
	if _, err := io.ReadFull(connection, requestHeader); err != nil || requestHeader[0] != 0x05 || requestHeader[1] != 0x01 {
		return
	}
	host, port, err := readSOCKS5Target(connection, requestHeader[3])
	if err != nil {
		return
	}
	var target string
	switch net.JoinHostPort(host, fmt.Sprintf("%d", port)) {
	case "trace.test:443":
		target = traceAddress
	case "gateway.test:443":
		target = gatewayAddress
	case "api.test:443":
		target = discordAPIAddress
	default:
		_ = writeAll(connection, []byte{0x05, 0x04, 0x00, 0x01})
		return
	}
	backend, err := net.DialTimeout("tcp", target, time.Second)
	if err != nil {
		_ = writeAll(connection, []byte{0x05, 0x05, 0x00, 0x01})
		return
	}
	defer backend.Close()
	_ = backend.SetDeadline(time.Now().Add(5 * time.Second))
	time.Sleep(delay)
	if err := writeAll(connection, []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(backend, connection)
		if tcp, ok := backend.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		close(done)
	}()
	_, _ = io.Copy(connection, backend)
	<-done
}

func startTraceServer(t *testing.T, certificate tls.Certificate, country string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen trace server: %v", err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(writer, "fl=test\nloc=%s\ntls=TLSv1.3\n", country)
		}),
		ReadHeaderTimeout: time.Second,
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(tlsListener) }()
	return listener.Addr().String()
}

func startDiscordAPIServer(t *testing.T, certificate tls.Certificate, status int, body string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen Discord API server: %v", err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/api/v9/gateway" {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(status)
			_, _ = io.WriteString(writer, body)
		}),
		ReadHeaderTimeout: time.Second,
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(tlsListener) }()
	return listener.Addr().String()
}

func startGatewayServer(t *testing.T, certificate tls.Certificate) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen gateway server: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(raw net.Conn) {
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
				server := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
				if err := server.Handshake(); err != nil {
					return
				}
				request, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					return
				}
				_ = request.Body.Close()
				key := request.Header.Get("Sec-WebSocket-Key")
				digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) // #nosec G505 -- WebSocket protocol fixture.
				accept := base64.StdEncoding.EncodeToString(digest[:])
				_, _ = fmt.Fprintf(server,
					"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
					accept,
				)
			}(connection)
		}
	}()
	return listener.Addr().String()
}

func startRejectedGatewayServer(t *testing.T, certificate tls.Certificate) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen rejected gateway server: %v", err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "forbidden", http.StatusForbidden)
		}),
		ReadHeaderTimeout: time.Second,
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(tlsListener) }()
	return listener.Addr().String()
}

func testServerCertificate(t *testing.T, hosts ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "discord-unlocker test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: hosts[0]},
		DNSNames:     hosts,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCertificate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	certificatePEM := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...,
	)
	certificate, err := tls.X509KeyPair(
		certificatePEM,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafKeyDER}),
	)
	if err != nil {
		t.Fatalf("load server keypair: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCertificate)
	return certificate, roots
}
