package cmd

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/tanq16/isane/agent/internal/daemon"
	u "github.com/tanq16/isane/agent/utils"
)

var serveFlags struct {
	timeout time.Duration
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Poll the server for every registered agent on this machine",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := daemon.Config{ServerURL: serverURL(), Timeout: serveFlags.timeout}
		err := daemon.Serve(cmd.Context(), cfg, openStore(), &eventPrinter{})
		switch {
		case errors.Is(err, daemon.ErrNoneConfigured):
			u.PrintFatal("No agents are configured, run 'isane-agent init <handle> --claim-token <token>' first", err)
		case errors.Is(err, daemon.ErrNoneRegistered):
			u.PrintFatal("No agent is registered, run 'isane-agent register <handle> --model <model>' first", err)
		case errors.Is(err, daemon.ErrLocked):
			u.PrintFatal("Another isane-agent serve is already running", err)
		case err != nil:
			u.PrintFatal("Could not start serving", err)
		}
		u.PrintSuccess("Stopped")
	},
}

type eventPrinter struct{ pending []byte }

func (p *eventPrinter) Write(b []byte) (int, error) {
	p.pending = append(p.pending, b...)
	for {
		line, rest, ok := bytes.Cut(p.pending, []byte("\n"))
		if !ok {
			return len(b), nil
		}
		p.pending = rest
		printEvent(line)
	}
}

func printEvent(line []byte) {
	var ev daemon.Event
	if err := json.Unmarshal(line, &ev); err != nil {
		u.PrintWarn("Could not read a daemon event", err)
		return
	}
	var err error
	if ev.Error != "" {
		err = errors.New(ev.Error)
	}
	switch ev.Level {
	case daemon.LevelSuccess:
		u.PrintSuccess(ev.Msg)
	case daemon.LevelWarn:
		u.PrintWarn(ev.Msg, err)
	case daemon.LevelError:
		u.PrintError(ev.Msg, err)
	default:
		u.PrintInfo(ev.Msg)
	}
}

func init() {
	serveCmd.Flags().DurationVarP(&serveFlags.timeout, "timeout", "T", 4*time.Minute,
		"Per-job timeout, which has to stay under the server's agents.job_timeout, so raising one means raising the other")
}
