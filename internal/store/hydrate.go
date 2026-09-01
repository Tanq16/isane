package store

import (
	"context"
	"uuid"
)

func (db *DB) AttachmentsForMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Attachment, error) {
	out := make(map[uuid.UUID][]Attachment, len(messageIDs))
	if len(messageIDs) == 0 {
		return out, nil
	}
	list, err := db.queryAttachments(ctx, "list attachments", `select `+attachmentColumns+`
		from attachments where message_id = any($1::uuid[]) order by created_at, id`, messageIDs)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.MessageID != nil {
			out[*a.MessageID] = append(out[*a.MessageID], a)
		}
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
