package media

import (
	"context"
	"fmt"
	"time"

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
