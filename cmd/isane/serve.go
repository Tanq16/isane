package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/tanq16/isane"
	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/http"
	"github.com/tanq16/isane/internal/store"
	u "github.com/tanq16/isane/utils"
)

const bootstrapInviteKey = "bootstrap_invite"

func runServe(cmd *cobra.Command, args []string) {
	cfg, err := config.Load(rootFlags.config)
	if err != nil {
		u.PrintFatal("Failed to load "+rootFlags.config, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.Database.URL)
	if err != nil {
		u.PrintFatal("Failed to reach Postgres", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		u.PrintFatal("Failed to apply migrations", err)
	}

	a, err := app.New(ctx, cfg, db, log.Logger)
	if err != nil {
		u.PrintFatal("Failed to build the application", err)
	}
	defer a.Close()

	if err := bootstrapInvite(ctx, cfg, db); err != nil {
		u.PrintFatal("Failed to mint the bootstrap invite", err)
	}

	a.Start(ctx)

	if err := http.NewServer(a, isane.WebFS()).ListenAndServe(ctx); err != nil {
		u.PrintFatal("Server failed", err)
	}
	u.PrintSuccess("Shut down")
}

func bootstrapInvite(ctx context.Context, cfg config.Config, db *store.DB) error {
	users, err := db.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if users > 0 {
		if err := db.MetaSet(ctx, bootstrapInviteKey, ""); err != nil {
			return fmt.Errorf("clear bootstrap invite: %w", err)
		}
		return nil
	}
	raw, hash, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("mint invite token: %w", err)
	}
	if err := db.MetaSet(ctx, bootstrapInviteKey, hex.EncodeToString(hash)); err != nil {
		return fmt.Errorf("store bootstrap invite: %w", err)
	}
	u.PrintInfo("No accounts exist. Open this once to create the first administrator:")
	u.PrintGeneric(strings.TrimSuffix(cfg.Server.PublicURL, "/") + "/invite/" + raw)
	return nil
}
