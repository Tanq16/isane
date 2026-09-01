package main

import (
	"github.com/spf13/cobra"

	"github.com/tanq16/isane/internal/push"
	u "github.com/tanq16/isane/utils"
)

var vapidCmd = &cobra.Command{
	Use:   "vapid",
	Short: "Generate a VAPID key pair for Web Push",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		public, private, err := push.GenerateVAPIDKeys()
		if err != nil {
			u.PrintFatal("Failed to generate a VAPID key pair", err)
		}
		u.PrintInfo("Paste these into config.yaml:")
		u.PrintGeneric("push:")
		u.PrintGeneric("  vapid_public_key: " + public)
		u.PrintGeneric("  vapid_private_key: " + private)
	},
}
