package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

const attachmentsForMessagesSQL = `select id, message_id, uploader_id, kind::text, original_name, mime,
	size_bytes, width, height, duration_ms, storage_path, thumb_path, state::text, error, created_at
	from attachments where message_id = any($1::uuid[]) order by created_at, id`

func (db *DB) AttachmentsForMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Attachment, error) {
	out := make(map[uuid.UUID][]Attachment, len(messageIDs))
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := db.Pool.Query(ctx, attachmentsForMessagesSQL, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", mapErr(err))
	}
	defer rows.Close()
	for rows.Next() {
		var a Attachment
		err := rows.Scan(&a.ID, &a.MessageID, &a.UploaderID, &a.Kind, &a.OriginalName, &a.Mime,
			&a.SizeBytes, &a.Width, &a.Height, &a.DurationMs, &a.StoragePath, &a.ThumbPath,
			&a.State, &a.Error, &a.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("list attachments: %w", err)
		}
		if a.MessageID != nil {
			out[*a.MessageID] = append(out[*a.MessageID], a)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return out, nil
}

func (db *DB) hydrate(ctx context.Context, msgs []Message) error {
	refs := make([]*Message, len(msgs))
	for i := range msgs {
		refs[i] = &msgs[i]
	}
	return db.hydrateRefs(ctx, refs)
}

func (db *DB) hydrateRefs(ctx context.Context, msgs []*Message) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	attachments, err := db.AttachmentsForMessages(ctx, ids)
	if err != nil {
		return err
	}
	mentions, err := db.MentionIDsForMessages(ctx, ids)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		m.Attachments = attachments[m.ID]
		m.Mentions = mentions[m.ID]
	}
	return nil
}
