package handlers

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"uuid"

	"github.com/rs/zerolog/log"
	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/store"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrBadRequest   = errors.New("bad request")
)

const (
	maxJSONBody   = 1 << 20
	maxNameLength = 64
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxAgent
)

func WithUser(ctx context.Context, u store.User) context.Context {
	return context.WithValue(ctx, ctxUser, u)
}

func UserFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(ctxUser).(store.User)
	return u, ok
}

func WithAgent(ctx context.Context, a store.AgentInfo) context.Context {
	return context.WithValue(ctx, ctxAgent, a)
}

func AgentFrom(ctx context.Context) (store.AgentInfo, bool) {
	a, ok := ctx.Value(ctxAgent).(store.AgentInfo)
	return a, ok
}

func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); !ok {
			WriteError(w, errSignIn())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFrom(r.Context())
		if !ok {
			WriteError(w, errSignIn())
			return
		}
		if !u.IsAdmin {
			WriteError(w, forbiddenf("administrator access required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if v == nil {
		w.WriteHeader(status)
		return
	}
	body, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("encode response")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal error"}`))
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func WriteError(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "internal error"
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, store.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, ErrUnauthorized):
		status, message = http.StatusUnauthorized, err.Error()
	case errors.Is(err, ErrForbidden), errors.Is(err, app.ErrForbidden):
		status, message = http.StatusForbidden, err.Error()
	case errors.Is(err, ErrBadRequest), errors.Is(err, app.ErrInvalid):
		status, message = http.StatusBadRequest, err.Error()
	default:
		log.Error().Err(err).Msg("request failed")
	}
	WriteJSON(w, status, errorBody{Error: message})
}

func ReadJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	if err := json.UnmarshalRead(io.LimitReader(r.Body, maxJSONBody), v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	return nil
}

type errorBody struct {
	Error string `json:"error"`
}

type okBody struct {
	OK bool `json:"ok"`
}

func writeOK(w http.ResponseWriter) {
	WriteJSON(w, http.StatusOK, okBody{OK: true})
}

func requestUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	u, ok := UserFrom(r.Context())
	if !ok {
		WriteError(w, errSignIn())
	}
	return u, ok
}

func errSignIn() error {
	return fmt.Errorf("%w: sign in required", ErrUnauthorized)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func boundedField(field, raw string, limit int) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", badRequestf("%s is required", field)
	}
	if len(value) > limit {
		return "", badRequestf("%s must be at most %d characters", field, limit)
	}
	return value, nil
}

func badRequestf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrBadRequest, fmt.Sprintf(format, a...))
}

func forbiddenf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrForbidden, fmt.Sprintf(format, a...))
}

func conflictf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", store.ErrConflict, fmt.Sprintf(format, a...))
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil(), badRequestf("%s must be a uuid", name)
	}
	return id, nil
}

func queryUUID(r *http.Request, name string) (*uuid.UUID, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, badRequestf("%s must be a uuid", name)
	}
	return &id, nil
}

func queryInt64(r *http.Request, name string) (int64, bool, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, badRequestf("%s must be an integer", name)
	}
	return n, true, nil
}

func queryLimit(r *http.Request, fallback, ceiling int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return fallback
	}
	return min(n, ceiling)
}

func queryBool(r *http.Request, name string) bool {
	b, err := strconv.ParseBool(r.URL.Query().Get(name))
	return err == nil && b
}
