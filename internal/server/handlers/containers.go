package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"uuid"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const maxTopicLength = 256

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

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

type channelRequest struct {
	Slug  string  `json:"slug"`
	Name  string  `json:"name"`
	Topic *string `json:"topic"`
}

type channelUpdate struct {
	Slug  *string `json:"slug"`
	Name  string  `json:"name"`
	Topic *string `json:"topic"`
}

func (h *Containers) CreateChannel(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	if err := h.requireChannelManagement(r.Context(), u, "creates"); err != nil {
		WriteError(w, err)
		return
	}
	var req channelRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugPattern.MatchString(slug) {
		WriteError(w, badRequestf("slug must match ^[a-z0-9][a-z0-9-]{0,63}$"))
		return
	}
	name, err := boundedField("name", req.Name, maxNameLength)
	if err != nil {
		WriteError(w, err)
		return
	}
	topic, err := boundedTopic(req.Topic)
	if err != nil {
		WriteError(w, err)
		return
	}
	c, err := h.app.DB.CreateChannel(r.Context(), slug, name, topic, u.ID)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			WriteError(w, conflictf("a channel with the slug %s already exists", slug))
			return
		}
		WriteError(w, err)
		return
	}
	view, err := h.app.DB.ContainerView(r.Context(), u.ID, c.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	h.app.Hub.ToAll(socket.NewFrame(socket.TypeContainer, c))
	WriteJSON(w, http.StatusCreated, view)
}

func (h *Containers) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	if err := h.requireChannelManagement(r.Context(), u, "edits"); err != nil {
		WriteError(w, err)
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	var req channelUpdate
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if req.Slug != nil {
		WriteError(w, badRequestf("slug cannot be changed"))
		return
	}
	c, err := h.app.DB.GetContainer(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if c.Kind != store.ContainerChannel {
		WriteError(w, badRequestf("only a channel carries a name and a topic"))
		return
	}
	name, err := boundedField("name", req.Name, maxNameLength)
	if err != nil {
		WriteError(w, err)
		return
	}
	topic := c.Topic
	if req.Topic != nil {
		if topic, err = boundedTopic(req.Topic); err != nil {
			WriteError(w, err)
			return
		}
	}
	if err := h.app.DB.UpdateChannel(r.Context(), id, name, topic); err != nil {
		WriteError(w, err)
		return
	}
	updated, err := h.app.DB.GetContainer(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	h.app.Hub.ToAll(socket.NewFrame(socket.TypeContainer, updated))
	WriteJSON(w, http.StatusOK, updated)
}

func (h *Containers) Get(w http.ResponseWriter, r *http.Request) {
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
	view, err := h.app.DB.ContainerView(r.Context(), u.ID, id)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (h *Containers) requireChannelManagement(ctx context.Context, u store.User, verb string) error {
	may, err := h.mayManageChannels(ctx, u)
	if err != nil {
		return err
	}
	if !may {
		return forbiddenf("only an administrator %s a channel", verb)
	}
	return nil
}

func (h *Containers) mayManageChannels(ctx context.Context, u store.User) (bool, error) {
	if u.IsAdmin {
		return true, nil
	}
	s, err := h.app.DB.ServerSettings(ctx)
	if err != nil {
		return false, err
	}
	return s.AllowMemberChannels, nil
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
	var req readRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	seq, err := h.app.MarkRead(r.Context(), u.ID, id, req.Seq)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, socket.ReadPayload{ContainerID: id, Seq: seq})
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
	h.app.Hub.ToUsers(participants, socket.NewFrame(socket.TypeContainer, c))
	WriteJSON(w, http.StatusOK, view)
}

func (h *Containers) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.app.DB.ListDirectory(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(users))
}

func boundedTopic(raw *string) (*string, error) {
	topic := trimTopic(raw)
	if topic != nil && len(*topic) > maxTopicLength {
		return nil, badRequestf("topic must be at most %d characters", maxTopicLength)
	}
	return topic, nil
}

func trimTopic(topic *string) *string {
	if topic == nil {
		return nil
	}
	return optionalString(strings.TrimSpace(*topic))
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
