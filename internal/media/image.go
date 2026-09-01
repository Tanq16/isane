package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tanq16/isane/internal/store"
)

const (
	defaultMaxDimension = 2560
	thumbMaxDimension   = 512
	imageQuality        = "82"
	thumbQuality        = "75"
)

func (s *Service) processImage(ctx context.Context, a store.Attachment) (result, error) {
	src := s.abs(a.StoragePath)
	tmp := src + ".tmp"
	dimension := s.cfg.Media.ImageMaxDimension
	if dimension <= 0 {
		dimension = defaultMaxDimension
	}

	args := append(magickLimits(),
		src+"[0]",
		"-auto-orient",
		"-strip",
		"-resize", fmt.Sprintf("%dx%d>", dimension, dimension),
		"-quality", imageQuality,
		"webp:"+tmp,
	)
	if _, err := run(ctx, imageTimeout, "magick", args...); err != nil {
		_ = removeFile(tmp)
		return result{}, fmt.Errorf("convert image: %w", err)
	}

	metadata, err := webpHasMetadata(tmp)
	if err != nil {
		_ = removeFile(tmp)
		return result{}, fmt.Errorf("inspect converted image: %w", err)
	}
	if metadata {
		_ = removeFile(tmp)
		return result{}, errors.New("converted image still carries exif or xmp metadata")
	}
	if err := os.Rename(tmp, src); err != nil {
		_ = removeFile(tmp)
		return result{}, fmt.Errorf("replace image: %w", err)
	}

	res := result{mime: "image/webp"}
	probed, err := s.probeStream(ctx, src)
	if err != nil {
		s.log.Debug().Err(err).Str("attachment", a.ID.String()).Msg("probe image dimensions")
	} else {
		res.width, res.height = probed.dimensions()
	}

	thumb := filepath.Join(thumbsDir, a.ID.String()+".webp")
	if err := s.thumbnail(ctx, src, s.abs(thumb)); err != nil {
		return result{}, err
	}
	res.thumbPath = &thumb
	return res, nil
}

func (s *Service) thumbnail(ctx context.Context, src, dst string) error {
	args := append(magickLimits(),
		src+"[0]",
		"-strip",
		"-resize", fmt.Sprintf("%dx%d>", thumbMaxDimension, thumbMaxDimension),
		"-quality", thumbQuality,
		"webp:"+dst,
	)
	if _, err := run(ctx, imageTimeout, "magick", args...); err != nil {
		_ = removeFile(dst)
		return fmt.Errorf("create thumbnail: %w", err)
	}
	return nil
}

func magickLimits() []string {
	return []string{
		"-limit", "memory", "256MiB",
		"-limit", "map", "512MiB",
		"-limit", "disk", "1GiB",
		"-limit", "thread", "2",
	}
}

func webpHasMetadata(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return false, err
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
		return false, fmt.Errorf("%s is not a webp file", filepath.Base(path))
	}
	for {
		var chunk [8]byte
		if _, err := io.ReadFull(f, chunk[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return false, nil
			}
			return false, err
		}
		switch string(chunk[0:4]) {
		case "EXIF", "XMP ":
			return true, nil
		}
		size := int64(binary.LittleEndian.Uint32(chunk[4:8]))
		if size%2 == 1 {
			size++
		}
		if _, err := f.Seek(size, io.SeekCurrent); err != nil {
			return false, err
		}
	}
}
