package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const callColumns = `id, container_id, room_name, started_by, started_at, ended_at, recording_state`

const recordingColumns = `id, call_id, user_id, egress_id, storage_path, duration_ms, created_at`

func scanCall(row pgx.Row) (Call, error) {
	var c Call
	err := row.Scan(&c.ID, &c.ContainerID, &c.RoomName, &c.StartedBy, &c.StartedAt, &c.EndedAt,
		&c.RecordingState)
	return c, err
}

func scanRecording(row pgx.Row) (CallRecording, error) {
	var r CallRecording
	err := row.Scan(&r.ID, &r.CallID, &r.UserID, &r.EgressID, &r.StoragePath, &r.DurationMs,
		&r.CreatedAt)
	return r, err
}

func (db *DB) queryRecordings(ctx context.Context, op, sql string, args ...any) ([]CallRecording, error) {
	rows, err := db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, mapErr(err))
	}
	list, err := pgx.CollectRows(rows,
		func(r pgx.CollectableRow) (CallRecording, error) { return scanRecording(r) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return list, nil
}

func (db *DB) withLiveParticipants(ctx context.Context, call Call) (Call, error) {
	ids, err := db.LiveCallParticipants(ctx, call.ID)
	if err != nil {
		return Call{}, err
	}
	call.Participants = ids
	return call, nil
}

func (db *DB) StartCall(ctx context.Context, containerID, startedBy uuid.UUID, roomName string) (Call, bool, error) {
	call, err := scanCall(db.Pool.QueryRow(ctx, `insert into calls (id, container_id, room_name, started_by)
		values ($1, $2, $3, $4) returning `+callColumns,
		uuid.New(), containerID, roomName, startedBy))
	if err == nil {
		return call, true, nil
	}
	if !IsUniqueViolation(err) {
		return Call{}, false, fmt.Errorf("start call: %w", mapErr(err))
	}
	live, err := db.LiveCall(ctx, containerID)
	if err != nil {
		return Call{}, false, fmt.Errorf("start call: %w", err)
	}
	return live, false, nil
}

func (db *DB) GetCall(ctx context.Context, id uuid.UUID) (Call, error) {
	call, err := scanCall(db.Pool.QueryRow(ctx,
		`select `+callColumns+` from calls where id = $1`, id))
	if err != nil {
		return Call{}, fmt.Errorf("get call: %w", mapErr(err))
	}
	return db.withLiveParticipants(ctx, call)
}

func (db *DB) CallByRoom(ctx context.Context, roomName string) (Call, error) {
	call, err := scanCall(db.Pool.QueryRow(ctx,
		`select `+callColumns+` from calls where room_name = $1`, roomName))
	if err != nil {
		return Call{}, fmt.Errorf("get call by room: %w", mapErr(err))
	}
	return call, nil
}

func (db *DB) LiveCall(ctx context.Context, containerID uuid.UUID) (Call, error) {
	call, err := scanCall(db.Pool.QueryRow(ctx, `select `+callColumns+`
		from calls where container_id = $1 and ended_at is null`, containerID))
	if err != nil {
		return Call{}, fmt.Errorf("get live call: %w", mapErr(err))
	}
	return db.withLiveParticipants(ctx, call)
}

func (db *DB) ListLiveCalls(ctx context.Context) ([]Call, error) {
	rows, err := db.Pool.Query(ctx,
		`select `+callColumns+` from calls where ended_at is null order by started_at`)
	if err != nil {
		return nil, fmt.Errorf("list live calls: %w", mapErr(err))
	}
	calls, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Call, error) { return scanCall(r) })
	if err != nil {
		return nil, fmt.Errorf("list live calls: %w", err)
	}
	if len(calls) == 0 {
		return calls, nil
	}
	ids := make([]uuid.UUID, len(calls))
	for i, call := range calls {
		ids[i] = call.ID
	}
	byCall, err := db.liveParticipantsByCall(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range calls {
		calls[i].Participants = byCall[calls[i].ID]
	}
	return calls, nil
}

func (db *DB) liveParticipantsByCall(ctx context.Context, callIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx, `select distinct call_id, user_id from call_participants
		where left_at is null and call_id = any($1)`, callIDs)
	if err != nil {
		return nil, fmt.Errorf("list call participants: %w", mapErr(err))
	}
	defer rows.Close()
	byCall := make(map[uuid.UUID][]uuid.UUID, len(callIDs))
	for rows.Next() {
		var callID, userID uuid.UUID
		if err := rows.Scan(&callID, &userID); err != nil {
			return nil, fmt.Errorf("list call participants: %w", err)
		}
		byCall[callID] = append(byCall[callID], userID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list call participants: %w", err)
	}
	return byCall, nil
}

func (db *DB) EndCall(ctx context.Context, id uuid.UUID) error {
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update calls set ended_at = coalesce(ended_at, now()) where id = $1`, id)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `update call_participants set left_at = now()
			where call_id = $1 and left_at is null`, id)
		return mapErr(err)
	})
	if err != nil {
		return fmt.Errorf("end call: %w", err)
	}
	return nil
}

func (db *DB) JoinCall(ctx context.Context, callID, userID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `insert into call_participants (call_id, user_id)
		select $1, $2
		where not exists (select 1 from call_participants
			where call_id = $1 and user_id = $2 and left_at is null)`, callID, userID)
	if err != nil {
		return fmt.Errorf("join call: %w", mapErr(err))
	}
	return nil
}

func (db *DB) LeaveCall(ctx context.Context, callID, userID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `update call_participants set left_at = now()
		where call_id = $1 and user_id = $2 and left_at is null`, callID, userID)
	if err != nil {
		return fmt.Errorf("leave call: %w", mapErr(err))
	}
	return nil
}

func (db *DB) LiveCallParticipants(ctx context.Context, callID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx, `select distinct user_id from call_participants
		where call_id = $1 and left_at is null`, callID)
	if err != nil {
		return nil, fmt.Errorf("list live call participants: %w", mapErr(err))
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("list live call participants: %w", err)
	}
	return ids, nil
}

func (db *DB) SetRecordingState(ctx context.Context, callID uuid.UUID, state RecordingState) error {
	return db.execOne(ctx, "set recording state",
		`update calls set recording_state = $2 where id = $1`, callID, state)
}

func (db *DB) CreateCallRecording(ctx context.Context, r CallRecording) (CallRecording, error) {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	out, err := scanRecording(db.Pool.QueryRow(ctx, `insert into call_recordings
		(id, call_id, user_id, egress_id, storage_path, duration_ms)
		values ($1, $2, $3, $4, $5, $6)
		returning `+recordingColumns,
		r.ID, r.CallID, r.UserID, r.EgressID, r.StoragePath, r.DurationMs))
	if err != nil {
		return CallRecording{}, fmt.Errorf("insert call recording: %w", mapErr(err))
	}
	return out, nil
}

func (db *DB) ListCallRecordings(ctx context.Context, callID uuid.UUID) ([]CallRecording, error) {
	return db.queryRecordings(ctx, "list call recordings", `select `+recordingColumns+`
		from call_recordings where call_id = $1 order by created_at`, callID)
}

func (db *DB) CompleteCallRecording(ctx context.Context, egressID string, durationMs *int) error {
	return db.execOne(ctx, "complete call recording",
		`update call_recordings set duration_ms = $2 where egress_id = $1`, egressID, durationMs)
}

func (db *DB) ListRecordingsBefore(ctx context.Context, cutoff time.Time) ([]CallRecording, error) {
	return db.queryRecordings(ctx, "list recordings before", `select `+recordingColumns+`
		from call_recordings where created_at < $1 order by created_at`, cutoff)
}

func (db *DB) DeleteCallRecording(ctx context.Context, id uuid.UUID) error {
	if _, err := db.Pool.Exec(ctx, `delete from call_recordings where id = $1`, id); err != nil {
		return fmt.Errorf("delete call recording: %w", mapErr(err))
	}
	return nil
}

// call_recordings carries no size column, so there is no sum to take here.
func (db *DB) TotalRecordingBytes(ctx context.Context) (int64, error) {
	return 0, nil
}
