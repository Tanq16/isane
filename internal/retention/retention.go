package retention

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/media"
	"github.com/tanq16/isane/internal/store"
)

const lastRunKey = "retention_last_run"

type Sweeper struct {
	cfg   *config.Config
	db    *store.DB
	media *media.Service
}

type Report struct {
	Attachments   int
	OrphanUploads int
	Recordings    int
	AuditEvents   int64
	Messages      int64
}

func New(cfg *config.Config, db *store.DB, media *media.Service) *Sweeper {
	return &Sweeper{cfg: cfg, db: db, media: media}
}

func (s *Sweeper) Run(ctx context.Context) (Report, error) {
	var report Report
	var errs []error
	var err error

	report.Attachments, err = s.media.Sweep(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("sweep staged attachments: %w", err))
	}

	report.OrphanUploads, err = s.media.SweepUploads(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("sweep orphan uploads: %w", err))
	}

	report.Recordings, err = s.sweepRecordings(ctx)
	if err != nil {
		errs = append(errs, err)
	}

	if days := s.cfg.Retention.AuditDays; days > 0 {
		report.AuditEvents, err = s.db.DeleteAuditEventsBefore(ctx, daysAgo(days))
		if err != nil {
			errs = append(errs, err)
		}
	}

	if days := s.cfg.Retention.MessageDays; days > 0 {
		report.Messages, err = s.db.DeleteMessagesOlderThan(ctx, daysAgo(days))
		if err != nil {
			errs = append(errs, fmt.Errorf("delete messages: %w", err))
		} else {
			orphaned, err := s.media.Sweep(ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("sweep orphaned attachments: %w", err))
			}
			report.Attachments += orphaned
		}
	}

	if err := s.db.MetaSet(ctx, lastRunKey, time.Now().UTC().Format(time.RFC3339)); err != nil {
		errs = append(errs, fmt.Errorf("record retention run: %w", err))
	}
	return report, errors.Join(errs...)
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
		return 0, err
	}

	deleted := 0
	var errs []error
	for _, r := range recordings {
		select {
		case <-ctx.Done():
			return deleted, errors.Join(append(errs, ctx.Err())...)
		default:
		}
		if path := s.resolve(r.StoragePath); path != "" {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, fmt.Errorf("recording %s: %w", r.ID, err))
				continue
			}
		}
		if err := s.db.DeleteCallRecording(ctx, r.ID); err != nil {
			errs = append(errs, fmt.Errorf("recording %s: %w", r.ID, err))
			continue
		}
		deleted++
	}
	return deleted, errors.Join(errs...)
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
