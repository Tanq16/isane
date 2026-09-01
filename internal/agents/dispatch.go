package agents

import (
	"context"
	"fmt"

	"github.com/tanq16/isane/internal/store"
)

type Dispatch struct {
	Agent  store.AgentInfo
	Job    *store.AgentJob
	Notice string
}

func (s *Service) DispatchFor(ctx context.Context, m store.Message, c store.Container, mentioned []store.AgentInfo) ([]Dispatch, error) {
	out := make([]Dispatch, 0, len(mentioned))
	for _, a := range mentioned {
		if a.User.ID == m.AuthorID {
			continue
		}
		d, err := s.dispatchOne(ctx, m, c, a)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *Service) dispatchOne(ctx context.Context, m store.Message, c store.Container, a store.AgentInfo) (Dispatch, error) {
	if a.Agent.State != store.AgentServing {
		return Dispatch{Agent: a, Notice: fmt.Sprintf("agent @%s is not serving", a.User.Handle)}, nil
	}
	busy, err := s.db.HasUnfinishedJob(ctx, a.User.ID, m.ContainerID)
	if err != nil {
		return Dispatch{}, fmt.Errorf("dispatch to %s: %w", a.User.Handle, err)
	}
	if busy {
		return Dispatch{Agent: a, Notice: fmt.Sprintf("agent @%s is already working", a.User.Handle)}, nil
	}
	prompt, attachmentIDs, err := s.ComposePrompt(ctx, a, m, c)
	if err != nil {
		return Dispatch{}, fmt.Errorf("dispatch to %s: %w", a.User.Handle, err)
	}
	job, err := s.db.CreateAgentJob(ctx, store.AgentJob{
		AgentID:      a.User.ID,
		ContainerID:  m.ContainerID,
		TriggerMsgID: m.ID,
		State:        store.JobQueued,
		Prompt:       prompt,
	}, attachmentIDs)
	if err != nil {
		return Dispatch{}, fmt.Errorf("dispatch to %s: %w", a.User.Handle, err)
	}
	s.wake(a.User.ID)
	s.log.Info().Str("agent", a.User.Handle).Str("job", job.ID.String()).Msg("agent job queued")
	return Dispatch{Agent: a, Job: &job}, nil
}
