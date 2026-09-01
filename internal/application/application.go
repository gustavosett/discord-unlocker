package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gustavosett/discord-unlocker/internal/cache"
	"github.com/gustavosett/discord-unlocker/internal/discord"
	"github.com/gustavosett/discord-unlocker/internal/proxy"
	"github.com/gustavosett/discord-unlocker/internal/runtimepaths"
)

const (
	DefaultCacheTTL         = 24 * time.Hour
	DefaultDiscoveryTimeout = 25 * time.Second
)

var ErrNoVerifiedProxy = errors.New("nenhuma proxy SOCKS5 válida fora do Brasil")

type Resolver interface {
	Resolve(context.Context, []proxy.Endpoint, proxy.ProgressFunc) ([]proxy.Endpoint, bool, error)
}

type DiscordController interface {
	RunningProcesses() ([]discord.ProcessInfo, error)
	LaunchWithProxy(context.Context, proxy.Endpoint, time.Duration) (discord.LaunchResult, error)
	LaunchDirect(context.Context) (discord.LaunchResult, error)
}

type DiagnosticLogger interface {
	Printf(string, ...any)
}

type Mode string

const (
	ModeChecked           Mode = "checked"
	ModeProxied           Mode = "proxied"
	ModeDirect            Mode = "direct"
	ModeAlreadyRunning    Mode = "already-running"
	ModeExistingUntouched Mode = "existing-untouched"
)

type Result struct {
	Mode       Mode
	ProxyCount int
	Fetched    bool
	Launch     discord.LaunchResult
	Reason     error
}

type Runner struct {
	Paths            runtimepaths.Paths
	Resolver         Resolver
	Discord          DiscordController
	Logger           DiagnosticLogger
	Now              func() time.Time
	CacheTTL         time.Duration
	DiscoveryTimeout time.Duration
	BootstrapWindow  time.Duration
}

type preparation struct {
	endpoints []proxy.Endpoint
	fetched   bool
	warning   error
}

// Run prepares a verified per-process SOCKS route and applies it only after
// every disk and network step has succeeded. In checkOnly mode it never
// inspects or starts Discord.
func (runner Runner) Run(ctx context.Context, checkOnly bool) (Result, error) {
	if checkOnly {
		prepared, prepareErr := runner.prepare(ctx)
		if prepareErr != nil {
			return Result{}, prepareErr
		}
		return Result{
			Mode:       ModeChecked,
			ProxyCount: len(prepared.endpoints),
			Fetched:    prepared.fetched,
			Reason:     prepared.warning,
		}, nil
	}
	if runner.Discord == nil {
		return Result{}, errors.New("controlador do Discord não configurado")
	}

	// A normal shortcut activation must never tear down a session that is
	// already running. Besides avoiding a surprising restart when the taskbar
	// icon is clicked again, this makes the launcher unable to interrupt an
	// active call or stream. To apply the route to an existing direct session,
	// the user must quit Discord once and reopen it through the managed shortcut.
	running, err := runner.Discord.RunningProcesses()
	if err != nil {
		cause := fmt.Errorf("não foi possível determinar com segurança os processos do Discord: %w", err)
		runner.logf("%v", cause)
		return runner.directOrPreserve(ctx, cause)
	}
	if len(running) != 0 {
		runner.logf("Discord já está aberto; nenhuma reinicialização foi feita.")
		return Result{Mode: ModeAlreadyRunning}, nil
	}

	prepared, prepareErr := runner.prepare(ctx)
	if prepareErr != nil {
		runner.logf("Modo liberado não aplicado: %v", prepareErr)
		return runner.directOrPreserve(ctx, prepareErr)
	}

	// Discovery can take a few seconds. Recheck to avoid racing another Discord
	// launch and accidentally creating a second bootstrap with different flags.
	running, err = runner.Discord.RunningProcesses()
	if err != nil {
		cause := fmt.Errorf("não foi possível determinar com segurança os processos do Discord: %w", err)
		runner.logf("%v", cause)
		return runner.directOrPreserve(ctx, cause)
	}

	if len(running) != 0 {
		runner.logf("Discord foi aberto durante a validação; nenhuma reinicialização foi feita.")
		return Result{Mode: ModeAlreadyRunning}, nil
	}

	selected := prepared.endpoints[0]
	runner.logf("Abrindo Discord com rota SOCKS5 dedicada por %s (%s)...", selected.Address(), selected.Country)
	launch, err := runner.Discord.LaunchWithProxy(ctx, selected, runner.bootstrapWindow())
	if err != nil {
		cause := fmt.Errorf("Discord não sobreviveu ao bootstrap com proxy dedicada: %w", err)
		runner.logf("%v", cause)
		return runner.directOrPreserve(ctx, cause)
	}
	return Result{
		Mode:       ModeProxied,
		ProxyCount: 1,
		Fetched:    prepared.fetched,
		Launch:     launch,
		Reason:     prepared.warning,
	}, nil
}

func (runner Runner) prepare(ctx context.Context) (preparation, error) {
	if runner.Resolver == nil {
		return preparation{}, errors.New("resolvedor de proxies não configurado")
	}
	if err := runner.Paths.Ensure(); err != nil {
		return preparation{}, err
	}

	now := runner.now()
	entries, cacheErr := cache.Load(runner.Paths.CacheFile, now, runner.cacheTTL())
	if cacheErr != nil {
		// Cache is only a hint. Malformed or stale local state must not prevent a
		// clean discovery from the public source.
		runner.logf("Cache ignorado: %v", cacheErr)
		entries = nil
	}
	cached := make([]proxy.Endpoint, 0, len(entries))
	for _, entry := range entries {
		cached = append(cached, proxy.Endpoint{
			Host:       entry.IP,
			Port:       uint16(entry.Port),
			Country:    entry.Country,
			Latency:    entry.Latency,
			VerifiedAt: entry.VerifiedAt,
		})
	}

	discoveryContext, cancel := context.WithTimeout(ctx, runner.discoveryTimeout())
	defer cancel()
	selected, fetched, resolveErr := runner.Resolver.Resolve(discoveryContext, cached, runner.reportProgress)
	if ctx.Err() != nil {
		return preparation{}, ctx.Err()
	}
	if len(selected) == 0 {
		if resolveErr == nil {
			resolveErr = ErrNoVerifiedProxy
		}
		return preparation{fetched: fetched}, fmt.Errorf("%w: %v", ErrNoVerifiedProxy, resolveErr)
	}
	if len(selected) > 3 {
		return preparation{}, fmt.Errorf("resolvedor retornou %d proxies; máximo permitido é 3", len(selected))
	}
	if resolveErr != nil {
		// A deadline or source failure can coexist with one or more candidates
		// which already completed every security check. Those remain safe to use.
		runner.logf("Descoberta terminou parcialmente: %v", resolveErr)
	}

	cacheEntries := make([]cache.Entry, 0, len(selected))
	for _, endpoint := range selected {
		if err := endpoint.Validate(); err != nil {
			return preparation{}, fmt.Errorf("resolvedor retornou endpoint inválido: %w", err)
		}
		cacheEntries = append(cacheEntries, cache.Entry{
			IP:         endpoint.Host,
			Port:       int(endpoint.Port),
			Country:    endpoint.Country,
			Latency:    endpoint.Latency,
			VerifiedAt: endpoint.VerifiedAt,
		})
	}
	if err := cache.Save(runner.Paths.CacheFile, cacheEntries); err != nil {
		return preparation{}, fmt.Errorf("salvar cache validado: %w", err)
	}

	return preparation{endpoints: selected, fetched: fetched, warning: resolveErr}, nil
}

func (runner Runner) directOrPreserve(ctx context.Context, cause error) (Result, error) {
	running, processErr := runner.Discord.RunningProcesses()
	if processErr == nil && len(running) != 0 {
		runner.logf("Discord já estava aberto; processo mantido intacto e sem reinicialização.")
		return Result{Mode: ModeExistingUntouched, Reason: cause}, nil
	}
	if processErr != nil {
		runner.logf("Detecção de processos falhou; a abertura direta será usada sem encerrar nada: %v", processErr)
	}

	launch, launchErr := runner.Discord.LaunchDirect(ctx)
	if launchErr != nil {
		return Result{}, errors.Join(cause, fmt.Errorf("abrir Discord diretamente: %w", launchErr))
	}
	runner.logf("Discord aberto diretamente; modo liberado não foi aplicado.")
	return Result{Mode: ModeDirect, Launch: launch, Reason: cause}, nil
}

func (runner Runner) reportProgress(progress proxy.Progress) {
	stage := map[proxy.ProgressStage]string{
		proxy.ProgressCache: "cache",
		proxy.ProgressFetch: "consulta ProxyScrape",
		proxy.ProgressFresh: "novas proxies",
	}[progress.Stage]
	if stage == "" {
		stage = string(progress.Stage)
	}
	if progress.Total == 0 {
		runner.logf("Proxies: %s...", stage)
		return
	}
	if progress.Checked == 0 {
		runner.logf("Proxies: validando %d candidato(s) de %s...", progress.Total, stage)
		return
	}
	if progress.Checked == progress.Total || progress.Checked%5 == 0 || progress.Verified > 0 {
		runner.logf("Proxies: %s %d/%d testadas, %d válidas.", stage, progress.Checked, progress.Total, progress.Verified)
	}
}

func (runner Runner) logf(format string, args ...any) {
	if runner.Logger != nil {
		runner.Logger.Printf(format, args...)
	}
}

func (runner Runner) now() time.Time {
	if runner.Now != nil {
		return runner.Now()
	}
	return time.Now()
}

func (runner Runner) cacheTTL() time.Duration {
	if runner.CacheTTL > 0 {
		return runner.CacheTTL
	}
	return DefaultCacheTTL
}

func (runner Runner) discoveryTimeout() time.Duration {
	if runner.DiscoveryTimeout > 0 {
		return runner.DiscoveryTimeout
	}
	return DefaultDiscoveryTimeout
}

func (runner Runner) bootstrapWindow() time.Duration {
	if runner.BootstrapWindow > 0 {
		return runner.BootstrapWindow
	}
	return discord.DefaultBootstrapWindow
}

func Describe(result Result) string {
	switch result.Mode {
	case ModeChecked:
		return fmt.Sprintf("Pool validado: %d proxy(s) pronta(s).", result.ProxyCount)
	case ModeProxied:
		return "Discord aberto com rota SOCKS5 dedicada; mídia e UDP permanecem diretos."
	case ModeDirect:
		return "Discord aberto diretamente; o modo liberado não foi aplicado."
	case ModeAlreadyRunning:
		return "Discord já estava aberto; nenhuma reinicialização foi feita."
	case ModeExistingUntouched:
		return "Discord já estava aberto e foi mantido intacto; o modo liberado não foi aplicado."
	default:
		return "Resultado desconhecido: " + strings.TrimSpace(string(result.Mode))
	}
}
