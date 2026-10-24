package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/store"
)

const (
	bootstrapInviteTTL  = 30 * 24 * time.Hour
	bootstrapInviteFile = "bootstrap-invite.url"
)

func (a *App) BootstrapInvite(ctx context.Context) error {
	users, err := a.DB.CountUsers(ctx)
	if err != nil {
		return err
	}
	if users > 0 {
		return nil
	}
	raw, hash, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("mint invite token: %w", err)
	}
	err = a.DB.CreateInvite(ctx, store.Invite{
		TokenHash: hash,
		ExpiresAt: time.Now().Add(bootstrapInviteTTL),
	})
	if err != nil {
		return err
	}
	url := strings.TrimSuffix(a.cfg.Server.PublicURL, "/") + "/invite/" + raw
	path := filepath.Join(a.cfg.Media.Root, bootstrapInviteFile)
	if err := os.WriteFile(path, []byte(url+"\n"), 0o600); err != nil {
		return fmt.Errorf("write bootstrap invite: %w", err)
	}
	a.Log.Info().Str("path", path).
		Msg("no accounts exist, open the url in this file once to create the first administrator")
	return nil
}
