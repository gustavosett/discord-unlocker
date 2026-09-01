package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

type Mode int

const (
	ModeLaunch Mode = iota
	ModeAutostart
	ModeCheck
	ModeVersion
)

type Options struct {
	Mode Mode
}

func Parse(args []string, output io.Writer) (Options, error) {
	set := flag.NewFlagSet("discord-unlocker", flag.ContinueOnError)
	set.SetOutput(output)
	autostart := set.Bool("autostart", false, "inicia silenciosamente com o Windows")
	check := set.Bool("check", false, "valida e atualiza o pool sem reiniciar o Discord")
	version := set.Bool("version", false, "mostra a versão")
	if err := set.Parse(args); err != nil {
		return Options{}, err
	}
	if set.NArg() != 0 {
		return Options{}, fmt.Errorf("argumento inesperado: %s", set.Arg(0))
	}

	selected := 0
	for _, value := range []bool{*autostart, *check, *version} {
		if value {
			selected++
		}
	}
	if selected > 1 {
		return Options{}, errors.New("use somente um entre --autostart, --check e --version")
	}

	switch {
	case *autostart:
		return Options{Mode: ModeAutostart}, nil
	case *check:
		return Options{Mode: ModeCheck}, nil
	case *version:
		return Options{Mode: ModeVersion}, nil
	default:
		return Options{Mode: ModeLaunch}, nil
	}
}
