package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const killDelay = 5 * time.Second

type Job struct {
	Agent   Agent
	ID      string
	Dir     string
	Prompt  string
	Server  string
	Timeout time.Duration
}

func Run(ctx context.Context, j Job) (string, error) {
	if !j.Agent.Registered() {
		return "", fmt.Errorf("%s is initialized but not registered", j.Agent.Handle)
	}
	dir, err := filepath.Abs(j.Dir)
	if err != nil {
		return "", err
	}
	result := filepath.Join(dir, resultFile)
	if err := os.Remove(result); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()

	argv := make([]string, len(j.Agent.Argv))
	for i, arg := range j.Agent.Argv {
		arg = strings.ReplaceAll(arg, promptPlaceholder, composePrompt(dir, j.Prompt))
		argv[i] = strings.ReplaceAll(arg, dirPlaceholder, dir)
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"ISANE_URL="+j.Server,
		"ISANE_AGENT_HANDLE="+j.Agent.Handle,
		"ISANE_CLAIM_TOKEN="+j.Agent.ClaimToken,
		"ISANE_JOB_ID="+j.ID,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// codex exec appends a non-TTY stdin to the prompt, so cmd.Stdin stays nil for /dev/null.

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out after %s", j.Timeout)
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("%s: %w", detail, err)
		}
		return "", err
	}

	written, err := os.ReadFile(result)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	answer := strings.TrimSpace(string(written))
	if answer == "" {
		return "", fmt.Errorf("%s wrote no answer to %s", argv[0], resultFile)
	}
	if err := os.Remove(result); err != nil {
		return "", err
	}
	return answer, nil
}
