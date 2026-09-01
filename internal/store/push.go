package store

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const pushColumns = `id, user_id, endpoint, p256dh, auth, user_agent, enabled, created_at,
	last_success_at, last_failure_at`

func scanPushSubscription(row pgx.Row) (PushSubscription, error) {
	var s PushSubscription
	err := row.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.UserAgent, &s.Enabled,
		&s.CreatedAt, &s.LastSuccessAt, &s.LastFailureAt)
	return s, err
}

func (db *DB) UpsertPushSubscription(ctx context.Context, s PushSubscription) (PushSubscription, error) {
	if s.ID == uuid.Nil() {
		s.ID = uuid.New()
	}
	out, err := scanPushSubscription(db.Pool.QueryRow(ctx, `insert into push_subscriptions
		(id, user_id, endpoint, p256dh, auth, user_agent, enabled)
		values ($1, $2, $3, $4, $5, $6, true)
		on conflict (endpoint) do update set
			user_id = excluded.user_id,
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_agent = excluded.user_agent,
			enabled = true
		returning `+pushColumns,
		s.ID, s.UserID, s.Endpoint, s.P256dh, s.Auth, s.UserAgent))
	if err != nil {
		return PushSubscription{}, fmt.Errorf("upsert push subscription: %w", mapErr(err))
	}
	return out, nil
}

func (db *DB) DeletePushSubscription(ctx context.Context, userID uuid.UUID, endpoint string) error {
	_, err := db.Pool.Exec(ctx,
		`delete from push_subscriptions where user_id = $1 and endpoint = $2`, userID, endpoint)
	if err != nil {
		return fmt.Errorf("delete push subscription: %w", mapErr(err))
	}
	return nil
}

func (db *DB) DeletePushSubscriptionByID(ctx context.Context, id uuid.UUID) error {
	if _, err := db.Pool.Exec(ctx, `delete from push_subscriptions where id = $1`, id); err != nil {
		return fmt.Errorf("delete push subscription: %w", mapErr(err))
	}
	return nil
}

func (db *DB) SetPushSubscriptionEnabled(ctx context.Context, userID uuid.UUID, endpoint string, enabled bool) error {
	return db.execOne(ctx, "set push subscription enabled",
		`update push_subscriptions set enabled = $3 where user_id = $1 and endpoint = $2`,
		userID, endpoint, enabled)
}

func (db *DB) ListPushSubscriptions(ctx context.Context, userID uuid.UUID) ([]PushSubscription, error) {
	rows, err := db.Pool.Query(ctx, `select `+pushColumns+`
		from push_subscriptions where user_id = $1 and enabled order by created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", mapErr(err))
	}
	list, err := pgx.CollectRows(rows,
		func(r pgx.CollectableRow) (PushSubscription, error) { return scanPushSubscription(r) })
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	return list, nil
}

func (db *DB) MarkPushSuccess(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx,
		`update push_subscriptions set last_success_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark push success: %w", mapErr(err))
	}
	return nil
}

func (db *DB) MarkPushFailure(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx,
		`update push_subscriptions set last_failure_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark push failure: %w", mapErr(err))
	}
	return nil
}
