package http

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/tanq16/isane/internal/http/handlers"
	"github.com/tanq16/isane/internal/store"
)

const serviceWorkerPath = "sw.js"

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		handlers.WriteError(w, fmt.Errorf("%w: no such endpoint", store.ErrNotFound))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		handlers.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." || !fs.ValidPath(name) {
		s.serveIndex(w, r)
		return
	}
	info, err := fs.Stat(s.web, name)
	if err != nil || info.IsDir() {
		s.serveIndex(w, r)
		return
	}
	if name == serviceWorkerPath {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	http.ServeFileFS(w, r, s.web, name)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	page, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		s.log.Error().Err(err).Msg("read index.html")
		handlers.WriteError(w, fmt.Errorf("%w: index.html", store.ErrNotFound))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(page)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DB.Pool.Ping(r.Context()); err != nil {
		handlers.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unreachable"})
		return
	}
	handlers.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
