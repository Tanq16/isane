package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

type Containers struct {
	app *app.App
}

func NewContainers(a *app.App) *Containers {
	return &Containers{app: a}
}

type readRequest struct {
	Seq int64 `json:"seq"`
}

type notificationPrefRequest struct {
	Level string `json:"level"`
}

type conversationRequest struct {
	UserIDs []uuid.UUID `json:"user_ids"`
}

type readFrame struct {
	ContainerID uuid.UUID `json:"container_id"`
	Seq         int64     `json:"seq"`
}

func (h *Containers) List(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	views, err := h.app.DB.ContainerViews(r.Context(), u.ID, queryBool(r, "include_archived"))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(views))
}

func (h *Containers) SetRead(w http.ResponseWriter, r *http.Request) {
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
	var req readRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	seq, err := h.app.DB.SetReadMarker(r.Context(), u.ID, id, req.Seq)
	if err != nil {
		WriteError(w, err)
		return
	}
	h.app.Hub.ToUser(u.ID, socket.NewFrame("read", readFrame{ContainerID: id, Seq: seq}))
	WriteJSON(w, http.StatusOK, readFrame{ContainerID: id, Seq: seq})
}

func (h *Containers) SetNotificationPref(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
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
	if c.Kind != store.ContainerChannel {
		WriteError(w, badRequestf("a notification level applies to channels only"))
		return
	}
	var req notificationPrefRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	level := store.NotificationLevel(req.Level)
	switch level {
	case store.LevelAll, store.LevelMentions, store.LevelNone:
	default:
		WriteError(w, badRequestf("level must be one of all, mentions, none"))
		return
	}
	if err := h.app.DB.SetNotificationPref(r.Context(), u.ID, id, level); err != nil {
		WriteError(w, err)
		return
	}
	view, err := h.app.DB.ContainerView(r.Context(), u.ID, id)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (h *Containers) CreateConversation(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req conversationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	seen := map[uuid.UUID]bool{u.ID: true}
	participants := []uuid.UUID{u.ID}
	for _, id := range req.UserIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		p, err := h.app.DB.GetUser(r.Context(), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		if p.Kind != store.UserHuman {
			WriteError(w, badRequestf("agent @%s cannot join a conversation", p.Handle))
			return
		}
		if !p.Active() {
			WriteError(w, badRequestf("@%s is deactivated", p.Handle))
			return
		}
		participants = append(participants, id)
	}
	if len(participants) < 2 {
		WriteError(w, badRequestf("user_ids must name at least one other user"))
		return
	}
	c, err := h.app.DB.GetOrCreateConversation(r.Context(), participants, u.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	view, err := h.app.DB.ContainerView(r.Context(), u.ID, c.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (h *Containers) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.app.DB.ListUsers(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(users))
}

func containerFor(ctx context.Context, a *app.App, containerID, userID uuid.UUID) (store.Container, error) {
	c, err := a.DB.GetContainer(ctx, containerID)
	if err != nil {
		return store.Container{}, err
	}
	member, err := a.DB.IsMember(ctx, containerID, userID)
	if err != nil {
		return store.Container{}, fmt.Errorf("check membership: %w", err)
	}
	if !member {
		return store.Container{}, forbiddenf("not a member of this container")
	}
	return c, nil
}
