package store

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const (
	deleteMentionsSQL = `delete from mentions where message_id = $1`
	insertMentionsSQL = `insert into mentions (message_id, user_id)
		select $1::uuid, t.u from unnest($2::uuid[]) as t(u) on conflict do nothing`
)

func (db *DB) MentionIDs(ctx context.Context, messageID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx, `select user_id from mentions where message_id = $1`, messageID)
	if err != nil {
		return nil, fmt.Errorf("list mentions: %w", mapErr(err))
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("list mentions: %w", err)
	}
	return ids, nil
}

func (db *DB) MentionIDsForMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	out := make(map[uuid.UUID][]uuid.UUID, len(messageIDs))
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := db.Pool.Query(ctx,
		`select message_id, user_id from mentions where message_id = any($1::uuid[])`, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("list mentions: %w", mapErr(err))
	}
	defer rows.Close()
	for rows.Next() {
		var messageID, userID uuid.UUID
		if err := rows.Scan(&messageID, &userID); err != nil {
			return nil, fmt.Errorf("list mentions: %w", err)
		}
		out[messageID] = append(out[messageID], userID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list mentions: %w", err)
	}
	return out, nil
}

func (db *DB) ReplaceMentions(ctx context.Context, messageID uuid.UUID, userIDs []uuid.UUID) error {
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, deleteMentionsSQL, messageID); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, insertMentionsSQL, messageID, userIDs); err != nil {
			return mapErr(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("replace mentions: %w", err)
	}
	return nil
}
