package daemon

import (
	"fmt"
	"slices"
	"strings"
)

const (
	promptPlaceholder = "{{prompt}}"
	dirPlaceholder    = "{{dir}}"
	CustomModel       = "custom"
)

var ClaudeAliases = []string{
	"default", "best", "fable", "sonnet", "opus", "haiku", "sonnet[1m]", "opus[1m]", "opusplan",
}

var geminiEfforts = []string{"-low", "-medium", "-high"}

const defaultGeminiEffort = "-medium"

type harness struct {
	prefix  string
	aliases []string
	argv    func(model string) []string
}

var harnesses = []harness{
	{
		prefix: "gemini-",
		argv: func(model string) []string {
			return []string{"agy", "-p", promptPlaceholder, "--model", geminiModel(model),
				"--dangerously-skip-permissions", "--add-dir", dirPlaceholder}
		},
	},
	{
		prefix:  "claude-",
		aliases: ClaudeAliases,
		argv: func(model string) []string {
			return []string{"claude", "-p", promptPlaceholder, "--model", model,
				"--dangerously-skip-permissions"}
		},
	},
	{
		prefix: "gpt-",
		argv: func(model string) []string {
			return []string{"codex", "exec", "--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox", "--model", model, "--", promptPlaceholder}
		},
	},
}

func KnownModel(model string) bool {
	if model == CustomModel {
		return true
	}
	_, ok := lookup(model)
	return ok
}

func Argv(model, command string) ([]string, error) {
	if model == CustomModel {
		return splitCommand(command)
	}
	h, ok := lookup(model)
	if !ok {
		return nil, fmt.Errorf("model %q is not one of claude-*, gemini-*, gpt-*, %s, or %s",
			model, strings.Join(ClaudeAliases, ", "), CustomModel)
	}
	return h.argv(model), nil
}

func lookup(model string) (harness, bool) {
	for _, h := range harnesses {
		if strings.HasPrefix(model, h.prefix) || slices.Contains(h.aliases, model) {
			return h, true
		}
	}
	return harness{}, false
}

func geminiModel(model string) string {
	for _, effort := range geminiEfforts {
		if strings.HasSuffix(model, effort) {
			return model
		}
	}
	return model + defaultGeminiEffort
}

func splitCommand(command string) ([]string, error) {
	var argv []string
	var current strings.Builder
	var quote rune
	started := false
	escaped := false

	for _, r := range command {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case quote == 0 && r == '\\':
			escaped = true
			started = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if started {
				argv = append(argv, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in the command", quote)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash in the command")
	}
	if started {
		argv = append(argv, current.String())
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("the command is empty")
	}
	return argv, nil
}
