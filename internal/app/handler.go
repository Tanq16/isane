package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const inlineGapLimit = 100

type ReadyPayload struct {
	User         store.User            `json:"user"`
	Containers   []store.ContainerView `json:"containers"`
	Users        []store.DirectoryUser `json:"users"`
	Presence     []uuid.UUID           `json:"presence"`
	Calls        []store.Call          `json:"calls"`
	Settings     store.ServerSettings  `json:"settings"`
	Gaps         []gap                 `json:"gaps"`
	MediaQuality mediaQuality          `json:"media_quality"`
}

type gap struct {
	ContainerID uuid.UUID `json:"container_id"`
	From        int64     `json:"from"`
	To          int64     `json:"to"`
}

type mediaQuality struct {
	Video       config.VideoQuality  `json:"video"`
	ScreenShare config.ScreenQuality `json:"screen_share"`
	Audio       config.AudioQuality  `json:"audio"`
	Codec       string               `json:"codec"`
}

func (a *App) Ready(ctx context.Context, u store.User, cursors map[uuid.UUID]int64) (ReadyPayload, []store.Message, error) {
	views, err := a.DB.ContainerViews(ctx, u.ID, true)
	if err != nil {
		return ReadyPayload{}, nil, fmt.Errorf("build ready payload: %w", err)
	}
	directory, err := a.DB.ListDirectory(ctx)
	if err != nil {
		return ReadyPayload{}, nil, fmt.Errorf("build ready payload: %w", err)
	}
	calls, err := a.DB.ListLiveCallsFor(ctx, u.ID)
	if err != nil {
		return ReadyPayload{}, nil, fmt.Errorf("build ready payload: %w", err)
	}
	settings, err := a.DB.ServerSettings(ctx)
	if err != nil {
		return ReadyPayload{}, nil, fmt.Errorf("build ready payload: %w", err)
	}
	gaps := make([]gap, 0)
	var inline []store.Message
	for _, v := range views {
		cursor, held := cursors[v.ID]
		if !held || v.LastSeq <= cursor {
			continue
		}
		if v.LastSeq-cursor > inlineGapLimit {
			gaps = append(gaps, gap{ContainerID: v.ID, From: cursor + 1, To: v.LastSeq})
			continue
		}
		msgs, err := a.DB.TimelineAfter(ctx, v.ID, cursor, int(v.LastSeq-cursor))
		if err != nil {
			return ReadyPayload{}, nil, fmt.Errorf("build ready payload: %w", err)
		}
		inline = append(inline, msgs...)
	}
	ready := ReadyPayload{
		User:       u,
		Containers: views,
		Users:      directory,
		Presence:   a.Hub.OnlineUsers(),
		Calls:      calls,
		Settings:   settings,
		Gaps:       gaps,
		MediaQuality: mediaQuality{
			Video:       a.Cfg().MediaQuality.Video,
			ScreenShare: a.Cfg().MediaQuality.ScreenShare,
			Audio:       a.Cfg().MediaQuality.Audio,
			Codec:       a.Cfg().MediaQuality.Codec,
		},
	}
	return ready, inline, nil
}

func (a *App) Hello(ctx context.Context, c *socket.Conn, p socket.HelloPayload) error {
	ready, inline, err := a.Ready(ctx, c.User(), p.Cursors)
	if err != nil {
		a.fail(c, "", err)
		return err
	}
	c.Send(socket.NewFrame(socket.TypeReady, ready))
	for _, m := range inline {
		c.Send(socket.NewFrame(socket.TypeMessage, m))
	}
	return nil
}

func (a *App) Send(ctx context.Context, c *socket.Conn, p socket.SendPayload) error {
	m, created, err := a.postMessage(ctx, c.User(), p)
	if err != nil {
		a.fail(c, p.ClientID, err)
		return err
	}
	if !created {
		c.Send(socket.NewFrame(socket.TypeMessage, m))
	}
	return nil
}

func (a *App) Edit(ctx context.Context, c *socket.Conn, p socket.EditPayload) error {
	if _, err := a.EditMessage(ctx, c.User(), p.MessageID, p.Body); err != nil {
		a.fail(c, "", err)
		return err
	}
	return nil
}

func (a *App) Delete(ctx context.Context, c *socket.Conn, p socket.DeletePayload) error {
	if _, err := a.DeleteMessage(ctx, c.User(), p.MessageID); err != nil {
		a.fail(c, "", err)
		return err
	}
	return nil
}

func (a *App) Read(ctx context.Context, c *socket.Conn, p socket.ReadPayload) error {
	if _, err := a.MarkRead(ctx, c.UserID(), p.ContainerID, p.Seq); err != nil {
		a.fail(c, "", err)
		return err
	}
	return nil
}

func (a *App) Typing(ctx context.Context, c *socket.Conn, p socket.TypingPayload) error {
	members, err := a.DB.MemberIDs(ctx, p.ContainerID)
	if err != nil {
		return fmt.Errorf("typing: %w", err)
	}
	if !slices.Contains(members, c.UserID()) {
		return fmt.Errorf("typing: %w: not a member of this container", ErrForbidden)
	}
	a.Hub.ToUsersExcept(members, c, socket.NewFrame(socket.TypeTyping,
		socket.TypingEventPayload{
			ContainerID:  p.ContainerID,
			UserID:       c.UserID(),
			ThreadRootID: p.ThreadRootID,
		}))
	return nil
}

func (a *App) ThreadSub(ctx context.Context, c *socket.Conn, p socket.ThreadSubPayload) error {
	if p.State != store.ThreadSubscribed && p.State != store.ThreadMuted {
		err := fmt.Errorf("thread subscription: %w: unknown state %q", ErrInvalid, p.State)
		a.fail(c, "", err)
		return err
	}
	if err := a.DB.SetThreadSubscription(ctx, c.UserID(), p.ThreadRootID, p.State); err != nil {
		err = fmt.Errorf("thread subscription: %w", err)
		a.fail(c, "", err)
		return err
	}
	return nil
}

func (a *App) fail(c *socket.Conn, clientID string, err error) {
	code := errorCode(err)
	message := err.Error()
	if code == socket.CodeInternal {
		message = "internal error"
		a.Log.Error().Err(err).Str("user", c.User().Handle).Msg("socket request failed")
	}
	c.Send(socket.NewFrame(socket.TypeError,
		socket.ErrorPayload{Code: code, Message: message, ClientID: clientID}))
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return socket.CodeNotFound
	case errors.Is(err, store.ErrConflict):
		return socket.CodeConflict
	case errors.Is(err, ErrForbidden):
		return socket.CodeForbidden
	case errors.Is(err, ErrInvalid):
		return socket.CodeInvalid
	default:
		return socket.CodeInternal
	}
}
