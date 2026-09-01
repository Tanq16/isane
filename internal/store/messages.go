package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const messageCols = `id, container_id, seq, author_id, body, reply_to_id, thread_root_id, client_id,
	is_system, thread_reply_count, thread_last_reply_at, created_at, edited_at, deleted_at`

const messageColsQualified = `m.id, m.container_id, m.seq, m.author_id, m.body, m.reply_to_id, m.thread_root_id,
	m.client_id, m.is_system, m.thread_reply_count, m.thread_last_reply_at, m.created_at, m.edited_at, m.deleted_at`

const messageByClientIDSQL = `select ` + messageCols + ` from messages where author_id = $1 and client_id = $2`

const allocateSeqSQL = `update containers set last_seq = last_seq + 1 where id = $1 returning last_seq`

func scanMessage(row pgx.Row) (Message, error) {
	var m Message
	err := row.Scan(&m.ID, &m.ContainerID, &m.Seq, &m.AuthorID, &m.Body, &m.ReplyToID, &m.ThreadRootID,
		&m.ClientID, &m.IsSystem, &m.ThreadReplyCount, &m.ThreadLastReplyAt, &m.CreatedAt, &m.EditedAt, &m.DeletedAt)
	return m, err
}

func collectMessages(rows pgx.Rows) ([]Message, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) { return scanMessage(r) })
}

func pageLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}

func (db *DB) InsertMessage(ctx context.Context, m NewMessage) (Message, error) {
	var out Message
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		existing, err := scanMessage(tx.QueryRow(ctx, messageByClientIDSQL, m.AuthorID, m.ClientID))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return mapErr(err)
		}

		var seq int64
		if err := tx.QueryRow(ctx, allocateSeqSQL, m.ContainerID).Scan(&seq); err != nil {
			return mapErr(err)
		}

		inserted, err := scanMessage(tx.QueryRow(ctx, `insert into messages
			(id, container_id, seq, author_id, body, reply_to_id, thread_root_id, client_id, is_system)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9) returning `+messageCols,
			uuid.New(), m.ContainerID, seq, m.AuthorID, m.Body, m.ReplyToID, m.ThreadRootID, m.ClientID, m.IsSystem))
		if err != nil {
			return mapErr(err)
		}

		if len(m.MentionIDs) > 0 {
			if _, err := tx.Exec(ctx, insertMentionsSQL, inserted.ID, m.MentionIDs); err != nil {
				return mapErr(err)
			}
		}

		if m.ThreadRootID != nil {
			if _, err := tx.Exec(ctx, `update messages
				set thread_reply_count = thread_reply_count + 1, thread_last_reply_at = $2
				where id = $1`, *m.ThreadRootID, inserted.CreatedAt); err != nil {
				return mapErr(err)
			}
			if _, err := tx.Exec(ctx, `insert into thread_subscriptions (user_id, thread_root_id, state)
				values ($1, $2, 'subscribed') on conflict do nothing`, m.AuthorID, *m.ThreadRootID); err != nil {
				return mapErr(err)
			}
		}

		if len(m.AttachmentIDs) > 0 {
			if _, err := tx.Exec(ctx, `update attachments set message_id = $1
				where id = any($2::uuid[]) and message_id is null`, inserted.ID, m.AttachmentIDs); err != nil {
				return mapErr(err)
			}
		}

		out = inserted
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			retried, retryErr := db.MessageByClientID(ctx, m.AuthorID, m.ClientID)
			if retryErr == nil {
				return retried, nil
			}
		}
		return Message{}, fmt.Errorf("insert message: %w", err)
	}
	if err := db.hydrateRefs(ctx, []*Message{&out}); err != nil {
		return Message{}, fmt.Errorf("insert message: %w", err)
	}
	return out, nil
}

func (db *DB) GetMessage(ctx context.Context, id uuid.UUID) (Message, error) {
	m, err := scanMessage(db.Pool.QueryRow(ctx, `select `+messageCols+` from messages where id = $1`, id))
	if err != nil {
		return Message{}, fmt.Errorf("get message: %w", mapErr(err))
	}
	if err := db.hydrateRefs(ctx, []*Message{&m}); err != nil {
		return Message{}, fmt.Errorf("get message: %w", err)
	}
	return m, nil
}

func (db *DB) MessageByClientID(ctx context.Context, authorID uuid.UUID, clientID string) (Message, error) {
	m, err := scanMessage(db.Pool.QueryRow(ctx, messageByClientIDSQL, authorID, clientID))
	if err != nil {
		return Message{}, fmt.Errorf("get message by client id: %w", mapErr(err))
	}
	if err := db.hydrateRefs(ctx, []*Message{&m}); err != nil {
		return Message{}, fmt.Errorf("get message by client id: %w", err)
	}
	return m, nil
}

func (db *DB) EditMessage(ctx context.Context, id uuid.UUID, body string, mentionIDs []uuid.UUID) (Message, error) {
	var out Message
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		edited, err := scanMessage(tx.QueryRow(ctx, `update messages set body = $2, edited_at = now()
			where id = $1 and deleted_at is null returning `+messageCols, id, body))
		if err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, deleteMentionsSQL, id); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, insertMentionsSQL, id, mentionIDs); err != nil {
			return mapErr(err)
		}
		out = edited
		return nil
	})
	if err != nil {
		return Message{}, fmt.Errorf("edit message: %w", err)
	}
	if err := db.hydrateRefs(ctx, []*Message{&out}); err != nil {
		return Message{}, fmt.Errorf("edit message: %w", err)
	}
	return out, nil
}

func (db *DB) DeleteMessage(ctx context.Context, id uuid.UUID) (Message, error) {
	m, err := scanMessage(db.Pool.QueryRow(ctx, `update messages
		set body = '', deleted_at = coalesce(deleted_at, now())
		where id = $1 returning `+messageCols, id))
	if err != nil {
		return Message{}, fmt.Errorf("delete message: %w", mapErr(err))
	}
	return m, nil
}

func (db *DB) TimelineLatest(ctx context.Context, containerID uuid.UUID, limit int) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `select * from (
		select `+messageCols+` from messages
		where container_id = $1 and thread_root_id is null
		order by seq desc limit $2) w order by seq`, containerID, pageLimit(limit))
	return db.timeline(ctx, rows, err, "timeline latest")
}

func (db *DB) TimelineBefore(ctx context.Context, containerID uuid.UUID, beforeSeq int64, limit int) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `select * from (
		select `+messageCols+` from messages
		where container_id = $1 and thread_root_id is null and seq < $2
		order by seq desc limit $3) w order by seq`, containerID, beforeSeq, pageLimit(limit))
	return db.timeline(ctx, rows, err, "timeline before")
}

func (db *DB) TimelineAfter(ctx context.Context, containerID uuid.UUID, afterSeq int64, limit int) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `select `+messageCols+` from messages
		where container_id = $1 and thread_root_id is null and seq > $2
		order by seq limit $3`, containerID, afterSeq, pageLimit(limit))
	return db.timeline(ctx, rows, err, "timeline after")
}

func (db *DB) TimelineAround(ctx context.Context, containerID uuid.UUID, seq int64, limit int) ([]Message, error) {
	n := pageLimit(limit)
	above := n / 2
	rows, err := db.Pool.Query(ctx, `select * from (
		(select `+messageCols+` from messages
			where container_id = $1 and thread_root_id is null and seq <= $2
			order by seq desc limit $3)
		union all
		(select `+messageCols+` from messages
			where container_id = $1 and thread_root_id is null and seq > $2
			order by seq limit $4)) w order by seq`, containerID, seq, n-above, above)
	return db.timeline(ctx, rows, err, "timeline around")
}

func (db *DB) ThreadMessages(ctx context.Context, rootID uuid.UUID, afterSeq int64, limit int) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `select `+messageCols+` from messages
		where thread_root_id = $1 and seq > $2 order by seq limit $3`, rootID, afterSeq, pageLimit(limit))
	return db.timeline(ctx, rows, err, "thread messages")
}

func (db *DB) PromptHistory(ctx context.Context, containerID uuid.UUID, beforeSeq int64, limit int) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `select * from (
		select `+messageCols+` from messages
		where container_id = $1 and thread_root_id is null and seq < $2 and deleted_at is null
		order by seq desc limit $3) w order by seq`, containerID, beforeSeq, pageLimit(limit))
	return db.timeline(ctx, rows, err, "prompt history")
}

func (db *DB) ThreadHistory(ctx context.Context, rootID uuid.UUID) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `select `+messageCols+` from messages
		where (id = $1 or thread_root_id = $1) and deleted_at is null order by seq`, rootID)
	return db.timeline(ctx, rows, err, "thread history")
}

func (db *DB) timeline(ctx context.Context, rows pgx.Rows, queryErr error, what string) ([]Message, error) {
	if queryErr != nil {
		return nil, fmt.Errorf("%s: %w", what, mapErr(queryErr))
	}
	msgs, err := collectMessages(rows)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if err := db.hydrate(ctx, msgs); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return msgs, nil
}

func (db *DB) Search(ctx context.Context, userID uuid.UUID, query string, containerID *uuid.UUID, limit int) ([]SearchResult, error) {
	rows, err := db.Pool.Query(ctx, `select `+messageColsQualified+`,
		ts_rank_cd(m.search_tsv, websearch_to_tsquery('english', $2)) as rank
		from messages m
		join containers c on c.id = m.container_id
		where m.search_tsv @@ websearch_to_tsquery('english', $2)
		  and m.deleted_at is null
		  and ($3::uuid is null or m.container_id = $3)
		  and `+containerVisibleSQL+`
		order by rank desc, m.seq desc
		limit $4`, userID, query, containerID, pageLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", mapErr(err))
	}
	results, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SearchResult, error) {
		var res SearchResult
		err := r.Scan(&res.Message.ID, &res.Message.ContainerID, &res.Message.Seq, &res.Message.AuthorID,
			&res.Message.Body, &res.Message.ReplyToID, &res.Message.ThreadRootID, &res.Message.ClientID,
			&res.Message.IsSystem, &res.Message.ThreadReplyCount, &res.Message.ThreadLastReplyAt,
			&res.Message.CreatedAt, &res.Message.EditedAt, &res.Message.DeletedAt, &res.Rank)
		return res, err
	})
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	refs := make([]*Message, len(results))
	for i := range results {
		refs[i] = &results[i].Message
	}
	if err := db.hydrateRefs(ctx, refs); err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	return results, nil
}

func (db *DB) CountMessages(ctx context.Context) (int64, error) {
	var n int64
	if err := db.Pool.QueryRow(ctx, `select count(*) from messages`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count messages: %w", mapErr(err))
	}
	return n, nil
}

func (db *DB) DeleteMessagesOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update messages set reply_to_id = null
			where created_at >= $1 and reply_to_id in (select id from messages where created_at < $1)`, cutoff); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `update messages set thread_root_id = null
			where created_at >= $1 and thread_root_id in (select id from messages where created_at < $1)`, cutoff); err != nil {
			return mapErr(err)
		}
		tag, err := tx.Exec(ctx, `delete from messages where created_at < $1`, cutoff)
		if err != nil {
			return mapErr(err)
		}
		deleted = tag.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("delete old messages: %w", err)
	}
	return deleted, nil
}
