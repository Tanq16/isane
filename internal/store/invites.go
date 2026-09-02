package store

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const inviteColumns = `token_hash, coalesce(created_by, '00000000-0000-0000-0000-000000000000'::uuid), note, expires_at, used_by, used_at, created_at`

func scanInvite(row pgx.Row) (Invite, error) {
	var inv Invite
	err := row.Scan(&inv.TokenHash, &inv.CreatedBy, &inv.Note, &inv.ExpiresAt, &inv.UsedBy, &inv.UsedAt, &inv.CreatedAt)
	return inv, err
}

func (db *DB) CreateInvite(ctx context.Context, inv Invite) error {
	_, err := db.Pool.Exec(ctx, `insert into invites (token_hash, created_by, note, expires_at)
		values ($1, $2, $3, $4)`, inv.TokenHash, nullUUID(inv.CreatedBy), inv.Note, inv.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create invite: %w", mapErr(err))
	}
	return nil
}

func (db *DB) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := db.Pool.Query(ctx, `select `+inviteColumns+` from invites order by created_at desc`)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	invites, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Invite, error) { return scanInvite(row) })
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	return invites, nil
}

func (db *DB) GetInvite(ctx context.Context, tokenHash []byte) (Invite, error) {
	inv, err := scanInvite(db.Pool.QueryRow(ctx, `select `+inviteColumns+` from invites where token_hash = $1`, tokenHash))
	if err != nil {
		return Invite{}, fmt.Errorf("get invite: %w", mapErr(err))
	}
	return inv, nil
}

func (db *DB) RevokeInvite(ctx context.Context, tokenHash []byte) error {
	return db.execOne(ctx, "revoke invite", `delete from invites where token_hash = $1`, tokenHash)
}

func (db *DB) DeleteExpiredInvites(ctx context.Context) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `delete from invites where expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("delete expired invites: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (db *DB) AcceptInvite(ctx context.Context, tokenHash []byte, u User) (User, error) {
	var created User
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var usedBy *uuid.UUID
		var expired bool
		err := tx.QueryRow(ctx, `select used_by, expires_at <= now() from invites
			where token_hash = $1 for update`, tokenHash).Scan(&usedBy, &expired)
		if err != nil {
			return fmt.Errorf("lock invite: %w", mapErr(err))
		}
		if usedBy != nil {
			return fmt.Errorf("invite already used: %w", ErrConflict)
		}
		if expired {
			return fmt.Errorf("invite expired: %w", ErrConflict)
		}
		var existing int64
		if err := tx.QueryRow(ctx, `select count(*) from users`).Scan(&existing); err != nil {
			return fmt.Errorf("count users: %w", mapErr(err))
		}
		u.IsAdmin = existing == 0
		created, err = insertUser(ctx, tx.QueryRow, u)
		if err != nil {
			return fmt.Errorf("insert user: %w", err)
		}
		_, err = tx.Exec(ctx, `update invites set used_by = $2, used_at = now() where token_hash = $1`,
			tokenHash, created.ID)
		if err != nil {
			return fmt.Errorf("consume invite: %w", err)
		}
		return nil
	})
	if err != nil {
		return User{}, fmt.Errorf("accept invite: %w", err)
	}
	return created, nil
}

func nullUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil() {
		return nil
	}
	return &id
}
