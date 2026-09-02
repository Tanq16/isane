package push

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	"github.com/rs/zerolog"
	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/store"
)

const (
	socketWindow = 30 * time.Second
	sendWorkers  = 8
)

type Presence interface {
	EndpointVisible(userID uuid.UUID, endpoint string, within time.Duration) bool
}

type Router struct {
	cfg      *config.Config
	db       *store.DB
	presence Presence
	log      zerolog.Logger
	client   *vapidClient
}

func New(cfg *config.Config, db *store.DB, presence Presence, log zerolog.Logger) *Router {
	return &Router{
		cfg:      cfg,
		db:       db,
		presence: presence,
		log:      log,
		client:   newVAPIDClient(),
	}
}

func (r *Router) Enabled() bool { return r.cfg.PushEnabled() }

func (r *Router) VAPIDPublicKey() string { return r.cfg.Push.VAPIDPublicKey }

func (r *Router) Route(ctx context.Context, m store.Message, author store.User, c store.Container) {
	if !r.Enabled() {
		return
	}
	recipients, err := r.recipients(ctx, m, author, c)
	if err != nil {
		r.log.Error().Err(err).Str("message_id", m.ID.String()).Msg("resolve notification recipients")
		return
	}
	var targets []target
	for _, userID := range recipients {
		t, err := r.targets(ctx, userID, m, author, c)
		if err != nil {
			r.log.Error().Err(err).Str("user_id", userID.String()).Msg("resolve push targets")
			continue
		}
		targets = append(targets, t...)
	}
	if len(targets) == 0 {
		return
	}
	sem := make(chan struct{}, sendWorkers)
	var wg sync.WaitGroup
	for _, t := range targets {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			r.deliver(ctx, t)
		})
	}
	wg.Wait()
}

type routing struct {
	prefs     map[uuid.UUID]store.NotificationLevel
	threads   map[uuid.UUID]store.ThreadSubState
	mentioned map[uuid.UUID]struct{}
}

func (rt routing) shouldNotify(userID uuid.UUID, m store.Message, c store.Container) bool {
	if c.Kind == store.ContainerConversation {
		return true
	}
	level := rt.prefs[userID]
	if level == "" {
		level = store.LevelMentions
	}
	if level == store.LevelNone {
		return false
	}
	_, mentioned := rt.mentioned[userID]
	if m.ThreadRootID != nil {
		sub := rt.threads[userID]
		if sub == store.ThreadMuted {
			return false
		}
		if mentioned {
			return true
		}
		return sub == store.ThreadSubscribed || level == store.LevelAll
	}
	if level == store.LevelAll {
		return true
	}
	return mentioned
}

func (r *Router) recipients(ctx context.Context, m store.Message, author store.User, c store.Container) ([]uuid.UUID, error) {
	memberIDs, err := r.db.MemberIDs(ctx, c.ID)
	if err != nil {
		return nil, fmt.Errorf("list container members: %w", err)
	}
	humans, err := r.db.ListActiveHumans(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active humans: %w", err)
	}
	human := make(map[uuid.UUID]struct{}, len(humans))
	for _, u := range humans {
		human[u.ID] = struct{}{}
	}
	mentionIDs, err := r.db.MentionIDs(ctx, m.ID)
	if err != nil {
		return nil, fmt.Errorf("list message mentions: %w", err)
	}
	rt := routing{mentioned: make(map[uuid.UUID]struct{}, len(mentionIDs))}
	for _, id := range mentionIDs {
		rt.mentioned[id] = struct{}{}
	}
	if c.Kind == store.ContainerChannel {
		rt.prefs, err = r.db.NotificationPrefsFor(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("list notification prefs: %w", err)
		}
		if m.ThreadRootID != nil {
			rt.threads, err = r.db.ThreadSubscriptions(ctx, *m.ThreadRootID)
			if err != nil {
				return nil, fmt.Errorf("list thread subscriptions: %w", err)
			}
		}
	}
	out := make([]uuid.UUID, 0, len(memberIDs))
	for _, userID := range memberIDs {
		if userID == author.ID {
			continue
		}
		if _, ok := human[userID]; !ok {
			continue
		}
		if !rt.shouldNotify(userID, m, c) {
			continue
		}
		out = append(out, userID)
	}
	return out, nil
}

type target struct {
	sub  store.PushSubscription
	body []byte
}

func (r *Router) targets(ctx context.Context, userID uuid.UUID, m store.Message, author store.User, c store.Container) ([]target, error) {
	subs, err := r.db.ListPushSubscriptions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	hidden := make([]store.PushSubscription, 0, len(subs))
	for _, s := range subs {
		if r.presence.EndpointVisible(userID, s.Endpoint, socketWindow) {
			continue
		}
		hidden = append(hidden, s)
	}
	if len(hidden) == 0 {
		return nil, nil
	}
	badge, err := r.db.TotalMentions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count unread mentions: %w", err)
	}
	body, err := json.Marshal(buildPayload(r.cfg.Server.PublicURL, m, author, c, badge))
	if err != nil {
		return nil, fmt.Errorf("encode push payload: %w", err)
	}
	out := make([]target, 0, len(hidden))
	for _, s := range hidden {
		out = append(out, target{sub: s, body: bytes.Clone(body)})
	}
	return out, nil
}

func (r *Router) deliver(ctx context.Context, t target) {
	err := r.send(ctx, t.sub, t.body)
	switch {
	case err == nil:
		if markErr := r.db.MarkPushSuccess(ctx, t.sub.ID); markErr != nil {
			r.log.Error().Err(markErr).Str("subscription_id", t.sub.ID.String()).Msg("mark push success")
		}
	case errors.Is(err, errSubscriptionGone):
		r.log.Info().Err(err).Str("subscription_id", t.sub.ID.String()).Msg("deleting dead push subscription")
		if delErr := r.db.DeletePushSubscriptionByID(ctx, t.sub.ID); delErr != nil {
			r.log.Error().Err(delErr).Str("subscription_id", t.sub.ID.String()).Msg("delete push subscription")
		}
	case errors.Is(err, errRateLimited):
		r.log.Warn().Err(err).Str("subscription_id", t.sub.ID.String()).Msg("push delivery throttled")
	default:
		r.log.Warn().Err(err).Str("subscription_id", t.sub.ID.String()).Msg("push delivery failed")
		if markErr := r.db.MarkPushFailure(ctx, t.sub.ID); markErr != nil {
			r.log.Error().Err(markErr).Str("subscription_id", t.sub.ID.String()).Msg("mark push failure")
		}
	}
}
