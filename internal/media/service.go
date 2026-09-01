package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/store"
)

var ErrTooLarge = errors.New("upload exceeds media.max_upload_bytes")

const (
	uploadsDir    = "uploads"
	thumbsDir     = "thumbs"
	recordingsDir = "recordings"

	sniffBytes   = 512
	maxNameBytes = 200
	maxErrBytes  = 500
	stderrCap    = 4096

	probeTimeout  = 30 * time.Second
	imageTimeout  = 2 * time.Minute
	posterTimeout = time.Minute

	waitDelay = 5 * time.Second
)

type Service struct {
	cfg  config.Config
	db   *store.DB
	log  zerolog.Logger
	root string
}

func New(cfg config.Config, db *store.DB, log zerolog.Logger) (*Service, error) {
	root, err := filepath.Abs(cfg.Media.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve media root: %w", err)
	}
	for _, dir := range []string{uploadsDir, thumbsDir, recordingsDir} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return nil, fmt.Errorf("create media directory: %w", err)
		}
	}
	return &Service{cfg: cfg, db: db, log: log, root: root}, nil
}

func (s *Service) Store(ctx context.Context, uploaderID uuid.UUID, name string, r io.Reader) (store.Attachment, error) {
	head := make([]byte, sniffBytes)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return store.Attachment{}, fmt.Errorf("read upload: %w", err)
	}
	head = head[:n]
	kind, mime := sniff(head)

	id := uuid.New()
	rel := filepath.Join(uploadsDir, id.String())
	f, err := os.OpenFile(s.abs(rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return store.Attachment{}, fmt.Errorf("create upload file: %w", err)
	}
	size, err := s.copyLimited(f, io.MultiReader(bytes.NewReader(head), r))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = removeFile(s.abs(rel))
		return store.Attachment{}, err
	}

	a, err := s.db.CreateAttachment(ctx, store.Attachment{
		ID:           id,
		UploaderID:   uploaderID,
		Kind:         kind,
		OriginalName: cleanName(name),
		Mime:         mime,
		SizeBytes:    size,
		StoragePath:  rel,
		State:        store.AttachmentStaged,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		_ = removeFile(s.abs(rel))
		return store.Attachment{}, fmt.Errorf("create attachment: %w", err)
	}
	return a, nil
}

func (s *Service) Process(ctx context.Context, id uuid.UUID) (store.Attachment, error) {
	a, err := s.db.GetAttachment(ctx, id)
	if err != nil {
		return store.Attachment{}, fmt.Errorf("get attachment: %w", err)
	}
	if a.State != store.AttachmentStaged {
		return a, nil
	}
	if err := s.db.SetAttachmentState(ctx, id, store.AttachmentProcessing, nil); err != nil {
		return a, fmt.Errorf("mark attachment processing: %w", err)
	}

	res, err := s.derive(ctx, a)
	if err != nil {
		text := truncate(err.Error(), maxErrBytes)
		if failErr := s.db.SetAttachmentState(ctx, id, store.AttachmentFailed, &text); failErr != nil {
			s.log.Error().Err(failErr).Str("attachment", id.String()).Msg("mark attachment failed")
		}
		return a, fmt.Errorf("process attachment %s: %w", id, err)
	}
	size := a.SizeBytes
	if info, statErr := os.Stat(s.abs(a.StoragePath)); statErr == nil {
		size = info.Size()
	}
	if err := s.db.FinishAttachment(ctx, id, res.mime, size, res.width, res.height, res.durationMs, res.thumbPath); err != nil {
		return a, fmt.Errorf("finish attachment: %w", err)
	}
	a, err = s.db.GetAttachment(ctx, id)
	if err != nil {
		return a, fmt.Errorf("get attachment: %w", err)
	}
	return a, nil
}

type result struct {
	mime       string
	width      *int
	height     *int
	durationMs *int
	thumbPath  *string
}

func (s *Service) derive(ctx context.Context, a store.Attachment) (result, error) {
	switch a.Kind {
	case store.AttachmentImage:
		return s.processImage(ctx, a)
	case store.AttachmentVideo:
		return s.processVideo(ctx, a)
	case store.AttachmentAudio:
		return s.processAudio(ctx, a)
	default:
		return result{mime: a.Mime}, nil
	}
}

func (s *Service) Open(a store.Attachment) (*os.File, error) {
	f, err := os.Open(s.abs(a.StoragePath))
	if err != nil {
		return nil, fmt.Errorf("open attachment %s: %w", a.ID, err)
	}
	return f, nil
}

func (s *Service) OpenThumb(a store.Attachment) (*os.File, error) {
	if !a.HasThumb() {
		return nil, fmt.Errorf("thumbnail for %s: %w", a.ID, fs.ErrNotExist)
	}
	f, err := os.Open(s.abs(*a.ThumbPath))
	if err != nil {
		return nil, fmt.Errorf("open thumbnail %s: %w", a.ID, err)
	}
	return f, nil
}

func (s *Service) Delete(ctx context.Context, a store.Attachment) error {
	if err := removeFile(s.abs(a.StoragePath)); err != nil {
		return err
	}
	if a.HasThumb() {
		if err := removeFile(s.abs(*a.ThumbPath)); err != nil {
			return err
		}
	}
	if err := s.db.DeleteAttachment(ctx, a.ID); err != nil {
		return fmt.Errorf("delete attachment %s: %w", a.ID, err)
	}
	return nil
}

func (s *Service) RecordingsDir() string { return filepath.Join(s.root, recordingsDir) }

func (s *Service) RecordingBytes() (int64, error) {
	var total int64
	err := filepath.WalkDir(s.RecordingsDir(), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("measure recordings: %w", err)
	}
	return total, nil
}

func (s *Service) abs(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.root, p)
}

func (s *Service) copyLimited(dst io.Writer, src io.Reader) (int64, error) {
	limit := s.cfg.Media.MaxUploadBytes
	if limit <= 0 {
		n, err := io.Copy(dst, src)
		if err != nil {
			return n, fmt.Errorf("write upload: %w", err)
		}
		return n, nil
	}
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return n, fmt.Errorf("write upload: %w", err)
	}
	if n > limit {
		return n, ErrTooLarge
	}
	return n, nil
}

func removeFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func fileHasBytes(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func run(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout bytes.Buffer
	var stderr capWriter
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	if err := cmd.Run(); err != nil {
		detail := truncate(strings.TrimSpace(string(stderr.buf)), maxErrBytes)
		if detail == "" {
			return stdout.Bytes(), fmt.Errorf("%s: %w", name, err)
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", name, err, detail)
	}
	return stdout.Bytes(), nil
}

type capWriter struct{ buf []byte }

func (w *capWriter) Write(p []byte) (int, error) {
	if room := stderrCap - len(w.buf); room > 0 {
		w.buf = append(w.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func cleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(truncate(name, maxNameBytes))
	if name == "" {
		return "file"
	}
	return name
}

func truncate(s string, limit int) string {
	if len(s) > limit {
		s = s[:limit]
	}
	return strings.ToValidUTF8(s, "")
}
