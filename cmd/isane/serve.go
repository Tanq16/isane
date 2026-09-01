package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/tanq16/isane"
	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/http"
	"github.com/tanq16/isane/internal/store"
)

func runServe(cmd *cobra.Command, args []string) {
	cfg, err := config.Load(rootFlags.config)
	if err != nil {
		log.Fatal().Err(err).Str("config", rootFlags.config).Msg("load config")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.Database.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("connect to postgres")
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		log.Fatal().Err(err).Msg("apply migrations")
	}

	a, err := app.New(ctx, cfg, db, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Msg("build application")
	}
	defer a.Close()

	if err := bootstrapInvite(ctx, cfg, db); err != nil {
		log.Fatal().Err(err).Msg("mint bootstrap invite")
	}

	a.Start(ctx)

	if err := http.NewServer(a, isane.WebFS()).ListenAndServe(ctx); err != nil {
		log.Fatal().Err(err).Msg("serve")
	}
	log.Info().Msg("shut down")
}

const bootstrapInviteTTL = 30 * 24 * time.Hour

func bootstrapInvite(ctx context.Context, cfg config.Config, db *store.DB) error {
	users, err := db.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if users > 0 {
		return nil
	}
	raw, hash, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("mint invite token: %w", err)
	}
	err = db.CreateInvite(ctx, store.Invite{
		TokenHash: hash,
		ExpiresAt: time.Now().Add(bootstrapInviteTTL),
	})
	if err != nil {
		return fmt.Errorf("store bootstrap invite: %w", err)
	}
	log.Info().Str("url", strings.TrimSuffix(cfg.Server.PublicURL, "/")+"/invite/"+raw).
		Msg("no accounts exist, open this once to create the first administrator")
	return nil
}
