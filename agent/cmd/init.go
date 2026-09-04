package cmd

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tanq16/isane/agent/internal/daemon"
	u "github.com/tanq16/isane/agent/utils"
)

var initFlags struct {
	claimToken string
}

var initCmd = &cobra.Command{
	Use:   "init <handle>",
	Short: "Store an agent's claim token and scaffold its directory",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		handle := validHandle(args[0])
		token := strings.TrimSpace(initFlags.claimToken)
		url := serverURL()

		if err := daemon.NewAPI(url, handle, token).Hello(cmd.Context()); err != nil {
			u.PrintFatal("The server rejected the claim token for "+handle, err)
		}

		s := openStore()
		agent, err := s.Load(handle)
		if err != nil && !errors.Is(err, daemon.ErrNoAgent) {
			u.PrintFatal("Could not read the record for "+handle, err)
		}
		agent.Handle = handle
		agent.ClaimToken = token
		if err := s.Scaffold(agent); err != nil {
			u.PrintFatal("Could not scaffold "+s.Dir(handle), err)
		}
		u.PrintSuccess("Initialized " + handle + " in " + s.Dir(handle))
	},
}

func init() {
	initCmd.Flags().StringVar(&initFlags.claimToken, "claim-token", "",
		"Claim token shown when the handle was reserved, or - to read it from stdin (required)")
	initCmd.MarkFlagRequired("claim-token")
	u.MarkStdinLine(initCmd, "claim-token")
}
