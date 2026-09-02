package store

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const userColumns = `id, kind, handle, display_name, avatar_id, email, password_hash, is_admin, mark_read_on_open,
	created_at, deactivated_at`

const insertUserSQL = `insert into users (id, kind, handle, display_name, avatar_id, email, password_hash, is_admin)
	values ($1, $2, $3, $4, $5, $6, $7, $8)
	returning ` + userColumns

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Kind, &u.Handle, &u.DisplayName, &u.AvatarID, &u.Email,
		&u.PasswordHash, &u.IsAdmin, &u.MarkReadOnOpen, &u.CreatedAt, &u.DeactivatedAt)
	return u, err
}

func insertUser(ctx context.Context, queryRow func(context.Context, string, ...any) pgx.Row, u User) (User, error) {
	if u.ID == uuid.Nil() {
		u.ID = uuid.New()
	}
	out, err := scanUser(queryRow(ctx, insertUserSQL,
		u.ID, u.Kind, u.Handle, u.DisplayName, u.AvatarID, u.Email, u.PasswordHash, u.IsAdmin))
	if err != nil {
		return User{}, mapErr(err)
	}
	return out, nil
}

func (db *DB) execOne(ctx context.Context, op, sql string, args ...any) error {
	tag, err := db.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	return nil
}

func (db *DB) queryUsers(ctx context.Context, op, sql string, args ...any) ([]User, error) {
	rows, err := db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) { return scanUser(row) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return users, nil
}

func (db *DB) CreateUser(ctx context.Context, u User) (User, error) {
	out, err := insertUser(ctx, db.Pool.QueryRow, u)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return out, nil
}

func (db *DB) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := scanUser(db.Pool.QueryRow(ctx, `select `+userColumns+` from users where id = $1`, id))
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", mapErr(err))
	}
	return u, nil
}

func (db *DB) GetUserByHandle(ctx context.Context, handle string) (User, error) {
	u, err := scanUser(db.Pool.QueryRow(ctx, `select `+userColumns+` from users where handle = $1`, handle))
	if err != nil {
		return User{}, fmt.Errorf("get user by handle: %w", mapErr(err))
	}
	return u, nil
}

func (db *DB) GetUserByLogin(ctx context.Context, ident string) (User, error) {
	u, err := scanUser(db.Pool.QueryRow(ctx, `select `+userColumns+`
		from users where kind = 'human' and (handle = $1 or email = $1)`, ident))
	if err != nil {
		return User{}, fmt.Errorf("get user by login: %w", mapErr(err))
	}
	return u, nil
}

func (db *DB) ListUsers(ctx context.Context) ([]User, error) {
	return db.queryUsers(ctx, "list users", `select `+userColumns+` from users order by handle`)
}

func (db *DB) ListDirectory(ctx context.Context) ([]DirectoryUser, error) {
	users, err := db.queryUsers(ctx, "list directory", `select `+userColumns+` from users order by handle`)
	if err != nil {
		return nil, err
	}
	out := make([]DirectoryUser, len(users))
	for i, u := range users {
		out[i] = u.Directory()
	}
	return out, nil
}

func (db *DB) ListActiveHumans(ctx context.Context) ([]User, error) {
	return db.queryUsers(ctx, "list active humans", `select `+userColumns+`
		from users where kind = 'human' and deactivated_at is null order by handle`)
}

func (db *DB) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	if err := db.Pool.QueryRow(ctx, `select count(*) from users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func (db *DB) SetPassword(ctx context.Context, id uuid.UUID, hash string) error {
	return db.execOne(ctx, "set password", `update users set password_hash = $2 where id = $1`, id, hash)
}

func (db *DB) DeactivateUser(ctx context.Context, id uuid.UUID) error {
	return db.execOne(ctx, "deactivate user",
		`update users set deactivated_at = now(), password_hash = null where id = $1`, id)
}

func (db *DB) SetAdmin(ctx context.Context, id uuid.UUID, isAdmin bool) error {
	return db.execOne(ctx, "set admin", `update users set is_admin = $2 where id = $1`, id, isAdmin)
}

func (db *DB) UpdateProfile(ctx context.Context, id uuid.UUID, displayName string, avatarID *uuid.UUID) error {
	return db.execOne(ctx, "update profile",
		`update users set display_name = $2, avatar_id = $3 where id = $1`, id, displayName, avatarID)
}

func (db *DB) SetMarkReadOnOpen(ctx context.Context, id uuid.UUID, on bool) error {
	return db.execOne(ctx, "set mark read on open",
		`update users set mark_read_on_open = $2 where id = $1`, id, on)
}

func (db *DB) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return db.execOne(ctx, "delete user", `delete from users where id = $1`, id)
}

func (db *DB) UserHasMessages(ctx context.Context, id uuid.UUID) (bool, error) {
	var has bool
	err := db.Pool.QueryRow(ctx, `select exists(select 1 from messages where author_id = $1)`, id).Scan(&has)
	if err != nil {
		return false, fmt.Errorf("check user messages: %w", err)
	}
	return has, nil
}
