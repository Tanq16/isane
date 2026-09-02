package server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/tanq16/isane/internal/server/handlers"
	"github.com/tanq16/isane/internal/store"
)

//go:embed static
var staticFiles embed.FS

const (
	serviceWorkerPath = "sw.js"
	buildIDLength     = 12
)

func fingerprint(files fs.FS) (map[string]string, string, error) {
	etags := make(map[string]string)
	tree := sha256.New()
	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		etags[path] = `"` + hex.EncodeToString(sum[:]) + `"`
		fmt.Fprintf(tree, "%s %x\n", path, sum)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return etags, hex.EncodeToString(tree.Sum(nil))[:buildIDLength], nil
}

func (s *Server) revalidated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag, ok := s.etags[r.URL.Path]; ok {
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	handlers.WriteJSON(w, http.StatusOK, map[string]string{"build": s.build})
}

func (s *Server) serveWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFileFS(w, r, s.static, serviceWorkerPath)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		handlers.WriteError(w, fmt.Errorf("%w: no such endpoint", store.ErrNotFound))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		handlers.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	page, err := fs.ReadFile(s.static, "index.html")
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
