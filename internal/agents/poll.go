package agents

import (
	"context"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/tanq16/isane/internal/store"
)

const pollBatch = 10

func (s *Service) Poll(ctx context.Context, a store.AgentInfo, wait time.Duration) ([]store.AgentJob, error) {
	woken := s.listen(a.User.ID)
	defer s.forget(a.User.ID, woken)

	jobs, err := s.claim(ctx, a)
	if err != nil || len(jobs) > 0 || wait <= 0 {
		return jobs, err
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.shutdown:
		return nil, nil
	case <-timer.C:
	case <-woken:
	}
	return s.claim(ctx, a)
}

func (s *Service) claim(ctx context.Context, a store.AgentInfo) ([]store.AgentJob, error) {
	jobs, err := s.db.ClaimQueuedJobs(ctx, a.User.ID, pollBatch)
	if err != nil {
		return nil, fmt.Errorf("poll for %s: %w", a.User.Handle, err)
	}
	return jobs, nil
}

func (s *Service) Complete(ctx context.Context, a store.AgentInfo, jobID uuid.UUID, result, errText string) (store.AgentJob, error) {
	errText = strings.TrimSpace(errText)
	if errText == "" && strings.TrimSpace(result) == "" {
		errText = "agent returned an empty result"
	}
	if errText != "" {
		failed, err := s.db.FinishAgentJob(ctx, jobID, a.User.ID, store.JobFailed, nil, &errText)
		if err != nil {
			return store.AgentJob{}, fmt.Errorf("complete job %s: %w", jobID, err)
		}
		return failed, nil
	}
	done, err := s.db.FinishAgentJob(ctx, jobID, a.User.ID, store.JobDone, &result, nil)
	if err != nil {
		return store.AgentJob{}, fmt.Errorf("complete job %s: %w", jobID, err)
	}
	return done, nil
}

func (s *Service) ExpireStale(ctx context.Context) ([]store.AgentJob, error) {
	return s.db.ExpireStaleJobs(ctx, s.cfg.Agents.JobTimeout)
}

func (s *Service) listen(agentID uuid.UUID) chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiters[agentID] == nil {
		s.waiters[agentID] = make(map[chan struct{}]struct{})
	}
	s.waiters[agentID][ch] = struct{}{}
	return ch
}

func (s *Service) forget(agentID uuid.UUID, ch chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.waiters[agentID], ch)
	if len(s.waiters[agentID]) == 0 {
		delete(s.waiters, agentID)
	}
}

func (s *Service) wake(agentID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.waiters[agentID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
