package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
	"uuid"

	"github.com/tanq16/isane/internal/store"
)

func (s *Service) Sweep(ctx context.Context) (int, error) {
	var candidates []store.Attachment
	if hours := s.cfg.Retention.StagedUploadHours; hours > 0 {
		staged, err := s.db.ListStagedBefore(ctx, time.Now().Add(-time.Duration(hours)*time.Hour))
		if err != nil {
			return 0, fmt.Errorf("list staged attachments: %w", err)
		}
		candidates = staged
	}
	swept := 0
	for _, a := range candidates {
		if err := s.Delete(ctx, a); err != nil {
			s.log.Error().Err(err).Str("attachment", a.ID.String()).Msg("sweep attachment")
			continue
		}
		swept++
	}
	return swept, nil
}

func (s *Service) SweepUploads(ctx context.Context) (int, error) {
	hours := s.cfg.Retention.StagedUploadHours
	if hours <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	entries, err := os.ReadDir(filepath.Join(s.root, uploadsDir))
	if err != nil {
		return 0, fmt.Errorf("list upload files: %w", err)
	}
	stale := map[uuid.UUID]string{}
	for _, e := range entries {
		id, err := uuid.Parse(e.Name())
		if err != nil || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("stat upload file: %w", err)
		}
		if info.ModTime().Before(cutoff) {
			stale[id] = filepath.Join(uploadsDir, e.Name())
		}
	}
	if len(stale) == 0 {
		return 0, nil
	}
	known, err := s.db.ExistingAttachmentIDs(ctx, slices.Collect(maps.Keys(stale)))
	if err != nil {
		return 0, err
	}
	for _, id := range known {
		delete(stale, id)
	}
	swept := 0
	for id, rel := range stale {
		if err := removeFile(s.abs(rel)); err != nil {
			s.log.Error().Err(err).Str("upload", id.String()).Msg("sweep orphan upload")
			continue
		}
		swept++
	}
	return swept, nil
}
