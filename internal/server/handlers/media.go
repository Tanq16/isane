package handlers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"uuid"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const (
	attachmentCache = "private, max-age=31536000, immutable"
	attachmentCSP   = "default-src 'none'; sandbox"
)

type Media struct{ app *app.App }

func NewMedia(a *app.App) *Media { return &Media{app: a} }

func (h *Media) Upload(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.app.Cfg().Media.MaxUploadBytes)
	parts, err := r.MultipartReader()
	if err != nil {
		WriteError(w, badRequestf("expected a multipart upload"))
		return
	}
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			WriteError(w, badRequestf("the upload carries no file part"))
			return
		}
		if err != nil {
			writeUploadError(w, err)
			return
		}
		if part.FileName() == "" {
			part.Close()
			continue
		}
		a, err := h.app.Media.Store(r.Context(), u.ID, part.FileName(), part)
		part.Close()
		if err != nil {
			writeUploadError(w, err)
			return
		}
		h.app.Spawn("process attachment", func(ctx context.Context) { h.process(ctx, a, u.ID) })
		WriteJSON(w, http.StatusCreated, a)
		return
	}
}

func (h *Media) Get(w http.ResponseWriter, r *http.Request) {
	a, ok := h.resolve(w, r)
	if !ok {
		return
	}
	if a.State != store.AttachmentReady && a.State != store.AttachmentFailed {
		WriteError(w, fmt.Errorf("%w: attachment %s is %s", store.ErrNotFound, a.ID, a.State))
		return
	}
	f, err := h.app.Media.Open(a)
	if err != nil {
		WriteError(w, fmt.Errorf("open attachment %s: %w", a.ID, err))
		return
	}
	w.Header().Set("Cache-Control", attachmentCache)
	download := a.Kind == store.AttachmentFile || a.State == store.AttachmentFailed
	serveFile(w, r, f, a.OriginalName, a.Mime, download)
}

func (h *Media) Thumb(w http.ResponseWriter, r *http.Request) {
	a, ok := h.resolve(w, r)
	if !ok {
		return
	}
	if a.State != store.AttachmentReady {
		WriteError(w, fmt.Errorf("%w: attachment %s is %s", store.ErrNotFound, a.ID, a.State))
		return
	}
	if !a.HasThumb() {
		WriteError(w, fmt.Errorf("%w: attachment %s has no thumbnail", store.ErrNotFound, a.ID))
		return
	}
	f, err := h.app.Media.OpenThumb(a)
	if err != nil {
		WriteError(w, fmt.Errorf("open thumbnail for %s: %w", a.ID, err))
		return
	}
	w.Header().Set("Cache-Control", attachmentCache)
	serveFile(w, r, f, filepath.Base(*a.ThumbPath), "", false)
}

func (h *Media) resolve(w http.ResponseWriter, r *http.Request) (store.Attachment, bool) {
	if _, ok := requestUser(w, r); !ok {
		return store.Attachment{}, false
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return store.Attachment{}, false
	}
	a, err := h.app.DB.GetAttachment(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return store.Attachment{}, false
	}
	return a, true
}

func (h *Media) process(ctx context.Context, a store.Attachment, uploaderID uuid.UUID) {
	done, err := h.app.Media.Process(ctx, a.ID)
	if err != nil {
		h.app.Log.Error().Err(err).Str("attachment_id", a.ID.String()).Msg("process attachment")
		return
	}
	frame := socket.NewFrame(socket.TypeAttachment, done)
	if done.MessageID == nil {
		h.app.Hub.ToUser(uploaderID, frame)
		return
	}
	m, err := h.app.DB.GetMessage(ctx, *done.MessageID)
	if err != nil {
		h.app.Log.Error().Err(err).Str("attachment_id", a.ID.String()).Msg("load attachment message")
		return
	}
	h.app.Broadcast(ctx, m.ContainerID, frame)
}

func serveFile(w http.ResponseWriter, r *http.Request, f *os.File, name, mimeType string, download bool) {
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		WriteError(w, fmt.Errorf("stat %s: %w", name, err))
		return
	}
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", attachmentCSP)
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition",
		cmp.Or(mime.FormatMediaType(disposition, map[string]string{"filename": name}), disposition))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func writeUploadError(w http.ResponseWriter, err error) {
	if tooLarge, ok := errors.AsType[*http.MaxBytesError](err); ok {
		WriteJSON(w, http.StatusRequestEntityTooLarge,
			errorBody{Error: fmt.Sprintf("upload exceeds the %d byte limit", tooLarge.Limit)})
		return
	}
	WriteError(w, err)
}
