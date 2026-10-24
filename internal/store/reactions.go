package store

import (
	"context"
	"fmt"
	"uuid"
)

func (db *DB) AddReaction(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	_, err := db.Pool.Exec(ctx, `insert into message_reactions (message_id, user_id, emoji)
		values ($1, $2, $3) on conflict do nothing`, messageID, userID, emoji)
	if err != nil {
		return fmt.Errorf("add reaction: %w", mapErr(err))
	}
	return nil
}

func (db *DB) RemoveReaction(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	_, err := db.Pool.Exec(ctx, `delete from message_reactions
		where message_id = $1 and user_id = $2 and emoji = $3`, messageID, userID, emoji)
	if err != nil {
		return fmt.Errorf("remove reaction: %w", mapErr(err))
	}
	return nil
}

func (db *DB) ReactionsForMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Reaction, error) {
	out := make(map[uuid.UUID][]Reaction, len(messageIDs))
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := db.Pool.Query(ctx, `select message_id, emoji, array_agg(user_id order by created_at, user_id)
		from message_reactions
		where message_id = any($1::uuid[])
		group by message_id, emoji
		order by message_id, min(created_at), emoji`, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("list reactions: %w", mapErr(err))
	}
	defer rows.Close()
	for rows.Next() {
		var messageID uuid.UUID
		var r Reaction
		if err := rows.Scan(&messageID, &r.Emoji, &r.Users); err != nil {
			return nil, fmt.Errorf("list reactions: %w", err)
		}
		out[messageID] = append(out[messageID], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list reactions: %w", err)
	}
	return out, nil
}
