package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/tanq16/isane/internal/agentproto"
)

const (
	minBackoff       = time.Second
	maxBackoff       = 30 * time.Second
	postTimeout      = 30 * time.Second
	interruptedError = "the daemon was interrupted"
)

var (
	ErrNoneConfigured = errors.New("no agents are configured on this machine")
	ErrNoneRegistered = errors.New("no agent on this machine is registered")
)

type Config struct {
	ServerURL string
	Timeout   time.Duration
}

func Serve(ctx context.Context, cfg Config, store *Store, w io.Writer) error {
	agents, err := store.List()
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return ErrNoneConfigured
	}

	release, err := store.Lock()
	if err != nil {
		return err
	}
	defer release()
	out := &reporter{w: w}
	out.report(LevelInfo, "polling "+cfg.ServerURL, nil)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, func() { signal.Reset(os.Interrupt, syscall.SIGTERM) })

	var registered []Agent
	for _, agent := range agents {
		if !agent.Registered() {
			out.report(LevelWarn, "skipping "+agent.Handle+", it has no registered command", nil)
			continue
		}
		if err := store.WriteFetchHelper(agent.Handle); err != nil {
			return err
		}
		registered = append(registered, agent)
	}
	if len(registered) == 0 {
		return ErrNoneRegistered
	}

	var wg sync.WaitGroup
	for _, agent := range registered {
		wg.Go(func() { serveAgent(ctx, cfg, store, out, agent) })
	}
	wg.Wait()
	return nil
}

func serveAgent(ctx context.Context, cfg Config, store *Store, out *reporter, agent Agent) {
	api := NewAPI(cfg.ServerURL, agent.Handle, agent.ClaimToken)
	if err := api.Hello(ctx); err != nil {
		if terminal(err) {
			out.report(LevelError, "the server rejected "+agent.Handle, err)
			return
		}
		out.report(LevelWarn, "could not reach the server for "+agent.Handle, err)
	} else {
		out.report(LevelSuccess, "serving "+agent.Handle, nil)
	}

	backoff := minBackoff
	for ctx.Err() == nil {
		jobs, err := api.Jobs(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if terminal(err) {
				out.report(LevelError, "the server rejected "+agent.Handle, err)
				return
			}
			out.report(LevelWarn, "poll for "+agent.Handle+" failed, retrying in "+backoff.String(), err)
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
			runJob(ctx, cfg, store, out, api, agent, job)
		}
	}
}

func runJob(ctx context.Context, cfg Config, store *Store, out *reporter, api *API, agent Agent, job agentproto.Job) {
	id := job.ID.String()
	out.report(LevelInfo, "running job "+id+" for "+agent.Handle, nil)

	answer, err := Run(ctx, Job{
		Agent:   agent,
		ID:      id,
		Dir:     store.Dir(agent.Handle),
		Prompt:  job.Prompt,
		Server:  cfg.ServerURL,
		Timeout: cfg.Timeout,
	})
	var res agentproto.ResultRequest
	var jobErr error
	switch {
	case ctx.Err() != nil:
		jobErr = ctx.Err()
		res.Error = interruptedError
	case err != nil:
		jobErr = err
		res.Error = err.Error()
	default:
		res.Result = answer
	}

	post, cancel := context.WithTimeout(context.WithoutCancel(ctx), postTimeout)
	defer cancel()
	if err := api.PostResult(post, id, res); err != nil {
		out.report(LevelError, "could not post the answer for job "+id, err)
		return
	}
	if jobErr != nil {
		out.report(LevelWarn, "job "+id+" failed", jobErr)
		return
	}
	out.report(LevelSuccess, "job "+id+" answered", nil)
}

func terminal(err error) bool {
	statusErr, ok := errors.AsType[*StatusError](err)
	return ok && statusErr.Terminal()
}
