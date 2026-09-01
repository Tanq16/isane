package handlers

import (
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

type Messages struct {
	app *app.App
}

func NewMessages(a *app.App) *Messages {
	return &Messages{app: a}
}

type postMessageRequest struct {
	ClientID      string      `json:"client_id"`
	Body          string      `json:"body"`
	ReplyToID     *uuid.UUID  `json:"reply_to_id"`
	ThreadRootID  *uuid.UUID  `json:"thread_root_id"`
	AttachmentIDs []uuid.UUID `json:"attachment_ids"`
}

type editMessageRequest struct {
	Body string `json:"body"`
}

type subscriptionRequest struct {
	State string `json:"state"`
}

type editedFrame struct {
	ID       uuid.UUID  `json:"id"`
	Body     string     `json:"body"`
	EditedAt *time.Time `json:"edited_at,omitempty"`
}

type deletedFrame struct {
	ID          uuid.UUID `json:"id"`
	ContainerID uuid.UUID `json:"container_id"`
	Seq         int64     `json:"seq"`
}

func (h *Messages) List(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	if _, err := containerFor(r.Context(), h.app, id, u.ID); err != nil {
		WriteError(w, err)
		return
	}
	limit := queryLimit(r, defaultPageLimit, maxPageLimit)
	before, hasBefore, err := queryInt64(r, "before")
	if err != nil {
		WriteError(w, err)
		return
	}
	after, hasAfter, err := queryInt64(r, "after")
	if err != nil {
		WriteError(w, err)
		return
	}
	around, hasAround, err := queryInt64(r, "around")
	if err != nil {
		WriteError(w, err)
		return
	}
	var msgs []store.Message
	switch {
	case hasBefore:
		msgs, err = h.app.DB.TimelineBefore(r.Context(), id, before, limit)
	case hasAfter:
		msgs, err = h.app.DB.TimelineAfter(r.Context(), id, after, limit)
	case hasAround:
		msgs, err = h.app.DB.TimelineAround(r.Context(), id, around, limit)
	default:
		msgs, err = h.app.DB.TimelineLatest(r.Context(), id, limit)
	}
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(msgs))
}

func (h *Messages) Post(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	if u.Kind == store.UserAgent {
		WriteError(w, forbiddenf("an agent posts only as a job result"))
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	c, err := containerFor(r.Context(), h.app, id, u.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if c.ArchivedAt != nil {
		WriteError(w, conflictf("this channel is archived"))
		return
	}
	var req postMessageRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if req.ClientID == "" {
		WriteError(w, badRequestf("client_id is required"))
		return
	}
	if strings.TrimSpace(req.Body) == "" && len(req.AttachmentIDs) == 0 {
		WriteError(w, badRequestf("a message needs a body or an attachment"))
		return
	}
	m, err := h.app.PostMessage(r.Context(), u, socket.SendPayload{
		ContainerID:   id,
		ClientID:      req.ClientID,
		Body:          req.Body,
		ReplyToID:     req.ReplyToID,
		ThreadRootID:  req.ThreadRootID,
		AttachmentIDs: req.AttachmentIDs,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, m)
}

func (h *Messages) Edit(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	m, c, err := h.writableMessage(r, id, u)
	if err != nil {
		WriteError(w, err)
		return
	}
	if m.AuthorID != u.ID {
		WriteError(w, forbiddenf("only the author edits a message"))
		return
	}
	var req editMessageRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		WriteError(w, badRequestf("body is required"))
		return
	}
	mentions, err := h.app.ResolveMentions(r.Context(), c.ID, req.Body)
	if err != nil {
		WriteError(w, err)
		return
	}
	edited, err := h.app.DB.EditMessage(r.Context(), m.ID, req.Body, mentions)
	if err != nil {
		WriteError(w, err)
		return
	}
	h.broadcast(r, c.ID, socket.NewFrame("message_edited", editedFrame{
		ID:       edited.ID,
		Body:     edited.Body,
		EditedAt: edited.EditedAt,
	}))
	WriteJSON(w, http.StatusOK, edited)
}

func (h *Messages) Delete(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	m, c, err := h.writableMessage(r, id, u)
	if err != nil {
		WriteError(w, err)
		return
	}
	if m.AuthorID != u.ID && !u.IsAdmin {
		WriteError(w, forbiddenf("only the author or an administrator deletes a message"))
		return
	}
	deleted, err := h.app.DB.DeleteMessage(r.Context(), m.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	h.broadcast(r, c.ID, socket.NewFrame("message_deleted", deletedFrame{
		ID:          deleted.ID,
		ContainerID: deleted.ContainerID,
		Seq:         deleted.Seq,
	}))
	writeOK(w)
}

func (h *Messages) Thread(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	rootID, err := pathUUID(r, "root_id")
	if err != nil {
		WriteError(w, err)
		return
	}
	root, err := h.app.DB.GetMessage(r.Context(), rootID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if _, err := containerFor(r.Context(), h.app, root.ContainerID, u.ID); err != nil {
		WriteError(w, err)
		return
	}
	after, _, err := queryInt64(r, "after")
	if err != nil {
		WriteError(w, err)
		return
	}
	msgs, err := h.app.DB.ThreadMessages(r.Context(), rootID, after, queryLimit(r, defaultPageLimit, maxPageLimit))
	if err != nil {
		WriteError(w, err)
		return
	}
	sub, subscribed, err := h.app.DB.ThreadSubscription(r.Context(), u.ID, rootID)
	if err != nil {
		WriteError(w, err)
		return
	}
	body := threadResponse{Root: root, Messages: nonNil(msgs)}
	if subscribed {
		body.Subscription = string(sub)
	}
	WriteJSON(w, http.StatusOK, body)
}

type threadResponse struct {
	Root         store.Message   `json:"root"`
	Messages     []store.Message `json:"messages"`
	Subscription string          `json:"subscription"`
}

func (h *Messages) Subscribe(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	rootID, err := pathUUID(r, "root_id")
	if err != nil {
		WriteError(w, err)
		return
	}
	root, err := h.app.DB.GetMessage(r.Context(), rootID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if _, err := containerFor(r.Context(), h.app, root.ContainerID, u.ID); err != nil {
		WriteError(w, err)
		return
	}
	var req subscriptionRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	state := store.ThreadSubState(req.State)
	switch state {
	case store.ThreadSubscribed, store.ThreadMuted:
	default:
		WriteError(w, badRequestf("state must be one of subscribed, muted"))
		return
	}
	if err := h.app.DB.SetThreadSubscription(r.Context(), u.ID, rootID, state); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Messages) Search(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		WriteError(w, badRequestf("q is required"))
		return
	}
	containerID, err := queryUUID(r, "container_id")
	if err != nil {
		WriteError(w, err)
		return
	}
	results, err := h.app.DB.Search(r.Context(), u.ID, query, containerID, queryLimit(r, defaultPageLimit, maxPageLimit))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(results))
}

func (h *Messages) writableMessage(r *http.Request, id uuid.UUID, u store.User) (store.Message, store.Container, error) {
	m, err := h.app.DB.GetMessage(r.Context(), id)
	if err != nil {
		return store.Message{}, store.Container{}, err
	}
	if m.DeletedAt != nil {
		return store.Message{}, store.Container{}, conflictf("this message is deleted")
	}
	c, err := containerFor(r.Context(), h.app, m.ContainerID, u.ID)
	if err != nil {
		return store.Message{}, store.Container{}, err
	}
	if c.ArchivedAt != nil {
		return store.Message{}, store.Container{}, conflictf("this channel is archived")
	}
	return m, c, nil
}

func (h *Messages) broadcast(r *http.Request, containerID uuid.UUID, f socket.Frame) {
	ids, err := h.app.DB.MemberIDs(r.Context(), containerID)
	if err != nil {
		h.app.Log.Error().Err(err).Msg("resolve members for broadcast")
		return
	}
	h.app.Hub.ToUsers(ids, f)
}
