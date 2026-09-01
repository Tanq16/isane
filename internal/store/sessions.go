package store

import (
	"context"
	"fmt"
	"time"
	"uuid"
)

func (db *DB) CreateSession(ctx context.Context, s Session) error {
	_, err := db.Pool.Exec(ctx, `insert into sessions (id, token_hash, user_id, expires_at, user_agent, ip)
		values ($1, $2, $3, $4, $5, $6)`, s.ID, s.TokenHash, s.UserID, s.ExpiresAt, s.UserAgent, s.IP)
	if err != nil {
		return fmt.Errorf("create session: %w", mapErr(err))
	}
	return nil
}

func (db *DB) SessionByTokenHash(ctx context.Context, hash []byte) (Session, User, error) {
	var s Session
	var u User
	err := db.Pool.QueryRow(ctx, `select s.id, s.token_hash, s.user_id, s.created_at, s.last_seen_at,
			s.expires_at, s.user_agent, s.ip,
			u.id, u.kind, u.handle, u.display_name, u.avatar_id, u.email, u.password_hash,
			u.is_admin, u.created_at, u.deactivated_at
		from sessions s join users u on u.id = s.user_id
		where s.token_hash = $1 and s.expires_at > now()`, hash).
		Scan(&s.ID, &s.TokenHash, &s.UserID, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.UserAgent, &s.IP,
			&u.ID, &u.Kind, &u.Handle, &u.DisplayName, &u.AvatarID, &u.Email, &u.PasswordHash,
			&u.IsAdmin, &u.CreatedAt, &u.DeactivatedAt)
	if err != nil {
		return Session{}, User{}, fmt.Errorf("session by token hash: %w", mapErr(err))
	}
	return s, u, nil
}

func (db *DB) TouchSession(ctx context.Context, id uuid.UUID, expiresAt time.Time) error {
	return db.execOne(ctx, "touch session",
		`update sessions set expires_at = $2, last_seen_at = now() where id = $1`, id, expiresAt)
}

func (db *DB) DeleteSession(ctx context.Context, id uuid.UUID) error {
	return db.execOne(ctx, "delete session", `delete from sessions where id = $1`, id)
}

func (db *DB) DeleteSessionsForUser(ctx context.Context, userID uuid.UUID) error {
	if _, err := db.Pool.Exec(ctx, `delete from sessions where user_id = $1`, userID); err != nil {
		return fmt.Errorf("delete sessions for user: %w", err)
	}
	return nil
}

func (db *DB) CountActiveSessions(ctx context.Context) (int64, error) {
	var n int64
	if err := db.Pool.QueryRow(ctx, `select count(*) from sessions where expires_at > now()`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count active sessions: %w", err)
	}
	return n, nil
}

func (db *DB) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `delete from sessions where expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
