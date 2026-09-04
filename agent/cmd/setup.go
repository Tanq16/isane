package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	u "github.com/tanq16/isane/agent/utils"
)

var setupFlags struct {
	serverURL string
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Record the isane server URL for this machine",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		url := strings.TrimSuffix(strings.TrimSpace(setupFlags.serverURL), "/")
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			u.PrintFatal("--server-url must start with http:// or https://", nil)
		}
		if err := u.SaveConfig(u.Config{ServerURL: url}); err != nil {
			u.PrintFatal("Could not write the configuration", err)
		}
		u.PrintSuccess("Server URL set to " + url)
	},
}

func init() {
	setupCmd.Flags().StringVarP(&setupFlags.serverURL, "server-url", "s", "",
		"Base URL of the isane server (required)")
	setupCmd.MarkFlagRequired("server-url")
}
