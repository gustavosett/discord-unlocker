package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gustavosett/discord-unlocker/internal/application"
	"github.com/gustavosett/discord-unlocker/internal/cli"
	"github.com/gustavosett/discord-unlocker/internal/discord"
	"github.com/gustavosett/discord-unlocker/internal/logging"
	"github.com/gustavosett/discord-unlocker/internal/proxy"
	"github.com/gustavosett/discord-unlocker/internal/runtimepaths"
	"github.com/gustavosett/discord-unlocker/internal/winutil"
)

var version = "dev"

const singletonName = `Local\DiscordUnlocker.Launcher.v1`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	silent := winutil.IsAutostart(args)
	if err := winutil.PrepareConsole(silent); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "discord-unlocker: preparar console: %v\n", err)
		return 1
	}

	options, err := cli.Parse(args, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		_, _ = fmt.Fprintf(os.Stderr, "discord-unlocker: %v\n", err)
		return 2
	}
	if options.Mode == cli.ModeVersion {
		_, _ = fmt.Fprintf(os.Stdout, "discord-unlocker %s\n", version)
		return 0
	}

	paths, err := runtimepaths.ForCurrentUser()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "discord-unlocker: %v\n", err)
		return 1
	}
	var console io.Writer
	if options.Mode != cli.ModeAutostart {
		console = os.Stdout
	}
	logger, err := logging.Open(paths.LogFile, console)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "discord-unlocker: %v\n", err)
		return 1
	}
	defer logger.Close()

	mutex, err := winutil.AcquireNamedMutex(singletonName)
	if err != nil {
		if errors.Is(err, winutil.ErrAlreadyRunning) {
			logger.Printf("Outra execução do Discord Unlocker já está em andamento; esta execução será encerrada.")
			return 0
		}
		logger.Printf("Erro ao adquirir trava de execução: %v", err)
		return 1
	}
	defer mutex.Close()

	logger.Printf("Discord Unlocker %s iniciado (%s).", version, modeName(options.Mode))
	resolver := proxy.Resolver{}
	runner := application.Runner{
		Paths:    paths,
		Resolver: resolver,
		Logger:   logger,
	}

	checkOnly := options.Mode == cli.ModeCheck
	if !checkOnly {
		installation, findErr := discord.FindStableFromEnvironment()
		if findErr != nil {
			logger.Printf("Discord Stable não encontrado: %v", findErr)
			return 1
		}
		client, clientErr := discord.NewClient(installation)
		if clientErr != nil {
			logger.Printf("Instalação do Discord não pôde ser usada: %v", clientErr)
			return 1
		}
		runner.Discord = client
		logger.Printf("Discord Stable %s localizado em %s.", installation.Version, installation.AppDir)
	}

	result, err := runner.Run(context.Background(), checkOnly)
	if err != nil {
		logger.Printf("Erro: %v", err)
		return 1
	}
	logger.Printf("%s", application.Describe(result))
	if result.Reason != nil {
		logger.Printf("Diagnóstico: %v", result.Reason)
	}
	return 0
}

func modeName(mode cli.Mode) string {
	switch mode {
	case cli.ModeAutostart:
		return "autostart"
	case cli.ModeCheck:
		return "check"
	default:
		return "manual"
	}
}
