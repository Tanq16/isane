package store

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const attachmentColumns = `id, message_id, uploader_id, kind, original_name, mime, size_bytes,
	width, height, duration_ms, storage_path, thumb_path, state, error, created_at`

func scanAttachment(row pgx.Row) (Attachment, error) {
	var a Attachment
	err := row.Scan(&a.ID, &a.MessageID, &a.UploaderID, &a.Kind, &a.OriginalName, &a.Mime,
		&a.SizeBytes, &a.Width, &a.Height, &a.DurationMs, &a.StoragePath, &a.ThumbPath,
		&a.State, &a.Error, &a.CreatedAt)
	return a, err
}

func (db *DB) queryAttachments(ctx context.Context, op, sql string, args ...any) ([]Attachment, error) {
	rows, err := db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, mapErr(err))
	}
	list, err := pgx.CollectRows(rows,
		func(r pgx.CollectableRow) (Attachment, error) { return scanAttachment(r) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return list, nil
}

func (db *DB) CreateAttachment(ctx context.Context, a Attachment) (Attachment, error) {
	if a.ID == uuid.Nil() {
		a.ID = uuid.New()
	}
	out, err := scanAttachment(db.Pool.QueryRow(ctx, `insert into attachments
		(id, message_id, uploader_id, kind, original_name, mime, size_bytes,
		 width, height, duration_ms, storage_path, thumb_path, state, error)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		returning `+attachmentColumns,
		a.ID, a.MessageID, a.UploaderID, a.Kind, a.OriginalName, a.Mime, a.SizeBytes,
		a.Width, a.Height, a.DurationMs, a.StoragePath, a.ThumbPath, a.State, a.Error))
	if err != nil {
		return Attachment{}, fmt.Errorf("insert attachment: %w", mapErr(err))
	}
	return out, nil
}

func (db *DB) GetAttachment(ctx context.Context, id uuid.UUID) (Attachment, error) {
	a, err := scanAttachment(db.Pool.QueryRow(ctx,
		`select `+attachmentColumns+` from attachments where id = $1`, id))
	if err != nil {
		return Attachment{}, fmt.Errorf("get attachment: %w", mapErr(err))
	}
	return a, nil
}

func (db *DB) SetAttachmentState(ctx context.Context, id uuid.UUID, state AttachmentState, errText *string) error {
	return db.execOne(ctx, "set attachment state",
		`update attachments set state = $2, error = $3 where id = $1`, id, state, errText)
}

func (db *DB) FinishAttachment(ctx context.Context, id uuid.UUID, mime string, sizeBytes int64, width, height, durationMs *int, thumbPath *string) error {
	return db.execOne(ctx, "finish attachment", `update attachments
		set mime = $2, size_bytes = $3, width = $4, height = $5, duration_ms = $6, thumb_path = $7,
		    state = 'ready', error = null
		where id = $1`, id, mime, sizeBytes, width, height, durationMs, thumbPath)
}

func (db *DB) ListStagedBefore(ctx context.Context, cutoff time.Time) ([]Attachment, error) {
	return db.queryAttachments(ctx, "list staged attachments", `select `+attachmentColumns+`
		from attachments
		where message_id is null and created_at < $1
		order by created_at`, cutoff)
}

func (db *DB) DeleteAttachment(ctx context.Context, id uuid.UUID) error {
	if _, err := db.Pool.Exec(ctx, `delete from attachments where id = $1`, id); err != nil {
		return fmt.Errorf("delete attachment: %w", mapErr(err))
	}
	return nil
}

func (db *DB) TotalMediaBytes(ctx context.Context) (int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`select coalesce(sum(size_bytes), 0)::bigint from attachments`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("total media bytes: %w", err)
	}
	return total, nil
}

func (db *DB) RequeueProcessingAttachments(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx,
		`update attachments set state = 'staged' where state = 'processing' returning id`)
	if err != nil {
		return nil, fmt.Errorf("requeue processing attachments: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("requeue processing attachments: %w", err)
	}
	return ids, nil
}
