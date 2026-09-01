package application

import (
	"context"
	"errors"
	"os"
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
	calls     *int
}

func (fake fakeResolver) Resolve(_ context.Context, _ []proxy.Endpoint, progress proxy.ProgressFunc) ([]proxy.Endpoint, bool, error) {
	if fake.calls != nil {
		(*fake.calls)++
	}
	if progress != nil {
		progress(proxy.Progress{Stage: proxy.ProgressFresh, Checked: 1, Total: 1, Verified: len(fake.endpoints)})
	}
	return append([]proxy.Endpoint(nil), fake.endpoints...), fake.fetched, fake.err
}

type fakeDiscord struct {
	running         []discord.ProcessInfo
	runningSequence [][]discord.ProcessInfo
	runningErr      error
	proxiedErr      error
	directErr       error
	runningCall     int
	proxiedCall     int
	directCall      int
	proxiedEndpoint proxy.Endpoint
}

func (fake *fakeDiscord) RunningProcesses() ([]discord.ProcessInfo, error) {
	fake.runningCall++
	if fake.runningErr != nil {
		return nil, fake.runningErr
	}
	if len(fake.runningSequence) != 0 {
		index := fake.runningCall - 1
		if index >= len(fake.runningSequence) {
			index = len(fake.runningSequence) - 1
		}
		return append([]discord.ProcessInfo(nil), fake.runningSequence[index]...), nil
	}
	return append([]discord.ProcessInfo(nil), fake.running...), nil
}

func (fake *fakeDiscord) LaunchWithProxy(_ context.Context, endpoint proxy.Endpoint, _ time.Duration) (discord.LaunchResult, error) {
	fake.proxiedCall++
	fake.proxiedEndpoint = endpoint
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

func TestRunCachesPoolAndLaunchesFastestDedicatedProxy(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	fastest := validEndpoint(now)
	slower := proxy.Endpoint{Host: "1.1.1.1", Port: 1081, Country: "DE", Latency: time.Second, VerifiedAt: now}
	controller := &fakeDiscord{}
	runner := testRunner(t, fakeResolver{endpoints: []proxy.Endpoint{fastest, slower}}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeProxied || result.ProxyCount != 1 || controller.proxiedCall != 1 || controller.directCall != 0 {
		t.Fatalf("resultado/chamadas inesperados: %#v, controller=%#v", result, controller)
	}
	if controller.proxiedEndpoint.Address() != fastest.Address() {
		t.Fatalf("proxy usada = %s, esperava %s", controller.proxiedEndpoint.Address(), fastest.Address())
	}
	if _, err := os.Stat(runner.Paths.CacheFile); err != nil {
		t.Fatalf("cache não foi criado: %v", err)
	}
}

func TestExistingDiscordIsNeverRestartedOrRevalidated(t *testing.T) {
	now := time.Now()
	resolverCalls := 0
	controller := &fakeDiscord{running: []discord.ProcessInfo{{PID: 10}}}
	runner := testRunner(t, fakeResolver{
		endpoints: []proxy.Endpoint{validEndpoint(now)},
		calls:     &resolverCalls,
	}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeAlreadyRunning || resolverCalls != 0 || controller.proxiedCall != 0 || controller.directCall != 0 {
		t.Fatalf("Discord aberto foi alterado: %#v, resolver=%d controller=%#v", result, resolverCalls, controller)
	}
}

func TestDiscordOpenedDuringDiscoveryIsPreserved(t *testing.T) {
	now := time.Now()
	controller := &fakeDiscord{runningSequence: [][]discord.ProcessInfo{
		nil,
		{{PID: 10}},
	}}
	runner := testRunner(t, fakeResolver{endpoints: []proxy.Endpoint{validEndpoint(now)}}, controller, now)

	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeAlreadyRunning || controller.proxiedCall != 0 || controller.directCall != 0 {
		t.Fatalf("corrida de inicialização não foi preservada: %#v, controller=%#v", result, controller)
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
	if result.Mode != ModeDirect || controller.directCall != 1 || controller.proxiedCall != 0 {
		t.Fatalf("fallback direto incorreto: %#v, controller=%#v", result, controller)
	}
}

func TestFailedProxyBootstrapFallsBackDirect(t *testing.T) {
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
		t.Fatal("esperava erro quando proxy e abertura direta falham")
	}
}

func TestProcessDiscoveryFailureNeverLaunchesProxy(t *testing.T) {
	now := time.Now()
	controller := &fakeDiscord{runningErr: errors.New("access denied")}
	runner := testRunner(t, fakeResolver{endpoints: []proxy.Endpoint{validEndpoint(now)}}, controller, now)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if controller.proxiedCall != 0 || controller.directCall != 1 {
		t.Fatalf("detecção incerta lançou proxy: %#v", controller)
	}
}
