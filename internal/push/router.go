package push

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync"
	"uuid"

	"github.com/tanq16/isane/internal/config"
	"github.com/tanq16/isane/internal/store"
)

const sendWorkers = 8

type Router struct {
	cfg    *config.Config
	db     *store.DB
	client *vapidClient
}

type Delivery struct {
	SubscriptionID uuid.UUID
	Err            error
	StoreErr       error
}

func New(cfg *config.Config, db *store.DB) *Router {
	return &Router{
		cfg:    cfg,
		db:     db,
		client: newVAPIDClient(),
	}
}

func (r *Router) Enabled() bool { return r.cfg.PushEnabled() }

func (r *Router) VAPIDPublicKey() string { return r.cfg.Push.VAPIDPublicKey }

func (r *Router) Route(ctx context.Context, m store.Message, author store.User, c store.Container) ([]Delivery, error) {
	if !r.Enabled() {
		return nil, nil
	}
	recipients, err := r.recipients(ctx, m, author, c)
	if err != nil {
		return nil, fmt.Errorf("resolve notification recipients: %w", err)
	}
	var targets []target
	var errs []error
	for _, userID := range recipients {
		t, err := r.targets(ctx, userID, m, author, c)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolve push targets for user %s: %w", userID, err))
			continue
		}
		targets = append(targets, t...)
	}
	deliveries := make([]Delivery, len(targets))
	sem := make(chan struct{}, sendWorkers)
	var wg sync.WaitGroup
	for i, t := range targets {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			deliveries[i] = r.deliver(ctx, t)
		})
	}
	wg.Wait()
	return deliveries, errors.Join(errs...)
}

type routing struct {
	prefs     map[uuid.UUID]store.NotificationLevel
	threads   map[uuid.UUID]store.ThreadSubState
	mentioned map[uuid.UUID]struct{}
}

func (rt routing) shouldNotify(userID uuid.UUID, m store.Message, c store.Container) bool {
	level := rt.prefs[userID]
	if level == "" {
		level = store.DefaultNotificationLevel(c.Kind)
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
		return nil, err
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
	rt.prefs, err = r.db.NotificationPrefsFor(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if m.ThreadRootID != nil {
		rt.threads, err = r.db.ThreadSubscriptions(ctx, *m.ThreadRootID)
		if err != nil {
			return nil, err
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
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}
	badge, err := r.db.UnreadTotal(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count unread total: %w", err)
	}
	body, err := json.Marshal(buildPayload(r.cfg.Server.PublicURL, m, author, c, badge))
	if err != nil {
		return nil, fmt.Errorf("encode push payload: %w", err)
	}
	out := make([]target, 0, len(subs))
	for _, s := range subs {
		out = append(out, target{sub: s, body: bytes.Clone(body)})
	}
	return out, nil
}

func (r *Router) deliver(ctx context.Context, t target) Delivery {
	d := Delivery{SubscriptionID: t.sub.ID, Err: r.send(ctx, t.sub, t.body)}
	switch {
	case d.Err == nil:
		d.StoreErr = r.db.MarkPushSuccess(ctx, t.sub.ID)
	case errors.Is(d.Err, ErrSubscriptionGone):
		d.StoreErr = r.db.DeletePushSubscriptionByID(ctx, t.sub.ID)
	case errors.Is(d.Err, ErrRateLimited):
	default:
		d.StoreErr = r.db.MarkPushFailure(ctx, t.sub.ID)
	}
	return d
}
