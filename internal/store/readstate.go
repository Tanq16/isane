package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (db *DB) SetReadMarker(ctx context.Context, userID, containerID uuid.UUID, seq int64) (int64, error) {
	var effective int64
	err := db.Pool.QueryRow(ctx, `insert into read_markers (user_id, container_id, last_read_seq)
		values ($1, $2, greatest($3::bigint, 0))
		on conflict (user_id, container_id) do update
		set last_read_seq = greatest(read_markers.last_read_seq, excluded.last_read_seq), updated_at = now()
		returning last_read_seq`, userID, containerID, seq).Scan(&effective)
	if err != nil {
		return 0, fmt.Errorf("set read marker: %w", mapErr(err))
	}
	return effective, nil
}

func (db *DB) ReadMarker(ctx context.Context, userID, containerID uuid.UUID) (int64, error) {
	var seq int64
	err := db.Pool.QueryRow(ctx, `select coalesce(
		(select last_read_seq from read_markers where user_id = $1 and container_id = $2), 0)`,
		userID, containerID).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("get read marker: %w", mapErr(err))
	}
	return seq, nil
}

func (db *DB) UnreadCounts(ctx context.Context, userID, containerID uuid.UUID) (unread, mentions int64, err error) {
	err = db.Pool.QueryRow(ctx, `select
		greatest(c.last_seq - coalesce(r.last_read_seq, 0), 0),
		(select count(*) from mentions m
			join messages msg on msg.id = m.message_id
			where m.user_id = $1 and msg.container_id = c.id
			  and msg.seq > coalesce(r.last_read_seq, 0) and msg.deleted_at is null)
		from containers c
		left join read_markers r on r.container_id = c.id and r.user_id = $1
		where c.id = $2`, userID, containerID).Scan(&unread, &mentions)
	if err != nil {
		return 0, 0, fmt.Errorf("count unread: %w", mapErr(err))
	}
	return unread, mentions, nil
}

func (db *DB) TotalMentions(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	err := db.Pool.QueryRow(ctx, `select count(*)
		from mentions m
		join messages msg on msg.id = m.message_id
		join containers c on c.id = msg.container_id
		left join read_markers r on r.container_id = c.id and r.user_id = $1
		where m.user_id = $1
		  and msg.deleted_at is null
		  and msg.seq > coalesce(r.last_read_seq, 0)
		  and `+containerVisibleSQL, userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count mentions: %w", mapErr(err))
	}
	return n, nil
}

func (db *DB) SetNotificationPref(ctx context.Context, userID, containerID uuid.UUID, level NotificationLevel) error {
	_, err := db.Pool.Exec(ctx, `insert into notification_prefs (user_id, container_id, level)
		values ($1, $2, $3::notification_level)
		on conflict (user_id, container_id) do update set level = excluded.level`,
		userID, containerID, string(level))
	if err != nil {
		return fmt.Errorf("set notification pref: %w", mapErr(err))
	}
	return nil
}

func (db *DB) NotificationPref(ctx context.Context, userID, containerID uuid.UUID) (NotificationLevel, error) {
	var level NotificationLevel
	err := db.Pool.QueryRow(ctx, `select coalesce(
		(select level::text from notification_prefs where user_id = $1 and container_id = $2), 'mentions')`,
		userID, containerID).Scan(&level)
	if err != nil {
		return "", fmt.Errorf("get notification pref: %w", mapErr(err))
	}
	return level, nil
}

func (db *DB) NotificationPrefsFor(ctx context.Context, containerID uuid.UUID) (map[uuid.UUID]NotificationLevel, error) {
	rows, err := db.Pool.Query(ctx,
		`select user_id, level::text from notification_prefs where container_id = $1`, containerID)
	if err != nil {
		return nil, fmt.Errorf("list notification prefs: %w", mapErr(err))
	}
	defer rows.Close()
	out := make(map[uuid.UUID]NotificationLevel)
	for rows.Next() {
		var userID uuid.UUID
		var level NotificationLevel
		if err := rows.Scan(&userID, &level); err != nil {
			return nil, fmt.Errorf("list notification prefs: %w", err)
		}
		out[userID] = level
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list notification prefs: %w", err)
	}
	return out, nil
}

func (db *DB) SetThreadSubscription(ctx context.Context, userID, rootID uuid.UUID, state ThreadSubState) error {
	_, err := db.Pool.Exec(ctx, `insert into thread_subscriptions (user_id, thread_root_id, state)
		values ($1, $2, $3::thread_sub_state)
		on conflict (user_id, thread_root_id) do update set state = excluded.state`,
		userID, rootID, string(state))
	if err != nil {
		return fmt.Errorf("set thread subscription: %w", mapErr(err))
	}
	return nil
}

func (db *DB) ThreadSubscription(ctx context.Context, userID, rootID uuid.UUID) (ThreadSubState, bool, error) {
	var state ThreadSubState
	err := db.Pool.QueryRow(ctx,
		`select state::text from thread_subscriptions where user_id = $1 and thread_root_id = $2`,
		userID, rootID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get thread subscription: %w", mapErr(err))
	}
	return state, true, nil
}

func (db *DB) ThreadSubscriptions(ctx context.Context, rootID uuid.UUID) (map[uuid.UUID]ThreadSubState, error) {
	rows, err := db.Pool.Query(ctx,
		`select user_id, state::text from thread_subscriptions where thread_root_id = $1`, rootID)
	if err != nil {
		return nil, fmt.Errorf("list thread subscriptions: %w", mapErr(err))
	}
	defer rows.Close()
	out := make(map[uuid.UUID]ThreadSubState)
	for rows.Next() {
		var userID uuid.UUID
		var state ThreadSubState
		if err := rows.Scan(&userID, &state); err != nil {
			return nil, fmt.Errorf("list thread subscriptions: %w", err)
		}
		out[userID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list thread subscriptions: %w", err)
	}
	return out, nil
}
