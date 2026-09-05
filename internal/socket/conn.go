package socket

import (
	"context"
	"encoding/json/v2"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/gorilla/websocket"
	"github.com/tanq16/isane/internal/store"
)

const (
	pingInterval  = 30 * time.Second
	pongWait      = 2*pingInterval + 5*time.Second
	writeWait     = 10 * time.Second
	frameTimeout  = 30 * time.Second
	sendBuffer    = 64
	maxFrameBytes = 1 << 20
)

type Conn struct {
	hub     *Hub
	ws      *websocket.Conn
	user    store.User
	session uuid.UUID
	out     chan Frame
	done    chan struct{}
	close   func()
	ctx     context.Context

	lastSeen atomic.Int64
	endpoint atomic.Pointer[string]
	visible  atomic.Bool
}

func newConn(h *Hub, ws *websocket.Conn, u store.User, sessionID uuid.UUID) *Conn {
	ctx, cancel := context.WithCancel(h.ctx)
	c := &Conn{
		hub:     h,
		ws:      ws,
		user:    u,
		session: sessionID,
		out:     make(chan Frame, sendBuffer),
		done:    make(chan struct{}),
		ctx:     ctx,
	}
	c.close = sync.OnceFunc(func() {
		close(c.done)
		cancel()
	})
	c.visible.Store(true)
	c.touch()
	return c
}

func (c *Conn) UserID() uuid.UUID { return c.user.ID }

func (c *Conn) User() store.User { return c.user }

func (c *Conn) PushEndpoint() string {
	if s := c.endpoint.Load(); s != nil {
		return *s
	}
	return ""
}

func (c *Conn) Send(f Frame) {
	select {
	case c.out <- f:
	default:
		c.hub.log.Warn().Str("user", c.user.Handle).Msg("socket buffer full, dropping connection")
		c.close()
	}
}

func (c *Conn) Visible() bool { return c.visible.Load() }

func (c *Conn) setPushEndpoint(endpoint string) { c.endpoint.Store(&endpoint) }

func (c *Conn) setVisible(visible bool) { c.visible.Store(visible) }

func (c *Conn) touch() { c.lastSeen.Store(time.Now().UnixNano()) }

func (c *Conn) seenWithin(d time.Duration) bool {
	return time.Since(time.Unix(0, c.lastSeen.Load())) <= d
}

func (c *Conn) readPump() {
	defer c.close()
	c.ws.SetReadLimit(maxFrameBytes)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		c.touch()
		return c.ws.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			c.hub.log.Debug().Err(err).Str("user", c.user.Handle).Msg("socket closed")
			return
		}
		c.touch()
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil {
			c.hub.log.Warn().Err(err).Str("user", c.user.Handle).Msg("malformed frame")
			continue
		}
		c.hub.dispatch(c, f)
	}
}

func (c *Conn) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.close()
		_ = c.ws.Close()
	}()
	for {
		select {
		case <-c.done:
			_ = c.ws.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(writeWait))
			return
		case f := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteJSON(f); err != nil {
				c.hub.log.Debug().Err(err).Str("user", c.user.Handle).Msg("socket write failed")
				return
			}
		case <-ticker.C:
			if err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				c.hub.log.Debug().Err(err).Str("user", c.user.Handle).Msg("socket ping failed")
				return
			}
		}
	}
}
