package main

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/tanq16/isane/internal/push"
)

var vapidCmd = &cobra.Command{
	Use:   "vapid",
	Short: "Generate a VAPID key pair for Web Push",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		public, private, err := push.GenerateVAPIDKeys()
		if err != nil {
			log.Fatal().Err(err).Msg("generate vapid key pair")
		}
		fmt.Println("push:")
		fmt.Println("  vapid_public_key: " + public)
		fmt.Println("  vapid_private_key: " + private)
	},
}
