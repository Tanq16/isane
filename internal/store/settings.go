package store

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const serverSettingsCols = `allow_member_channels, updated_at, updated_by`

func scanServerSettings(row pgx.Row) (ServerSettings, error) {
	var s ServerSettings
	err := row.Scan(&s.AllowMemberChannels, &s.UpdatedAt, &s.UpdatedBy)
	return s, err
}

func (db *DB) ServerSettings(ctx context.Context) (ServerSettings, error) {
	s, err := scanServerSettings(db.Pool.QueryRow(ctx,
		`select `+serverSettingsCols+` from server_settings where id`))
	if err != nil {
		return ServerSettings{}, fmt.Errorf("get server settings: %w", mapErr(err))
	}
	return s, nil
}

func (db *DB) UpdateServerSettings(ctx context.Context, allowMemberChannels bool, actor uuid.UUID) (ServerSettings, error) {
	s, err := scanServerSettings(db.Pool.QueryRow(ctx, `update server_settings
		set allow_member_channels = $1, updated_at = now(), updated_by = $2
		where id returning `+serverSettingsCols, allowMemberChannels, nullUUID(actor)))
	if err != nil {
		return ServerSettings{}, fmt.Errorf("update server settings: %w", mapErr(err))
	}
	return s, nil
}
