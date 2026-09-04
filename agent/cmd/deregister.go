package cmd

import (
	"github.com/spf13/cobra"

	"github.com/tanq16/isane/agent/internal/daemon"
	u "github.com/tanq16/isane/agent/utils"
)

var deregisterCmd = &cobra.Command{
	Use:   "deregister <handle>",
	Short: "Return an agent to reserved, leaving its directory intact",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		handle := validHandle(args[0])
		url := serverURL()
		agent := loadAgent(openStore(), handle)
		if err := daemon.NewAPI(url, handle, agent.ClaimToken).Deregister(cmd.Context()); err != nil {
			u.PrintFatal("The server refused to deregister "+handle, err)
		}
		u.PrintSuccess("Deregistered " + handle)
	},
}
