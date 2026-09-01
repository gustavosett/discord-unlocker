package application

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gustavosett/discord-unlocker/internal/discord"
	"github.com/gustavosett/discord-unlocker/internal/proxy"
	"github.com/gustavosett/discord-unlocker/internal/runtimepaths"
)

type fakeResolver struct {
	endpoints []proxy.Endpoint
	fetched   bool
	err       error
}

func (fake fakeResolver) Resolve(_ context.Context, _ []proxy.Endpoint, progress proxy.ProgressFunc) ([]proxy.Endpoint, bool, error) {
	if progress != nil {
		progress(proxy.Progress{Stage: proxy.ProgressFresh, Checked: 1, Total: 1, Verified: len(fake.endpoints)})
	}
	return append([]proxy.Endpoint(nil), fake.endpoints...), fake.fetched, fake.err
}

type fakeDiscord struct {
	running       []discord.ProcessInfo
	runningErr    error
	terminateErr  error
	proxiedErr    error
	directErr     error
	terminateCall int
	proxiedCall   int
	directCall    int
}

func (fake *fakeDiscord) RunningProcesses() ([]discord.ProcessInfo, error) {
	return append([]discord.ProcessInfo(nil), fake.running...), fake.runningErr
}

func (fake *fakeDiscord) TerminateAll(context.Context, time.Duration) error {
	fake.terminateCall++
	if fake.terminateErr == nil {
		fake.running = nil
	}
	return fake.terminateErr
}

func (fake *fakeDiscord) LaunchWithPAC(context.Context, string, time.Duration) (discord.LaunchResult, error) {
	fake.proxiedCall++
	return discord.LaunchResult{PID: 100}, fake.proxiedErr
}

func (fake *fakeDiscord) LaunchDirect(context.Context) (discord.LaunchResult, error) {
	fake.directCall++
	return discord.LaunchResult{PID: 200}, fake.directErr
}

func validEndpoint(now time.Time) proxy.Endpoint {
	return proxy.Endpoint{Host: "8.8.8.8", Port: 1080, Country: "US", Latency: 100 * time.Millisecond, VerifiedAt: now}
}

func testRunner(t *testing.T, resolver Resolver, controller DiscordController, now time.Time) Runner {
	t.Helper()
	paths, err := runtimepaths.FromLocalAppData(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Runner{
		Paths:            paths,
		Resolver:         resolver,
		Discord:          controller,
		Now:              func() time.Time { return now },
		DiscoveryTimeout: time.Second,
		BootstrapWindow:  time.Millisecond,
	}
}

func TestRunWritesScopedPACThenRestartsDiscord(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	controller := &fakeDiscord{running: []discord.ProcessInfo{{PID: 10}}}
	runner := testRunner(t, fakeResolver{endpoints: []proxy.Endpoint{validEndpoint(now)}}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeProxied || !result.Restarted || controller.terminateCall != 1 || controller.proxiedCall != 1 || controller.directCall != 0 {
		t.Fatalf("resultado/chamadas inesperados: %#v, controller=%#v", result, controller)
	}
	contents, err := os.ReadFile(runner.Paths.PACFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "gateway.discord.gg") || !strings.Contains(string(contents), "SOCKS5 8.8.8.8:1080; DIRECT") {
		t.Fatalf("PAC inesperado:\n%s", contents)
	}
	if _, err := os.Stat(runner.Paths.CacheFile); err != nil {
		t.Fatalf("cache não foi criado: %v", err)
	}
}

func TestNoProxyPreservesRunningDiscord(t *testing.T) {
	now := time.Now()
	controller := &fakeDiscord{running: []discord.ProcessInfo{{PID: 10}}}
	runner := testRunner(t, fakeResolver{err: proxy.ErrNoVerifiedProxies}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeExistingUntouched || controller.terminateCall != 0 || controller.directCall != 0 || controller.proxiedCall != 0 {
		t.Fatalf("Discord aberto foi alterado: %#v, controller=%#v", result, controller)
	}
}

func TestNoProxyOpensDirectWhenDiscordClosed(t *testing.T) {
	now := time.Now()
	controller := &fakeDiscord{}
	runner := testRunner(t, fakeResolver{err: proxy.ErrNoVerifiedProxies}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeDirect || controller.directCall != 1 || controller.terminateCall != 0 {
		t.Fatalf("fallback direto incorreto: %#v, controller=%#v", result, controller)
	}
}

func TestFailedShutdownNeverStartsAnotherDiscord(t *testing.T) {
	now := time.Now()
	controller := &fakeDiscord{
		running:      []discord.ProcessInfo{{PID: 10}},
		terminateErr: errors.New("access denied"),
	}
	runner := testRunner(t, fakeResolver{endpoints: []proxy.Endpoint{validEndpoint(now)}}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeExistingUntouched || controller.terminateCall != 1 ||
		controller.proxiedCall != 0 || controller.directCall != 0 {
		t.Fatalf("falha de encerramento não preservou o Discord: %#v, controller=%#v", result, controller)
	}
}

func TestFailedPACBootstrapFallsBackDirect(t *testing.T) {
	now := time.Now()
	controller := &fakeDiscord{proxiedErr: discord.ErrBootstrapFailed}
	runner := testRunner(t, fakeResolver{endpoints: []proxy.Endpoint{validEndpoint(now)}}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeDirect || controller.proxiedCall != 1 || controller.directCall != 1 {
		t.Fatalf("fallback após bootstrap incorreto: %#v, controller=%#v", result, controller)
	}
}

func TestCheckNeverTouchesDiscord(t *testing.T) {
	now := time.Now()
	runner := testRunner(t, fakeResolver{endpoints: []proxy.Endpoint{validEndpoint(now)}, fetched: true}, nil, now)
	result, err := runner.Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeChecked || result.ProxyCount != 1 || !result.Fetched {
		t.Fatalf("check = %#v", result)
	}
}

func TestDirectLaunchFailureIsFatal(t *testing.T) {
	now := time.Now()
	controller := &fakeDiscord{directErr: errors.New("boom")}
	runner := testRunner(t, fakeResolver{err: proxy.ErrNoVerifiedProxies}, controller, now)
	if _, err := runner.Run(context.Background(), false); err == nil {
		t.Fatal("esperava erro quando PAC e abertura direta falham")
	}
}
