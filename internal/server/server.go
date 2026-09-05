package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/tanq16/isane/internal/app"
)

const (
	shutdownGrace     = 15 * time.Second
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

type Server struct {
	app     *app.App
	static  fs.FS
	etags   map[string]string
	build   string
	log     zerolog.Logger
	handler http.Handler
}

func New(a *app.App) (*Server, error) {
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("mount embedded static tree: %w", err)
	}
	etags, build, err := fingerprint(static)
	if err != nil {
		return nil, fmt.Errorf("fingerprint embedded static tree: %w", err)
	}
	s := &Server{app: a, static: static, etags: etags, build: build, log: a.Log}
	mux := http.NewServeMux()
	s.routes(mux)
	s.handler = requestID(s.recovery(s.accessLog(s.securityHeaders(s.sameOrigin(mux)))))
	a.Hub.SetOriginCheck(s.originAllowed)
	if a.Cfg().Server.Insecure {
		a.Log.Warn().Msg("server.insecure is set, so session cookies drop the Secure flag, HSTS is not sent, and push is disabled")
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.app.Cfg().Server.Bind,
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	listenErr := make(chan error, 1)
	go func() {
		s.log.Info().Str("addr", srv.Addr).Msg("http listening")
		listenErr <- srv.ListenAndServe()
	}()
	select {
	case err := <-listenErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen %s: %w", srv.Addr, err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http: %w", err)
	}
	return nil
}

func (s *Server) secureCookies() bool {
	return !s.app.Cfg().Server.Insecure
}
