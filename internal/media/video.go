package media

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tanq16/isane/internal/store"
)

type probeResult struct {
	Streams []struct {
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Duration string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (s *Service) processVideo(ctx context.Context, a store.Attachment) (result, error) {
	src := s.abs(a.StoragePath)
	probed, err := s.probeStream(ctx, src)
	if err != nil {
		return result{}, err
	}
	res := result{mime: a.Mime, durationMs: probed.duration()}

	poster := s.abs(filepath.Join(thumbsDir, a.ID.String()+".png"))
	defer func() { _ = removeFile(poster) }()
	if err := s.posterFrame(ctx, src, poster); err != nil {
		s.log.Warn().Err(err).Str("attachment", a.ID.String()).Msg("video poster frame")
		return res, nil
	}

	if width, height, err := posterSize(poster); err != nil {
		s.log.Warn().Err(err).Str("attachment", a.ID.String()).Msg("read poster dimensions")
	} else {
		res.width, res.height = &width, &height
	}

	thumb := filepath.Join(thumbsDir, a.ID.String()+".webp")
	if err := s.thumbnail(ctx, poster, s.abs(thumb)); err != nil {
		s.log.Warn().Err(err).Str("attachment", a.ID.String()).Msg("video thumbnail")
		return res, nil
	}
	res.thumbPath = &thumb
	return res, nil
}

func (s *Service) processAudio(ctx context.Context, a store.Attachment) (result, error) {
	probed, err := s.probeDuration(ctx, s.abs(a.StoragePath))
	if err != nil {
		return result{}, err
	}
	return result{mime: a.Mime, durationMs: probed.duration()}, nil
}

func (s *Service) posterFrame(ctx context.Context, src, dst string) error {
	for _, seek := range []string{"1", ""} {
		args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}
		if seek != "" {
			args = append(args, "-ss", seek)
		}
		args = append(args, "-i", src, "-map_metadata", "-1", "-frames:v", "1", "-f", "image2", "-c:v", "png", dst)
		if _, err := run(ctx, posterTimeout, "ffmpeg", args...); err != nil {
			return fmt.Errorf("extract poster frame: %w", err)
		}
		if fileHasBytes(dst) {
			return nil
		}
	}
	return fmt.Errorf("extract poster frame: no frame written")
}

func posterSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, fmt.Errorf("decode poster frame: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}

func (s *Service) probeStream(ctx context.Context, path string) (probeResult, error) {
	return s.probe(ctx, path, "-select_streams", "v:0",
		"-show_entries", "stream=width,height,duration:format=duration")
}

func (s *Service) probeDuration(ctx context.Context, path string) (probeResult, error) {
	return s.probe(ctx, path, "-show_entries", "format=duration")
}

func (s *Service) probe(ctx context.Context, path string, entries ...string) (probeResult, error) {
	args := append([]string{"-v", "error"}, entries...)
	args = append(args, "-of", "json", path)
	out, err := run(ctx, probeTimeout, "ffprobe", args...)
	if err != nil {
		return probeResult{}, fmt.Errorf("probe media: %w", err)
	}
	var probed probeResult
	if err := json.Unmarshal(out, &probed); err != nil {
		return probeResult{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	return probed, nil
}

func (p probeResult) dimensions() (*int, *int) {
	if len(p.Streams) == 0 || p.Streams[0].Width <= 0 || p.Streams[0].Height <= 0 {
		return nil, nil
	}
	width, height := p.Streams[0].Width, p.Streams[0].Height
	return &width, &height
}

func (p probeResult) duration() *int {
	seconds := p.Format.Duration
	if len(p.Streams) > 0 && parseSeconds(seconds) == nil {
		seconds = p.Streams[0].Duration
	}
	return parseSeconds(seconds)
}

func parseSeconds(raw string) *int {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(seconds) || seconds <= 0 {
		return nil
	}
	ms := math.Round(seconds * 1000)
	if ms > math.MaxInt32 {
		return nil
	}
	value := int(ms)
	return &value
}
