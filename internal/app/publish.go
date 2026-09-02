package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/tanq16/isane/internal/markdown"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

func (a *App) PublishMessage(ctx context.Context, m store.Message, c store.Container) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backgroundTimeout)
	defer cancel()
	a.Broadcast(ctx, c.ID, socket.NewFrame(socket.TypeMessage, m))
	author, err := a.DB.GetUser(ctx, m.AuthorID)
	if err != nil {
		a.Log.Error().Err(err).Str("message", m.ID.String()).Msg("load message author")
		return
	}
	a.background("notification routing", func(ctx context.Context) {
		a.Push.Route(ctx, m, author, c)
	})
	if author.Kind == store.UserAgent || len(m.Mentions) == 0 {
		return
	}
	a.background("agent dispatch", func(ctx context.Context) {
		a.dispatchAgents(ctx, m, c)
	})
}

func (a *App) Broadcast(ctx context.Context, containerID uuid.UUID, f socket.Frame) {
	members, err := a.DB.MemberIDs(ctx, containerID)
	if err != nil {
		a.Log.Error().Err(err).Str("container", containerID.String()).Msg("resolve members for broadcast")
		return
	}
	a.Hub.ToUsers(members, f)
}

func (a *App) PostMessage(ctx context.Context, author store.User, p socket.SendPayload) (store.Message, error) {
	m, _, err := a.postMessage(ctx, author, p)
	return m, err
}

func (a *App) postMessage(ctx context.Context, author store.User, p socket.SendPayload) (store.Message, bool, error) {
	if author.Kind == store.UserAgent {
		return store.Message{}, false, fmt.Errorf("post message: %w: an agent answers through a job result", ErrForbidden)
	}
	body := strings.TrimSpace(p.Body)
	if body == "" && len(p.AttachmentIDs) == 0 {
		return store.Message{}, false, fmt.Errorf("post message: %w: the message is empty", ErrInvalid)
	}
	c, err := a.writableContainer(ctx, p.ContainerID, author.ID)
	if err != nil {
		return store.Message{}, false, fmt.Errorf("post message: %w", err)
	}

	clientID := p.ClientID
	if clientID == "" {
		clientID = uuid.New().String()
	}
	existing, err := a.DB.MessageByClientID(ctx, author.ID, clientID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Message{}, false, fmt.Errorf("post message: %w", err)
	}

	if err := a.sameContainer(ctx, c.ID, p.ReplyToID, "reply target"); err != nil {
		return store.Message{}, false, fmt.Errorf("post message: %w", err)
	}
	if err := a.threadRoot(ctx, c.ID, p.ThreadRootID); err != nil {
		return store.Message{}, false, fmt.Errorf("post message: %w", err)
	}
	mentions, err := a.ResolveMentions(ctx, c.ID, body)
	if err != nil {
		return store.Message{}, false, fmt.Errorf("post message: %w", err)
	}
	m, err := a.DB.InsertMessage(ctx, store.NewMessage{
		ContainerID:   c.ID,
		AuthorID:      author.ID,
		Body:          body,
		ClientID:      clientID,
		ReplyToID:     p.ReplyToID,
		ThreadRootID:  p.ThreadRootID,
		AttachmentIDs: p.AttachmentIDs,
		MentionIDs:    mentions,
	})
	if err != nil {
		return store.Message{}, false, fmt.Errorf("post message: %w", err)
	}
	a.PublishMessage(ctx, m, c)
	return m, true, nil
}

func (a *App) PostSystem(ctx context.Context, containerID uuid.UUID, authorID uuid.UUID, threadRootID *uuid.UUID, body string) (store.Message, error) {
	return a.postSystem(ctx, store.NewMessage{
		ContainerID:  containerID,
		AuthorID:     authorID,
		Body:         body,
		ThreadRootID: threadRootID,
	})
}

func (a *App) PostCallNotice(ctx context.Context, containerID, authorID, callID uuid.UUID, body string) (store.Message, error) {
	return a.postSystem(ctx, store.NewMessage{
		ContainerID: containerID,
		AuthorID:    authorID,
		Body:        body,
		CallID:      &callID,
	})
}

func (a *App) PostRecordingNotice(ctx context.Context, containerID, authorID, recordingID uuid.UUID, body string) (store.Message, error) {
	return a.postSystem(ctx, store.NewMessage{
		ContainerID: containerID,
		AuthorID:    authorID,
		Body:        body,
		RecordingID: &recordingID,
	})
}

func (a *App) postSystem(ctx context.Context, n store.NewMessage) (store.Message, error) {
	c, err := a.DB.GetContainer(ctx, n.ContainerID)
	if err != nil {
		return store.Message{}, fmt.Errorf("post system message: %w", err)
	}
	n.ClientID = uuid.New().String()
	n.IsSystem = true
	m, err := a.DB.InsertMessage(ctx, n)
	if err != nil {
		return store.Message{}, fmt.Errorf("post system message: %w", err)
	}
	a.PublishMessage(ctx, m, c)
	return m, nil
}

func (a *App) EditMessage(ctx context.Context, actor store.User, messageID uuid.UUID, body string) (store.Message, error) {
	m, err := a.writableMessage(ctx, actor, messageID)
	if err != nil {
		return store.Message{}, fmt.Errorf("edit message: %w", err)
	}
	if m.AuthorID != actor.ID {
		return store.Message{}, fmt.Errorf("edit message: %w: only the author edits a message", ErrForbidden)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return store.Message{}, fmt.Errorf("edit message: %w: the message is empty", ErrInvalid)
	}
	mentions, err := a.ResolveMentions(ctx, m.ContainerID, body)
	if err != nil {
		return store.Message{}, fmt.Errorf("edit message: %w", err)
	}
	edited, err := a.DB.EditMessage(ctx, messageID, body, mentions)
	if err != nil {
		return store.Message{}, fmt.Errorf("edit message: %w", err)
	}
	a.Broadcast(ctx, edited.ContainerID, socket.NewFrame(socket.TypeMessageEdited,
		socket.MessageEditedPayload{ID: edited.ID, Body: edited.Body, EditedAt: edited.EditedAt}))
	return edited, nil
}

func (a *App) DeleteMessage(ctx context.Context, actor store.User, messageID uuid.UUID) (store.Message, error) {
	m, err := a.writableMessage(ctx, actor, messageID)
	if err != nil {
		return store.Message{}, fmt.Errorf("delete message: %w", err)
	}
	if m.AuthorID != actor.ID && !actor.IsAdmin {
		return store.Message{}, fmt.Errorf("delete message: %w: only the author or an administrator deletes a message", ErrForbidden)
	}
	deleted, err := a.DB.DeleteMessage(ctx, messageID)
	if err != nil {
		return store.Message{}, fmt.Errorf("delete message: %w", err)
	}
	a.Broadcast(ctx, deleted.ContainerID, socket.NewFrame(socket.TypeMessageDeleted,
		socket.MessageDeletedPayload{ID: deleted.ID, ContainerID: deleted.ContainerID, Seq: deleted.Seq}))
	return deleted, nil
}

func (a *App) MarkRead(ctx context.Context, userID, containerID uuid.UUID, seq int64) (int64, error) {
	member, err := a.DB.IsMember(ctx, containerID, userID)
	if err != nil {
		return 0, fmt.Errorf("mark read: %w", err)
	}
	if !member {
		return 0, fmt.Errorf("mark read: %w: not a member of this container", ErrForbidden)
	}
	effective, err := a.DB.SetReadMarker(ctx, userID, containerID, seq)
	if err != nil {
		return 0, fmt.Errorf("mark read: %w", err)
	}
	a.Hub.ToUser(userID, socket.NewFrame(socket.TypeRead,
		socket.ReadPayload{ContainerID: containerID, Seq: effective}))
	return effective, nil
}

func (a *App) ResolveMentions(ctx context.Context, containerID uuid.UUID, body string) ([]uuid.UUID, error) {
	handles, everyone := markdown.Mentions(body)
	if len(handles) == 0 && !everyone {
		return nil, nil
	}
	members, err := a.DB.MemberIDs(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("resolve mentions: %w", err)
	}
	member := make(map[uuid.UUID]bool, len(members))
	for _, id := range members {
		member[id] = true
	}
	ids := make([]uuid.UUID, 0, len(handles)+len(members))
	seen := make(map[uuid.UUID]bool, len(handles)+len(members))
	if everyone {
		for _, id := range members {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	for _, handle := range handles {
		u, err := a.DB.GetUserByHandle(ctx, handle)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resolve mentions: %w", err)
		}
		if !u.Active() || seen[u.ID] {
			continue
		}
		if u.Kind == store.UserHuman && !member[u.ID] {
			continue
		}
		seen[u.ID] = true
		ids = append(ids, u.ID)
	}
	return ids, nil
}

func (a *App) dispatchAgents(ctx context.Context, m store.Message, c store.Container) {
	mentioned, err := a.mentionedAgents(ctx, m.Mentions)
	if err != nil {
		a.Log.Error().Err(err).Str("message", m.ID.String()).Msg("resolve mentioned agents")
		return
	}
	if len(mentioned) == 0 {
		return
	}
	dispatches, err := a.Agents.DispatchFor(ctx, m, c, mentioned)
	if err != nil {
		a.Log.Error().Err(err).Str("message", m.ID.String()).Msg("dispatch agents")
		return
	}
	for _, d := range dispatches {
		switch {
		case d.Job != nil:
			a.Broadcast(ctx, c.ID, socket.NewFrame(socket.TypeAgentWorking, socket.AgentWorkingPayload{
				ContainerID: c.ID,
				AgentID:     d.Agent.User.ID,
				JobID:       d.Job.ID,
			}))
		case d.Notice != "":
			if _, err := a.PostSystem(ctx, c.ID, d.Agent.User.ID, m.ThreadRootID, d.Notice); err != nil {
				a.Log.Error().Err(err).Str("agent", d.Agent.User.Handle).Msg("post dispatch notice")
			}
		}
	}
}

func (a *App) mentionedAgents(ctx context.Context, ids []uuid.UUID) ([]store.AgentInfo, error) {
	var out []store.AgentInfo
	for _, id := range ids {
		u, err := a.DB.GetUser(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load mentioned user: %w", err)
		}
		if u.Kind != store.UserAgent {
			continue
		}
		agent, err := a.DB.GetAgent(ctx, u.ID)
		if err != nil {
			return nil, fmt.Errorf("load mentioned agent: %w", err)
		}
		out = append(out, store.AgentInfo{User: u, Agent: agent})
	}
	return out, nil
}

func (a *App) writableContainer(ctx context.Context, containerID, userID uuid.UUID) (store.Container, error) {
	c, err := a.DB.GetContainer(ctx, containerID)
	if err != nil {
		return store.Container{}, err
	}
	member, err := a.DB.IsMember(ctx, c.ID, userID)
	if err != nil {
		return store.Container{}, err
	}
	if !member {
		return store.Container{}, fmt.Errorf("%w: not a member of this container", ErrForbidden)
	}
	if c.ArchivedAt != nil {
		return store.Container{}, fmt.Errorf("%w: the channel is archived", store.ErrConflict)
	}
	return c, nil
}

func (a *App) writableMessage(ctx context.Context, actor store.User, messageID uuid.UUID) (store.Message, error) {
	m, err := a.DB.GetMessage(ctx, messageID)
	if err != nil {
		return store.Message{}, err
	}
	if m.DeletedAt != nil {
		return store.Message{}, fmt.Errorf("%w: the message is deleted", store.ErrConflict)
	}
	if _, err := a.writableContainer(ctx, m.ContainerID, actor.ID); err != nil {
		return store.Message{}, err
	}
	return m, nil
}

func (a *App) threadRoot(ctx context.Context, containerID uuid.UUID, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	root, err := a.DB.GetMessage(ctx, *id)
	if err != nil {
		return fmt.Errorf("resolve thread root: %w", err)
	}
	if root.ContainerID != containerID {
		return fmt.Errorf("%w: the thread root belongs to another container", ErrInvalid)
	}
	if root.ThreadRootID != nil {
		return fmt.Errorf("%w: the thread root is itself a thread reply", ErrInvalid)
	}
	return nil
}

func (a *App) sameContainer(ctx context.Context, containerID uuid.UUID, id *uuid.UUID, what string) error {
	if id == nil {
		return nil
	}
	m, err := a.DB.GetMessage(ctx, *id)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", what, err)
	}
	if m.ContainerID != containerID {
		return fmt.Errorf("%w: the %s belongs to another container", ErrInvalid, what)
	}
	return nil
}
