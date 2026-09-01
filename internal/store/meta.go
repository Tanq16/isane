package store

import (
	"context"
	"fmt"
)

func (db *DB) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := db.Pool.QueryRow(ctx, `select
		(select count(*) from messages),
		(select count(*) from users where kind = 'human'),
		(select count(*) from agents),
		(select count(*) from containers where kind = 'channel'),
		(select count(*) from containers where kind = 'conversation'),
		(select count(*) from sessions where expires_at > now()),
		(select coalesce(sum(size_bytes), 0)::bigint from attachments),
		pg_database_size(current_database())`).Scan(
		&s.Messages, &s.Users, &s.Agents, &s.Channels, &s.Conversations, &s.ActiveSessions,
		&s.MediaBytes, &s.DatabaseBytes)
	if err != nil {
		return Stats{}, fmt.Errorf("stats: %w", err)
	}
	recordingBytes, err := db.TotalRecordingBytes(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("stats: %w", err)
	}
	s.RecordingBytes = recordingBytes
	return s, nil
}

func (db *DB) MetaGet(ctx context.Context, key string) (string, error) {
	var value string
	err := db.Pool.QueryRow(ctx, `select value from server_meta where key = $1`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("meta get %s: %w", key, mapErr(err))
	}
	return value, nil
}

func (db *DB) MetaSet(ctx context.Context, key, value string) error {
	_, err := db.Pool.Exec(ctx, `insert into server_meta (key, value) values ($1, $2)
		on conflict (key) do update set value = excluded.value, updated_at = now()`, key, value)
	if err != nil {
		return fmt.Errorf("meta set %s: %w", key, mapErr(err))
	}
	return nil
}
