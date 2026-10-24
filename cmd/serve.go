package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/server"
	"github.com/tanq16/isane/internal/store"
)

var serveFlags struct {
	config string
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the chat server",
	Args:  cobra.NoArgs,
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVarP(&serveFlags.config, "config", "c", "config.yaml", "Path to the configuration file")
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(serveFlags.config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	a, err := app.New(ctx, cfg, db, log.Logger)
	if err != nil {
		return fmt.Errorf("build application: %w", err)
	}
	defer a.Close()

	if err := a.BootstrapInvite(ctx); err != nil {
		return fmt.Errorf("mint bootstrap invite: %w", err)
	}

	a.Start(ctx)

	srv, err := server.New(a)
	if err != nil {
		return fmt.Errorf("build http server: %w", err)
	}

	if err := srv.ListenAndServe(ctx); err != nil {
		return err
	}
	log.Info().Msg("shut down")
	return nil
}
