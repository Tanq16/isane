package store

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const auditColumns = `id, at, actor_id, actor_handle, action, target_type, target_id, target_label,
	detail, ip, user_agent`

func (db *DB) CreateAuditEvent(ctx context.Context, e AuditEvent) error {
	if e.ID == uuid.Nil() {
		e.ID = uuid.NewV7()
	}
	_, err := db.Pool.Exec(ctx, `insert into audit_events
		(id, actor_id, actor_handle, action, target_type, target_id, target_label, detail, ip, user_agent)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.ID, e.ActorID, e.ActorHandle, e.Action, e.TargetType, e.TargetID, e.TargetLabel,
		[]byte(e.Detail), e.IP, e.UserAgent)
	if err != nil {
		return fmt.Errorf("create audit event: %w", mapErr(err))
	}
	return nil
}

func (db *DB) ListAuditEvents(ctx context.Context, before *uuid.UUID, limit int) ([]AuditEvent, error) {
	rows, err := db.Pool.Query(ctx, `select `+auditColumns+` from audit_events
		where ($1::uuid is null or id < $1) order by id desc limit $2`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", mapErr(err))
	}
	events, err := pgx.CollectRows(rows, scanAuditEvent)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	return events, nil
}

func (db *DB) DeleteAuditEventsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `delete from audit_events where at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete audit events: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanAuditEvent(row pgx.CollectableRow) (AuditEvent, error) {
	var e AuditEvent
	var detail []byte
	err := row.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorHandle, &e.Action, &e.TargetType, &e.TargetID,
		&e.TargetLabel, &detail, &e.IP, &e.UserAgent)
	e.Detail = jsontext.Value(detail)
	return e, err
}
