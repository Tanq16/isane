package utils

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	colorReset  = "\x1b[0m"
	colorBlue   = "\x1b[94m"
	colorGreen  = "\x1b[92m"
	colorRed    = "\x1b[91m"
	colorYellow = "\x1b[93m"
)

func PrintInfo(msg string) {
	if GlobalDebugFlag {
		log.Info().Msg(msg)
		return
	}
	render(colorBlue, "→ "+msg)
}

func PrintSuccess(msg string) {
	if GlobalDebugFlag {
		log.Info().Msg(msg)
		return
	}
	render(colorGreen, "✓ "+msg)
}

func PrintWarn(msg string, err error) {
	if GlobalDebugFlag {
		emit(log.Warn(), msg, err)
		return
	}
	render(colorYellow, "! "+msg)
}

func PrintError(msg string, err error) {
	if GlobalDebugFlag {
		emit(log.Error(), msg, err)
		return
	}
	render(colorRed, "✗ "+msg)
}

func PrintFatal(msg string, err error) {
	PrintError(msg, err)
	os.Exit(1)
}

func PrintGeneric(msg string) {
	render("", msg)
}

func render(color, msg string) {
	if color != "" && StdoutIsTerminal {
		msg = color + msg + colorReset
	}
	fmt.Fprintln(os.Stdout, msg)
}

func emit(e *zerolog.Event, msg string, err error) {
	if err != nil {
		e = e.Err(err)
	}
	e.Msg(msg)
}
