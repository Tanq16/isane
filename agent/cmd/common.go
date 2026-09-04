package cmd

import (
	"errors"

	"github.com/tanq16/isane/agent/internal/daemon"
	u "github.com/tanq16/isane/agent/utils"
)

func openStore() *daemon.Store {
	dir, err := u.ConfigDir()
	if err != nil {
		u.PrintFatal("Could not resolve the config directory", err)
	}
	return daemon.NewStore(dir)
}

func serverURL() string {
	cfg, err := u.LoadConfig()
	if errors.Is(err, u.ErrNoConfig) {
		u.PrintFatal("Run 'isane-agent setup --server-url <url>' first", err)
	}
	if err != nil {
		u.PrintFatal("Could not read the configuration", err)
	}
	return cfg.ServerURL
}

func validHandle(handle string) string {
	if err := daemon.ValidateHandle(handle); err != nil {
		u.PrintFatal("Invalid handle "+handle, err)
	}
	return handle
}

func loadAgent(s *daemon.Store, handle string) daemon.Agent {
	agent, err := s.Load(handle)
	if errors.Is(err, daemon.ErrNoAgent) {
		u.PrintFatal("Run 'isane-agent init "+handle+" --claim-token <token>' first", err)
	}
	if err != nil {
		u.PrintFatal("Could not read the record for "+handle, err)
	}
	return agent
}
