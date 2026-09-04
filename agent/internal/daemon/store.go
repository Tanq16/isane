package daemon

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

const (
	agentsDir        = "agents"
	recordFile       = "agent.json"
	instructionsFile = "AGENTS.md"
	skillsDir        = ".agents/skills"
	resultFile       = ".result"
	fetchHelperFile  = "fetch-attachment"
	lockFile         = "serve.lock"
)

const fetchHelper = `#!/bin/sh
mkdir -p ./files
curl -fsS -o "./files/$1" \
  -H "Authorization: Bearer $ISANE_CLAIM_TOKEN" \
  -H "X-Isane-Agent: $ISANE_AGENT_HANDLE" \
  "$ISANE_URL/api/agent/jobs/$ISANE_JOB_ID/attachments/$1"
echo "./files/$1"
`

var claudeLinks = map[string]string{
	"CLAUDE.md":      instructionsFile,
	".claude/skills": "../" + skillsDir,
}

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

var ErrNoAgent = errors.New("agent is not configured on this machine")

func ValidateHandle(handle string) error {
	if !handlePattern.MatchString(handle) {
		return fmt.Errorf("handle must match %s", handlePattern.String())
	}
	return nil
}

type Agent struct {
	Handle       string   `json:"handle"`
	ClaimToken   string   `json:"claim_token"`
	Argv         []string `json:"argv,omitempty"`
	AllowHistory bool     `json:"allow_history,omitempty"`
}

func (a Agent) Registered() bool { return len(a.Argv) > 0 }

type Store struct{ root string }

func NewStore(root string) *Store { return &Store{root: root} }

func (s *Store) Dir(handle string) string { return filepath.Join(s.root, agentsDir, handle) }

func (s *Store) List() ([]Agent, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, agentsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var agents []Agent
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agent, err := s.Load(entry.Name())
		if errors.Is(err, ErrNoAgent) {
			continue
		}
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	slices.SortFunc(agents, func(a, b Agent) int { return cmp.Compare(a.Handle, b.Handle) })
	return agents, nil
}

func (s *Store) Load(handle string) (Agent, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir(handle), recordFile))
	if errors.Is(err, os.ErrNotExist) {
		return Agent{}, ErrNoAgent
	}
	if err != nil {
		return Agent{}, err
	}
	var agent Agent
	if err := json.Unmarshal(data, &agent); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (s *Store) Save(agent Agent) error {
	dir := s.Dir(agent.Handle)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(agent, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, recordFile), data, 0o600)
}

func (s *Store) Scaffold(agent Agent) error {
	dir := s.Dir(agent.Handle)
	if err := os.MkdirAll(filepath.Join(dir, skillsDir), 0o700); err != nil {
		return err
	}
	if err := s.Save(agent); err != nil {
		return err
	}
	instructions := filepath.Join(dir, instructionsFile)
	if _, err := os.Stat(instructions); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(instructions, []byte("# "+agent.Handle+"\n"), 0o600); err != nil {
			return err
		}
	}
	for name, target := range claudeLinks {
		link := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			return err
		}
		if err := os.Symlink(target, link); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return nil
}

func (s *Store) WriteFetchHelper(handle string) error {
	path := filepath.Join(s.Dir(handle), fetchHelperFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(fetchHelper), 0o700)
}

func (s *Store) Lock() (func(), error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, lockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder, _ := io.ReadAll(f)
		f.Close()
		return nil, fmt.Errorf("another isane-agent serve holds %s, started by pid %s",
			path, cmp.Or(strings.TrimSpace(string(holder)), "unknown"))
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := fmt.Fprintln(f, os.Getpid()); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
