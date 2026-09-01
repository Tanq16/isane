package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
	"uuid"

	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/http/handlers"
	"github.com/tanq16/isane/internal/store"
)

const (
	agentHandleHeader    = "X-Isane-Agent"
	bearerPrefix         = "Bearer "
	sessionTouchInterval = time.Hour
)

type ctxKey int

const ctxRequestID ctxKey = iota

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New().String()
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))
	})
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

func (s *Server) recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			s.log.Error().
				Str("request_id", requestIDFrom(r.Context())).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Interface("panic", rec).
				Bytes("stack", debug.Stack()).
				Msg("handler panic")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal error"}`))
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		event := s.log.Info()
		switch {
		case rec.status >= http.StatusInternalServerError:
			event = s.log.Error()
		case rec.status >= http.StatusBadRequest:
			event = s.log.Warn()
		}
		event.
			Str("request_id", requestIDFrom(r.Context())).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.status).
			Int64("bytes", rec.bytes).
			Dur("duration", time.Since(start)).
			Msg("request")
	})
}

func (s *Server) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := auth.ReadSession(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		sess, u, err := s.app.DB.SessionByTokenHash(r.Context(), auth.HashToken(raw))
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				s.log.Error().Err(err).Str("request_id", requestIDFrom(r.Context())).Msg("load session")
			}
			auth.ClearSession(w, s.secureCookies())
			next.ServeHTTP(w, r)
			return
		}
		if !u.Active() {
			auth.ClearSession(w, s.secureCookies())
			next.ServeHTTP(w, r)
			return
		}
		if time.Since(sess.LastSeenAt) > sessionTouchInterval {
			expires := time.Now().Add(auth.SessionTTL)
			if err := s.app.DB.TouchSession(r.Context(), sess.ID, expires); err != nil {
				s.log.Error().Err(err).Msg("extend session")
			} else {
				auth.SetSession(w, raw, expires, s.secureCookies())
			}
		}
		next.ServeHTTP(w, r.WithContext(handlers.WithUser(r.Context(), u)))
	})
}

func (s *Server) agentAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
		handle := strings.TrimSpace(r.Header.Get(agentHandleHeader))
		if !ok || token == "" || handle == "" {
			handlers.WriteError(w, fmt.Errorf("%w: agent claim token required", handlers.ErrUnauthorized))
			return
		}
		info, err := s.app.Agents.Authenticate(r.Context(), handle, token)
		if err != nil {
			s.log.Warn().Err(err).Str("handle", handle).Msg("agent authentication failed")
			handlers.WriteError(w, fmt.Errorf("%w: invalid agent claim token", handlers.ErrUnauthorized))
			return
		}
		next.ServeHTTP(w, r.WithContext(handlers.WithAgent(r.Context(), info)))
	})
}

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (rw *recorder) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *recorder) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += int64(n)
	return n, err
}

func (rw *recorder) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *recorder) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
