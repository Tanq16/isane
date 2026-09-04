package daemon

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	u "github.com/tanq16/isane/agent/utils"
	"github.com/tanq16/isane/internal/agentproto"
)

const (
	minBackoff       = time.Second
	maxBackoff       = 30 * time.Second
	postTimeout      = 30 * time.Second
	interruptedError = "the daemon was interrupted"
)

type Config struct {
	ServerURL string
	Timeout   time.Duration
}

func Serve(ctx context.Context, cfg Config, store *Store) error {
	agents, err := store.List()
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return errors.New("no agents are configured, run 'isane-agent init <handle> --claim-token <token>' first")
	}

	release, err := store.Lock()
	if err != nil {
		return err
	}
	defer release()
	u.PrintInfo("polling " + cfg.ServerURL)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, func() { signal.Reset(os.Interrupt, syscall.SIGTERM) })

	var wg sync.WaitGroup
	served := 0
	for _, agent := range agents {
		if !agent.Registered() {
			u.PrintWarn("skipping "+agent.Handle+", it has no registered command", nil)
			continue
		}
		if err := store.WriteFetchHelper(agent.Handle); err != nil {
			return err
		}
		served++
		wg.Go(func() { serveAgent(ctx, cfg, store, agent) })
	}
	if served == 0 {
		return errors.New("no agent on this machine is registered, run 'isane-agent register <handle> --model <model>' first")
	}
	wg.Wait()
	return nil
}

func serveAgent(ctx context.Context, cfg Config, store *Store, agent Agent) {
	api := NewAPI(cfg.ServerURL, agent.Handle, agent.ClaimToken)
	if err := api.Hello(ctx); err != nil {
		if terminal(err) {
			u.PrintError("the server rejected "+agent.Handle, err)
			return
		}
		u.PrintWarn("could not reach the server for "+agent.Handle, err)
	} else {
		u.PrintSuccess("serving " + agent.Handle)
	}

	backoff := minBackoff
	for ctx.Err() == nil {
		jobs, err := api.Jobs(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if terminal(err) {
				u.PrintError("the server rejected "+agent.Handle, err)
				return
			}
			u.PrintWarn("poll for "+agent.Handle+" failed, retrying in "+backoff.String(), err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = minBackoff
		for _, job := range jobs {
			runJob(ctx, cfg, store, api, agent, job)
		}
	}
}

func runJob(ctx context.Context, cfg Config, store *Store, api *API, agent Agent, job agentproto.Job) {
	id := job.ID.String()
	u.PrintInfo("running job " + id + " for " + agent.Handle)

	answer, err := Run(ctx, Job{
		Agent:   agent,
		ID:      id,
		Dir:     store.Dir(agent.Handle),
		Prompt:  job.Prompt,
		Server:  cfg.ServerURL,
		Timeout: cfg.Timeout,
	})
	var res agentproto.ResultRequest
	switch {
	case ctx.Err() != nil:
		res.Error = interruptedError
	case err != nil:
		res.Error = err.Error()
	default:
		res.Result = answer
	}

	post, cancel := context.WithTimeout(context.WithoutCancel(ctx), postTimeout)
	defer cancel()
	if err := api.PostResult(post, id, res); err != nil {
		u.PrintError("could not post the answer for job "+id, err)
		return
	}
	if res.Error != "" {
		u.PrintWarn("job "+id+" failed: "+res.Error, nil)
		return
	}
	u.PrintSuccess("job " + id + " answered")
}

func terminal(err error) bool {
	statusErr, ok := errors.AsType[*StatusError](err)
	return ok && statusErr.Terminal()
}
