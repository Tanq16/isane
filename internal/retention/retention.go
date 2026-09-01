package retention

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"

	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/media"
	"github.com/tanq16/isane/internal/store"
)

const lastRunKey = "retention_last_run"

type Sweeper struct {
	cfg   config.Config
	db    *store.DB
	media *media.Service
	log   zerolog.Logger
}

func New(cfg config.Config, db *store.DB, media *media.Service, log zerolog.Logger) *Sweeper {
	return &Sweeper{cfg: cfg, db: db, media: media, log: log}
}

func (s *Sweeper) Run(ctx context.Context) error {
	var errs []error

	attachments, err := s.media.Sweep(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("sweep staged attachments: %w", err))
	}

	recordings, err := s.sweepRecordings(ctx)
	if err != nil {
		errs = append(errs, err)
	}

	var messages int64
	if days := s.cfg.Retention.MessageDays; days > 0 {
		messages, err = s.db.DeleteMessagesOlderThan(ctx, daysAgo(days))
		if err != nil {
			errs = append(errs, fmt.Errorf("delete messages: %w", err))
		} else {
			orphaned, err := s.media.Sweep(ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("sweep orphaned attachments: %w", err))
			}
			attachments += orphaned
		}
	}

	if err := s.db.MetaSet(ctx, lastRunKey, time.Now().UTC().Format(time.RFC3339)); err != nil {
		errs = append(errs, fmt.Errorf("record retention run: %w", err))
	}
	s.log.Info().
		Int("attachments", attachments).
		Int("recordings", recordings).
		Int64("messages", messages).
		Msg("retention sweep complete")
	return errors.Join(errs...)
}

func (s *Sweeper) LastRun(ctx context.Context) (time.Time, error) {
	value, err := s.db.MetaGet(ctx, lastRunKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("read retention marker: %w", err)
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse retention marker: %w", err)
	}
	return at, nil
}

func (s *Sweeper) sweepRecordings(ctx context.Context) (int, error) {
	days := s.cfg.Retention.RecordingDays
	if days <= 0 {
		return 0, nil
	}
	recordings, err := s.db.ListRecordingsBefore(ctx, daysAgo(days))
	if err != nil {
		return 0, fmt.Errorf("list recordings: %w", err)
	}

	deleted := 0
	for _, r := range recordings {
		if path := s.resolve(r.StoragePath); path != "" {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				s.log.Error().Err(err).Str("recording", r.ID.String()).Msg("remove recording file")
				continue
			}
			if dir := filepath.Dir(path); dir != s.media.RecordingsDir() {
				_ = os.Remove(dir)
			}
		}
		if err := s.db.DeleteCallRecording(ctx, r.ID); err != nil {
			s.log.Error().Err(err).Str("recording", r.ID.String()).Msg("delete recording row")
			continue
		}
		deleted++
	}
	return deleted, nil
}

func (s *Sweeper) resolve(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(s.cfg.Media.Root, path)
}

func daysAgo(days int) time.Time {
	return time.Now().Add(-24 * time.Hour * time.Duration(days))
}
