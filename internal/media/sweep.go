package media

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

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
	orphaned, err := s.db.ListOrphanedAttachments(ctx)
	if err != nil {
		return 0, fmt.Errorf("list orphaned attachments: %w", err)
	}

	swept := 0
	seen := make(map[uuid.UUID]struct{}, len(candidates)+len(orphaned))
	for _, a := range slices.Concat(candidates, orphaned) {
		if _, done := seen[a.ID]; done {
			continue
		}
		seen[a.ID] = struct{}{}
		if err := s.Delete(ctx, a); err != nil {
			s.log.Error().Err(err).Str("attachment", a.ID.String()).Msg("sweep attachment")
			continue
		}
		swept++
	}
	return swept, nil
}
