package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const jobPollWait = 30 * time.Second

type Agents struct{ app *app.App }

func NewAgents(a *app.App) *Agents { return &Agents{app: a} }

type registerBody struct {
	Handle       string   `json:"handle"`
	ClaimToken   string   `json:"claim_token"`
	Argv         []string `json:"argv"`
	AllowHistory bool     `json:"allow_history"`
}

type resultBody struct {
	Result string `json:"result"`
	Error  string `json:"error"`
}

type jobList struct {
	Jobs []store.AgentJob `json:"jobs"`
}

func (h *Agents) Register(w http.ResponseWriter, r *http.Request) {
	var body registerBody
	if err := ReadJSON(r, &body); err != nil {
		WriteError(w, err)
		return
	}
	a, err := h.claim(r, body.Handle, body.ClaimToken)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := h.app.Agents.Register(r.Context(), a, body.Argv, body.AllowHistory); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Agents) Deregister(w http.ResponseWriter, r *http.Request) {
	var body registerBody
	if err := ReadJSON(r, &body); err != nil {
		WriteError(w, err)
		return
	}
	a, err := h.claim(r, body.Handle, body.ClaimToken)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := h.app.Agents.Deregister(r.Context(), a); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Agents) Hello(w http.ResponseWriter, r *http.Request) {
	a, ok := requestAgent(w, r)
	if !ok {
		return
	}
	if err := h.app.DB.TouchAgent(r.Context(), a.User.ID); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Agents) Jobs(w http.ResponseWriter, r *http.Request) {
	a, ok := requestAgent(w, r)
	if !ok {
		return
	}
	if err := h.app.DB.TouchAgent(r.Context(), a.User.ID); err != nil {
		h.app.Log.Error().Err(err).Str("agent", a.User.Handle).Msg("touch polling agent")
	}
	jobs, err := h.app.Agents.Poll(r.Context(), a, jobPollWait)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		WriteError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	WriteJSON(w, http.StatusOK, jobList{Jobs: nonNil(jobs)})
	if err := http.NewResponseController(w).Flush(); err != nil {
		h.app.Log.Debug().Err(err).Msg("flush agent job response")
	}
}

func (h *Agents) Result(w http.ResponseWriter, r *http.Request) {
	a, ok := requestAgent(w, r)
	if !ok {
		return
	}
	jobID, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	var body resultBody
	if err := ReadJSON(r, &body); err != nil {
		WriteError(w, err)
		return
	}
	job, err := h.app.Agents.Complete(r.Context(), a, jobID, body.Result, body.Error)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := h.answer(r.Context(), a, job); err != nil {
		h.app.Log.Error().Err(err).Str("job", job.ID.String()).Msg("post agent answer")
	}
	broadcastTo(r.Context(), h.app, job.ContainerID,
		socket.NewFrame(socket.TypeAgentDone, socket.AgentDonePayload{JobID: job.ID}))
	WriteJSON(w, http.StatusOK, job)
}

func (h *Agents) Attachment(w http.ResponseWriter, r *http.Request) {
	a, ok := requestAgent(w, r)
	if !ok {
		return
	}
	jobID, err := pathUUID(r, "job_id")
	if err != nil {
		WriteError(w, err)
		return
	}
	attachmentID, err := pathUUID(r, "attachment_id")
	if err != nil {
		WriteError(w, err)
		return
	}
	job, err := h.app.DB.GetAgentJob(r.Context(), jobID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if job.AgentID != a.User.ID {
		WriteError(w, fmt.Errorf("%w: job %s", store.ErrNotFound, jobID))
		return
	}
	if job.State != store.JobDispatched {
		WriteError(w, conflictf("job %s is %s", jobID, job.State))
		return
	}
	allowed, err := h.app.DB.JobAllowsAttachment(r.Context(), jobID, attachmentID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if !allowed {
		WriteError(w, fmt.Errorf("%w: attachment %s was not shown to job %s", store.ErrNotFound, attachmentID, jobID))
		return
	}
	at, err := h.app.DB.GetAttachment(r.Context(), attachmentID)
	if err != nil {
		WriteError(w, err)
		return
	}
	f, err := h.app.Media.Open(at)
	if err != nil {
		WriteError(w, fmt.Errorf("open attachment %s: %w", at.ID, err))
		return
	}
	serveFile(w, r, f, at.OriginalName, at.Mime, true)
}

func (h *Agents) answer(ctx context.Context, a store.AgentInfo, job store.AgentJob) error {
	c, err := h.app.DB.GetContainer(ctx, job.ContainerID)
	if err != nil {
		return err
	}
	trigger, err := h.app.DB.GetMessage(ctx, job.TriggerMsgID)
	if err != nil {
		return err
	}
	if job.State != store.JobDone || job.Result == nil {
		reason := "the agent returned no result"
		if job.Error != nil && *job.Error != "" {
			reason = *job.Error
		}
		_, err := h.app.PostSystem(ctx, job.ContainerID, a.User.ID, trigger.ThreadRootID,
			fmt.Sprintf("agent @%s failed: %s", a.User.Handle, reason))
		return err
	}
	out := store.NewMessage{
		ContainerID:  job.ContainerID,
		AuthorID:     a.User.ID,
		Body:         *job.Result,
		ClientID:     job.ID.String(),
		ThreadRootID: trigger.ThreadRootID,
	}
	if out.ThreadRootID == nil {
		out.ReplyToID = &trigger.ID
	}
	out.MentionIDs, err = h.app.ResolveMentions(ctx, job.ContainerID, out.Body)
	if err != nil {
		return err
	}
	m, err := h.app.DB.InsertMessage(ctx, out)
	if err != nil {
		return err
	}
	h.app.PublishMessage(ctx, m, c)
	return nil
}

func (h *Agents) claim(r *http.Request, handle, claimToken string) (store.AgentInfo, error) {
	if a, ok := AgentFrom(r.Context()); ok && (handle == "" || a.User.Handle == handle) {
		return a, nil
	}
	a, err := h.app.Agents.Authenticate(r.Context(), handle, claimToken)
	if err != nil {
		return store.AgentInfo{}, fmt.Errorf("%w: unknown handle or claim token", ErrUnauthorized)
	}
	return a, nil
}

func requestAgent(w http.ResponseWriter, r *http.Request) (store.AgentInfo, bool) {
	a, ok := AgentFrom(r.Context())
	if !ok {
		WriteError(w, fmt.Errorf("%w: a claim token is required", ErrUnauthorized))
	}
	return a, ok
}
