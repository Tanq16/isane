package cmd

import (
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
		if err := daemon.Serve(cmd.Context(), cfg, openStore()); err != nil {
			u.PrintFatal(err.Error(), err)
		}
		u.PrintSuccess("Stopped")
	},
}

func init() {
	serveCmd.Flags().DurationVarP(&serveFlags.timeout, "timeout", "T", 4*time.Minute,
		"Per-job timeout, which has to stay under the server's agents.job_timeout, so raising one means raising the other")
}
