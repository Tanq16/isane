package socket

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/tanq16/isane/internal/store"
)

type Handler interface {
	Hello(ctx context.Context, c *Conn, p HelloPayload) error
	Send(ctx context.Context, c *Conn, p SendPayload) error
	Edit(ctx context.Context, c *Conn, p EditPayload) error
	Delete(ctx context.Context, c *Conn, p DeletePayload) error
	Read(ctx context.Context, c *Conn, p ReadPayload) error
	Typing(ctx context.Context, c *Conn, p TypingPayload) error
	ThreadSub(ctx context.Context, c *Conn, p ThreadSubPayload) error
}

type Hub struct {
	log      zerolog.Logger
	upgrader websocket.Upgrader
	ctx      context.Context
	cancel   context.CancelFunc

	mu       sync.RWMutex
	conns    map[uuid.UUID]map[*Conn]struct{}
	handler  Handler
	presence []func(userID uuid.UUID, online bool)
}

func NewHub(log zerolog.Logger) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		log:      log,
		upgrader: websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096},
		ctx:      ctx,
		cancel:   cancel,
		conns:    make(map[uuid.UUID]map[*Conn]struct{}),
	}
}

func (h *Hub) SetHandler(handler Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handler = handler
}

func (h *Hub) OnPresenceChange(fn func(userID uuid.UUID, online bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.presence = append(h.presence, fn)
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, u store.User) {
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Debug().Err(err).Str("user", u.Handle).Msg("websocket upgrade failed")
		return
	}
	ctx, cancel := context.WithCancel(h.ctx)
	c := &Conn{
		hub:    h,
		ws:     ws,
		user:   u,
		out:    make(chan Frame, sendBuffer),
		done:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	c.touch()
	h.add(c)
	go c.writePump()
	c.readPump()
	h.remove(c)
}

func (h *Hub) ToUser(userID uuid.UUID, f Frame) {
	h.ToUsersExcept([]uuid.UUID{userID}, nil, f)
}

func (h *Hub) ToUsers(userIDs []uuid.UUID, f Frame) {
	h.ToUsersExcept(userIDs, nil, f)
}

func (h *Hub) ToUsersExcept(userIDs []uuid.UUID, except *Conn, f Frame) {
	h.mu.RLock()
	targets := make([]*Conn, 0, len(userIDs))
	for _, id := range userIDs {
		for c := range h.conns[id] {
			if c != except {
				targets = append(targets, c)
			}
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.Send(f)
	}
}

func (h *Hub) Online(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[userID]) > 0
}

func (h *Hub) OnlineUsers() []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]uuid.UUID, 0, len(h.conns))
	for id, set := range h.conns {
		if len(set) > 0 {
			out = append(out, id)
		}
	}
	return out
}

func (h *Hub) EndpointLive(userID uuid.UUID, endpoint string, within time.Duration) bool {
	if endpoint == "" {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.conns[userID] {
		if c.PushEndpoint() == endpoint && c.seenWithin(within) {
			return true
		}
	}
	return false
}

func (h *Hub) Close() {
	h.cancel()
	h.mu.Lock()
	var open []*Conn
	for _, set := range h.conns {
		for c := range set {
			open = append(open, c)
		}
	}
	h.conns = make(map[uuid.UUID]map[*Conn]struct{})
	h.mu.Unlock()
	for _, c := range open {
		c.close()
	}
}

func (h *Hub) add(c *Conn) {
	h.mu.Lock()
	set := h.conns[c.user.ID]
	if set == nil {
		set = make(map[*Conn]struct{})
		h.conns[c.user.ID] = set
	}
	set[c] = struct{}{}
	held := len(set)
	h.mu.Unlock()
	h.log.Debug().Str("user", c.user.Handle).Int("sockets", held).Msg("socket connected")
	if held == 1 {
		h.firePresence(c.user.ID, true)
	}
}

func (h *Hub) remove(c *Conn) {
	h.mu.Lock()
	set := h.conns[c.user.ID]
	_, held := set[c]
	delete(set, c)
	if len(set) == 0 {
		delete(h.conns, c.user.ID)
	}
	last := held && len(set) == 0
	h.mu.Unlock()
	if last {
		h.firePresence(c.user.ID, false)
	}
}

func (h *Hub) firePresence(userID uuid.UUID, online bool) {
	h.mu.RLock()
	listeners := slices.Clone(h.presence)
	h.mu.RUnlock()
	for _, fn := range listeners {
		fn(userID, online)
	}
}

func (h *Hub) dispatch(c *Conn, f Frame) {
	if f.T == TypePing {
		c.Send(NewFrame(TypePong, nil))
		return
	}
	h.mu.RLock()
	handler := h.handler
	h.mu.RUnlock()
	if handler == nil {
		h.log.Warn().Str("type", f.T).Msg("frame received before the handler was wired")
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, frameTimeout)
	defer cancel()
	var err error
	switch f.T {
	case TypeHello:
		var p HelloPayload
		if err = decode(f, &p); err == nil {
			c.setPushEndpoint(p.PushEndpoint)
			err = handler.Hello(ctx, c, p)
		}
	case TypeSend:
		err = handle(ctx, c, f, handler.Send)
	case TypeEdit:
		err = handle(ctx, c, f, handler.Edit)
	case TypeDelete:
		err = handle(ctx, c, f, handler.Delete)
	case TypeRead:
		err = handle(ctx, c, f, handler.Read)
	case TypeTyping:
		err = handle(ctx, c, f, handler.Typing)
	case TypeThreadSub:
		err = handle(ctx, c, f, handler.ThreadSub)
	default:
		h.log.Warn().Str("type", f.T).Str("user", c.user.Handle).Msg("unknown frame type")
		return
	}
	if err != nil {
		h.log.Warn().Err(err).Str("type", f.T).Str("user", c.user.Handle).Msg("frame rejected")
	}
}

func handle[P any](ctx context.Context, c *Conn, f Frame, fn func(context.Context, *Conn, P) error) error {
	var p P
	if err := decode(f, &p); err != nil {
		return err
	}
	return fn(ctx, c, p)
}

func decode(f Frame, v any) error {
	if len(f.D) == 0 {
		return nil
	}
	if err := json.Unmarshal(f.D, v); err != nil {
		return fmt.Errorf("decode %s payload: %w", f.T, err)
	}
	return nil
}
