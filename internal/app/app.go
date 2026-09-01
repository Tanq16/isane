package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/tanq16/isane/internal/agents"
	"github.com/tanq16/isane/internal/calls"
	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/media"
	"github.com/tanq16/isane/internal/push"
	"github.com/tanq16/isane/internal/retention"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

var (
	ErrForbidden = errors.New("forbidden")
	ErrInvalid   = errors.New("invalid request")
)

const (
	agentTimeoutInterval = 30 * time.Second
	callReapInterval     = time.Minute
	expiryInterval       = time.Hour
	retentionInterval    = 24 * time.Hour
	backgroundTimeout    = time.Minute
)

type App struct {
	Cfg       config.Config
	DB        *store.DB
	Hub       *socket.Hub
	Push      *push.Router
	Calls     *calls.Service
	Media     *media.Service
	Agents    *agents.Service
	Retention *retention.Sweeper
	Log       zerolog.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
}

func New(ctx context.Context, cfg config.Config, db *store.DB, log zerolog.Logger) (*App, error) {
	mediaSvc, err := media.New(cfg, db, log)
	if err != nil {
		return nil, fmt.Errorf("start media service: %w", err)
	}
	hub := socket.NewHub(log)
	base, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a := &App{
		Cfg:       cfg,
		DB:        db,
		Hub:       hub,
		Push:      push.New(cfg, db, hub, log),
		Calls:     calls.New(cfg, log),
		Media:     mediaSvc,
		Agents:    agents.New(cfg, db, log),
		Retention: retention.New(cfg, db, mediaSvc, log),
		Log:       log,
		ctx:       base,
		cancel:    cancel,
	}
	hub.SetHandler(a)
	hub.OnPresenceChange(a.presenceChanged)
	return a, nil
}

func (a *App) Start(ctx context.Context) {
	context.AfterFunc(ctx, a.cancel)
	a.every(agentTimeoutInterval, "agent timeouts", a.expireAgentJobs)
	a.every(callReapInterval, "call reaper", a.reapCalls)
	a.every(retentionInterval, "retention", a.runRetention)
	a.every(expiryInterval, "expiry", a.deleteExpired)
}

func (a *App) Close() {
	a.mu.Lock()
	a.closing = true
	a.mu.Unlock()
	a.cancel()
	a.Hub.Close()
	a.wg.Wait()
}

func (a *App) spawn(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return
	}
	a.wg.Go(fn)
}

func (a *App) every(d time.Duration, name string, fn func(context.Context)) {
	a.spawn(func() {
		ticker := time.NewTicker(d)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.guard(name, func() { fn(a.ctx) })
			}
		}
	})
}

func (a *App) background(name string, fn func(context.Context)) {
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(a.ctx, backgroundTimeout)
		defer cancel()
		a.guard(name, func() { fn(ctx) })
	})
}

func (a *App) guard(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			a.Log.Error().Str("job", name).Any("panic", r).Msg("background job panicked")
		}
	}()
	fn()
}

func (a *App) presenceChanged(userID uuid.UUID, online bool) {
	a.Hub.ToUsers(a.Hub.OnlineUsers(), socket.NewFrame(socket.TypePresence,
		socket.PresencePayload{UserID: userID, Online: online}))
}

func (a *App) expireAgentJobs(ctx context.Context) {
	jobs, err := a.Agents.ExpireStale(ctx)
	if err != nil {
		a.Log.Error().Err(err).Msg("expire agent jobs")
		return
	}
	for _, job := range jobs {
		agent, err := a.DB.GetUser(ctx, job.AgentID)
		if err != nil {
			a.Log.Error().Err(err).Str("job", job.ID.String()).Msg("load timed out agent")
			continue
		}
		var root *uuid.UUID
		if trigger, err := a.DB.GetMessage(ctx, job.TriggerMsgID); err == nil {
			root = trigger.ThreadRootID
		}
		body := fmt.Sprintf("agent @%s timed out after %s", agent.Handle, jobElapsed(job))
		if _, err := a.PostSystem(ctx, job.ContainerID, agent.ID, root, body); err != nil {
			a.Log.Error().Err(err).Str("job", job.ID.String()).Msg("post agent timeout")
		}
		a.broadcast(ctx, job.ContainerID, socket.NewFrame(socket.TypeAgentDone,
			socket.AgentDonePayload{JobID: job.ID}))
	}
}

func jobElapsed(job store.AgentJob) time.Duration {
	start := job.CreatedAt
	if job.DispatchedAt != nil {
		start = *job.DispatchedAt
	}
	end := time.Now()
	if job.FinishedAt != nil {
		end = *job.FinishedAt
	}
	return end.Sub(start).Round(time.Second)
}

func (a *App) reapCalls(ctx context.Context) {
	if !a.Calls.Enabled() {
		return
	}
	live, err := a.DB.ListLiveCalls(ctx)
	if err != nil {
		a.Log.Error().Err(err).Msg("list live calls")
		return
	}
	for _, call := range live {
		if len(call.Participants) > 0 {
			continue
		}
		exists, err := a.Calls.RoomExists(ctx, call.RoomName)
		if err != nil {
			a.Log.Error().Err(err).Str("room", call.RoomName).Msg("check livekit room")
			continue
		}
		if exists {
			continue
		}
		if err := a.DB.EndCall(ctx, call.ID); err != nil {
			a.Log.Error().Err(err).Str("call", call.ID.String()).Msg("close abandoned call")
			continue
		}
		a.Log.Info().Str("call", call.ID.String()).Msg("closed abandoned call")
		a.broadcast(ctx, call.ContainerID, socket.NewFrame(socket.TypeCallEnded,
			socket.CallEndedPayload{CallID: call.ID}))
	}
}

func (a *App) runRetention(ctx context.Context) {
	if err := a.Retention.Run(ctx); err != nil {
		a.Log.Error().Err(err).Msg("retention sweep")
	}
}

func (a *App) deleteExpired(ctx context.Context) {
	sessions, err := a.DB.DeleteExpiredSessions(ctx)
	if err != nil {
		a.Log.Error().Err(err).Msg("delete expired sessions")
	}
	invites, err := a.DB.DeleteExpiredInvites(ctx)
	if err != nil {
		a.Log.Error().Err(err).Msg("delete expired invites")
	}
	a.Log.Debug().Int64("sessions", sessions).Int64("invites", invites).Msg("expired rows deleted")
}
