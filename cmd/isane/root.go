package main

import (
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var AppVersion = "dev-build"

var debugFlag bool

var rootFlags struct {
	config string
}

var rootCmd = &cobra.Command{
	Use:               "isane",
	Short:             "Self-hosted team chat, voice, video, and screen share",
	Version:           AppVersion,
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	Run:               runServe,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func setupLogs() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	var out io.Writer = os.Stdout
	if term.IsTerminal(os.Stdout.Fd()) {
		out = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.DateTime}
	}
	log.Logger = zerolog.New(out).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debugFlag {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
}

func init() {
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})

	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Enable debug logging")
	rootCmd.Flags().StringVarP(&rootFlags.config, "config", "c", "config.yaml", "Path to the configuration file")

	cobra.OnInitialize(setupLogs)

	rootCmd.AddCommand(vapidCmd)
}
