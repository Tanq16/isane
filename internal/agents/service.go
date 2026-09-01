package agents

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/store"
)

var ErrClaim = errors.New("invalid claim token")

type Service struct {
	cfg config.Config
	db  *store.DB
	log zerolog.Logger

	mu      sync.Mutex
	waiters map[uuid.UUID]map[chan struct{}]struct{}
}

func New(cfg config.Config, db *store.DB, log zerolog.Logger) *Service {
	return &Service{
		cfg:     cfg,
		db:      db,
		log:     log,
		waiters: make(map[uuid.UUID]map[chan struct{}]struct{}),
	}
}

func (s *Service) Authenticate(ctx context.Context, handle, claimToken string) (store.AgentInfo, error) {
	info, err := s.db.AgentByHandle(ctx, handle)
	if err != nil {
		return store.AgentInfo{}, fmt.Errorf("authenticate %s: %w", handle, err)
	}
	if claimToken == "" {
		return store.AgentInfo{}, ErrClaim
	}
	if subtle.ConstantTimeCompare(info.Agent.ClaimTokenHash, auth.HashToken(claimToken)) != 1 {
		return store.AgentInfo{}, ErrClaim
	}
	return info, nil
}

func (s *Service) Register(ctx context.Context, a store.AgentInfo, argv []string, allowHistory bool) error {
	if err := s.db.RegisterAgent(ctx, a.User.ID, argv, allowHistory); err != nil {
		return fmt.Errorf("register %s: %w", a.User.Handle, err)
	}
	s.log.Info().Str("agent", a.User.Handle).Bool("allow_history", allowHistory).Msg("agent registered")
	return nil
}

func (s *Service) Deregister(ctx context.Context, a store.AgentInfo) error {
	if err := s.db.DeregisterAgent(ctx, a.User.ID); err != nil {
		return fmt.Errorf("deregister %s: %w", a.User.Handle, err)
	}
	s.log.Info().Str("agent", a.User.Handle).Msg("agent deregistered")
	return nil
}
