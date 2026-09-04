package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tanq16/isane/agent/internal/daemon"
	u "github.com/tanq16/isane/agent/utils"
)

type modelValue string

func (m *modelValue) String() string { return string(*m) }

func (m *modelValue) Type() string { return "claude-*|gemini-*|gpt-*|alias|custom" }

func (m *modelValue) Set(v string) error {
	if !daemon.KnownModel(v) {
		return fmt.Errorf("must start with claude-, gemini-, or gpt-, be one of %s, or be %s",
			strings.Join(daemon.ClaudeAliases, ", "), daemon.CustomModel)
	}
	*m = modelValue(v)
	return nil
}

var registerFlags struct {
	model        modelValue
	command      string
	allowHistory bool
}

var registerCmd = &cobra.Command{
	Use:   "register <handle>",
	Short: "Register an agent with the server so it starts receiving jobs",
	Long: `Register an agent with the server so it starts receiving jobs.

--model picks the harness and the flags it is run with. Every derived command carries the permission bypass its harness needs to write a file in headless mode.

  claude-*   claude, such as claude-opus-5, claude-opus-4-8, claude-sonnet-5, claude-fable-5, claude-haiku-4-5
  alias      claude, one of default, best, fable, sonnet, opus, haiku, sonnet[1m], opus[1m], opusplan
  gemini-*   agy, such as gemini-3.8-flash. An id with no -low, -medium, or -high suffix gets -medium, which agy requires.
  gpt-*      codex, such as gpt-6-astra, gpt-5.6-sol, gpt-5.6-terra, gpt-5.6-luna, gpt-5.3-codex-spark, gpt-5.5, gpt-5.4, gpt-5.4-mini
  custom     the --command string, which has to carry {{prompt}} itself`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		model := string(registerFlags.model)
		command := strings.TrimSpace(registerFlags.command)
		if model == daemon.CustomModel && command == "" {
			u.PrintFatal("--model custom needs --command", nil)
		}
		if model != daemon.CustomModel && command != "" {
			u.PrintFatal("--command applies only to --model "+daemon.CustomModel, nil)
		}
		handle := validHandle(args[0])
		url := serverURL()

		argv, err := daemon.Argv(model, command)
		if err != nil {
			u.PrintFatal("Could not derive the command", err)
		}
		if strings.HasPrefix(argv[0], ".") {
			u.PrintFatal("The command must name a binary on PATH or an absolute path, not "+argv[0], nil)
		}

		s := openStore()
		agent := loadAgent(s, handle)
		agent.Argv = argv
		agent.AllowHistory = registerFlags.allowHistory
		if err := s.Save(agent); err != nil {
			u.PrintFatal("Could not write the record for "+handle, err)
		}
		api := daemon.NewAPI(url, handle, agent.ClaimToken)
		if err := api.Register(cmd.Context(), agent.Argv, agent.AllowHistory); err != nil {
			u.PrintFatal("The server refused to register "+handle, err)
		}
		u.PrintSuccess("Registered " + handle + " as: " + strings.Join(argv, " "))
	},
}

func init() {
	registerCmd.Flags().Var(&registerFlags.model, "model",
		"Model the agent runs, or custom to supply the whole command (required)")
	registerCmd.MarkFlagRequired("model")
	registerCmd.Flags().StringVar(&registerFlags.command, "command", "",
		"Whole command to run, only with --model custom, carrying {{prompt}} itself")
	registerCmd.Flags().BoolVar(&registerFlags.allowHistory, "allow-history", false,
		"Let a prompt carry recent container history as well as the message that mentioned the agent")
}
