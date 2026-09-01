package push

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/tanq16/isane/internal/store"
)

const (
	recordOverhead = 16 + 4 + 1 + 65 + 16 + 1 // salt, record size, key length, public key, GCM tag, padding delimiter
	maxPlaintext   = int(webpush.MaxRecordSize) - recordOverhead
	pushTTL        = 86400
	sendTimeout    = 10 * time.Second
	vapidReuse     = time.Hour
)

var (
	errSubscriptionGone = errors.New("push subscription gone")
	errRateLimited      = errors.New("push endpoint rate limited")
)

func (r *Router) send(ctx context.Context, s store.PushSubscription, body []byte) error {
	if len(body) > maxPlaintext {
		return fmt.Errorf("payload is %d bytes over the %d byte limit", len(body)-maxPlaintext, maxPlaintext)
	}
	sub := &webpush.Subscription{
		Endpoint: s.Endpoint,
		Keys:     webpush.Keys{P256dh: s.P256dh, Auth: s.Auth},
	}
	opts := &webpush.Options{
		HTTPClient: r.client,
		RecordSize: uint32(len(body) + recordOverhead),
		// webpush-go prepends its own mailto:, and Apple rejects mailto:mailto: with 403 BadJwtToken.
		Subscriber:      strings.TrimPrefix(r.cfg.Push.Subject, "mailto:"),
		TTL:             pushTTL,
		Urgency:         webpush.UrgencyHigh,
		VAPIDPublicKey:  r.cfg.Push.VAPIDPublicKey,
		VAPIDPrivateKey: r.cfg.Push.VAPIDPrivateKey,
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	resp, err := webpush.SendNotificationWithContext(ctx, body, sub, opts)
	if err != nil {
		return fmt.Errorf("post push message: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone:
		return fmt.Errorf("push endpoint returned %d: %w", resp.StatusCode, errSubscriptionGone)
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("push endpoint returned %d: %w", resp.StatusCode, errRateLimited)
	default:
		return fmt.Errorf("push endpoint returned %d", resp.StatusCode)
	}
}

type vapidClient struct {
	http  *http.Client
	mu    sync.Mutex
	cache map[string]vapidHeader
}

type vapidHeader struct {
	value   string
	renewAt time.Time
}

func newVAPIDClient() *vapidClient {
	return &vapidClient{
		http: &http.Client{
			Timeout: sendTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		cache: map[string]vapidHeader{},
	}
}

func (c *vapidClient) Do(req *http.Request) (*http.Response, error) {
	audience := req.URL.Scheme + "://" + req.URL.Host
	// webpush-go signs a fresh VAPID JWT per request; push services rate-limit rotation faster than hourly.
	c.mu.Lock()
	if h, ok := c.cache[audience]; ok && time.Now().Before(h.renewAt) {
		req.Header.Set("Authorization", h.value)
	} else {
		c.cache[audience] = vapidHeader{value: req.Header.Get("Authorization"), renewAt: time.Now().Add(vapidReuse)}
	}
	c.mu.Unlock()
	return c.http.Do(req)
}
