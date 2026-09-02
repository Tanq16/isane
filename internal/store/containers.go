package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const containerCols = `id, kind::text, slug, name, topic, last_seq, created_by, created_at, archived_at`

const containerVisibleSQL = `(
	(c.kind = 'channel' and c.archived_at is null)
	or exists (select 1 from conversation_participants p where p.container_id = c.id and p.user_id = $1)
)`

const containerViewSQL = `
with visible as (
	select c.id, c.kind::text as kind, c.slug, c.name, c.topic, c.last_seq, c.created_by, c.created_at, c.archived_at
	from containers c
	where c.kind = 'channel' and ($2::boolean or c.archived_at is null)
	union all
	select c.id, c.kind::text, c.slug, c.name, c.topic, c.last_seq, c.created_by, c.created_at, c.archived_at
	from containers c
	join conversation_participants p on p.container_id = c.id and p.user_id = $1
	where c.kind = 'conversation'
)
select v.id, v.kind, v.slug, v.name, v.topic, v.last_seq, v.created_by, v.created_at, v.archived_at,
	coalesce(r.last_read_seq, 0) as last_read_seq,
	(select count(*) from messages m
		where m.container_id = v.id
		  and m.thread_root_id is null
		  and m.seq > coalesce(r.last_read_seq, 0)
		  and m.deleted_at is null) as unread,
	mc.n as mentions,
	coalesce(np.level::text, case when v.kind = 'conversation' then 'all' else 'mentions' end) as level,
	coalesce(pp.ids, '{}'::uuid[]) as participants
from visible v
left join read_markers r on r.container_id = v.id and r.user_id = $1
left join notification_prefs np on np.container_id = v.id and np.user_id = $1
left join lateral (
	select count(*) as n
	from mentions m
	join messages msg on msg.id = m.message_id
	where m.user_id = $1
	  and msg.container_id = v.id
	  and msg.seq > coalesce(r.last_read_seq, 0)
	  and msg.deleted_at is null
) mc on true
left join lateral (
	select array_agg(p.user_id order by p.added_at) as ids
	from conversation_participants p
	where p.container_id = v.id
) pp on true
where $3::uuid is null or v.id = $3
order by v.kind, v.slug, v.created_at`

func scanContainer(row pgx.Row) (Container, error) {
	var c Container
	err := row.Scan(&c.ID, &c.Kind, &c.Slug, &c.Name, &c.Topic, &c.LastSeq, &c.CreatedBy, &c.CreatedAt, &c.ArchivedAt)
	return c, err
}

func scanContainerView(row pgx.Row) (ContainerView, error) {
	var v ContainerView
	err := row.Scan(&v.ID, &v.Kind, &v.Slug, &v.Name, &v.Topic, &v.LastSeq, &v.CreatedBy, &v.CreatedAt, &v.ArchivedAt,
		&v.LastReadSeq, &v.Unread, &v.Mentions, &v.Level, &v.Participants)
	return v, err
}

func (db *DB) CreateChannel(ctx context.Context, slug, name string, topic *string, createdBy uuid.UUID) (Container, error) {
	row := db.Pool.QueryRow(ctx, `insert into containers (id, kind, slug, name, topic, created_by)
		values ($1, 'channel', $2, $3, $4, $5) returning `+containerCols,
		uuid.New(), slug, name, topic, createdBy)
	c, err := scanContainer(row)
	if err != nil {
		return Container{}, fmt.Errorf("insert channel: %w", mapErr(err))
	}
	return c, nil
}

func (db *DB) GetContainer(ctx context.Context, id uuid.UUID) (Container, error) {
	c, err := scanContainer(db.Pool.QueryRow(ctx, `select `+containerCols+` from containers where id = $1`, id))
	if err != nil {
		return Container{}, fmt.Errorf("get container: %w", mapErr(err))
	}
	return c, nil
}

func (db *DB) GetChannelBySlug(ctx context.Context, slug string) (Container, error) {
	c, err := scanContainer(db.Pool.QueryRow(ctx,
		`select `+containerCols+` from containers where kind = 'channel' and slug = $1`, slug))
	if err != nil {
		return Container{}, fmt.Errorf("get channel: %w", mapErr(err))
	}
	return c, nil
}

func (db *DB) UpdateChannel(ctx context.Context, id uuid.UUID, name string, topic *string) error {
	tag, err := db.Pool.Exec(ctx,
		`update containers set name = $2, topic = $3 where id = $1 and kind = 'channel'`, id, name, topic)
	if err != nil {
		return fmt.Errorf("update channel: %w", mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update channel: %w", ErrNotFound)
	}
	return nil
}

func (db *DB) ArchiveChannel(ctx context.Context, id uuid.UUID) error {
	return db.setChannelArchived(ctx, id, `coalesce(archived_at, now())`)
}

func (db *DB) UnarchiveChannel(ctx context.Context, id uuid.UUID) error {
	return db.setChannelArchived(ctx, id, `null`)
}

func (db *DB) setChannelArchived(ctx context.Context, id uuid.UUID, value string) error {
	tag, err := db.Pool.Exec(ctx,
		`update containers set archived_at = `+value+` where id = $1 and kind = 'channel'`, id)
	if err != nil {
		return fmt.Errorf("archive channel: %w", mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("archive channel: %w", ErrNotFound)
	}
	return nil
}

func (db *DB) ListChannels(ctx context.Context, includeArchived bool) ([]Container, error) {
	rows, err := db.Pool.Query(ctx, `select `+containerCols+` from containers
		where kind = 'channel' and ($1::boolean or archived_at is null) order by slug`, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", mapErr(err))
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Container, error) { return scanContainer(r) })
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return list, nil
}

func (db *DB) ListConversationsForUser(ctx context.Context, userID uuid.UUID) ([]Container, error) {
	rows, err := db.Pool.Query(ctx, `select `+containerCols+` from containers
		join conversation_participants on conversation_participants.container_id = containers.id
		where conversation_participants.user_id = $1 and containers.kind = 'conversation'
		order by containers.created_at desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", mapErr(err))
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Container, error) { return scanContainer(r) })
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return list, nil
}

func (db *DB) ConversationParticipants(ctx context.Context, containerID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx,
		`select user_id from conversation_participants where container_id = $1 order by added_at`, containerID)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", mapErr(err))
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	return ids, nil
}

func (db *DB) GetOrCreateConversation(ctx context.Context, participants []uuid.UUID, createdBy uuid.UUID) (Container, error) {
	ids := normalizeParticipants(participants)
	if len(ids) == 0 {
		return Container{}, errors.New("create conversation: no participants")
	}
	key := conversationKey(ids)

	existing, err := db.conversationByKey(ctx, key)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Container{}, err
	}

	var created Container
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		c, err := scanContainer(tx.QueryRow(ctx, `insert into containers (id, kind, created_by)
			values ($1, 'conversation', $2) returning `+containerCols, uuid.New(), createdBy))
		if err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `insert into conversation_participants (container_id, user_id)
			select $1::uuid, t.u from unnest($2::uuid[]) as t(u)`, c.ID, ids); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `insert into conversation_keys (participant_key, container_id)
			values ($1, $2)`, key, c.ID); err != nil {
			return mapErr(err)
		}
		created = c
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return db.conversationByKey(ctx, key)
		}
		return Container{}, fmt.Errorf("create conversation: %w", err)
	}
	return created, nil
}

func (db *DB) conversationByKey(ctx context.Context, key string) (Container, error) {
	c, err := scanContainer(db.Pool.QueryRow(ctx, `select `+containerCols+` from containers
		where id = (select container_id from conversation_keys where participant_key = $1)`, key))
	if err != nil {
		return Container{}, fmt.Errorf("get conversation: %w", mapErr(err))
	}
	return c, nil
}

func normalizeParticipants(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return cmp.Compare(a.String(), b.String()) })
	return slices.Compact(out)
}

func conversationKey(sorted []uuid.UUID) string {
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = id.String()
	}
	return strings.Join(parts, ",")
}

func (db *DB) IsMember(ctx context.Context, containerID, userID uuid.UUID) (bool, error) {
	var member bool
	err := db.Pool.QueryRow(ctx, `select case when c.kind = 'channel'
			then exists (select 1 from users u where u.id = $2 and u.kind = 'human' and u.deactivated_at is null)
			else exists (select 1 from conversation_participants p where p.container_id = c.id and p.user_id = $2)
		end
		from containers c where c.id = $1`, containerID, userID).Scan(&member)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", mapErr(err))
	}
	return member, nil
}

func (db *DB) MemberIDs(ctx context.Context, containerID uuid.UUID) ([]uuid.UUID, error) {
	c, err := db.GetContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	var rows pgx.Rows
	if c.Kind == ContainerChannel {
		rows, err = db.Pool.Query(ctx,
			`select id from users where kind = 'human' and deactivated_at is null order by handle`)
	} else {
		rows, err = db.Pool.Query(ctx,
			`select user_id from conversation_participants where container_id = $1 order by added_at`, containerID)
	}
	if err != nil {
		return nil, fmt.Errorf("list members: %w", mapErr(err))
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return ids, nil
}

func (db *DB) ContainerViews(ctx context.Context, userID uuid.UUID, includeArchived bool) ([]ContainerView, error) {
	rows, err := db.Pool.Query(ctx, containerViewSQL, userID, includeArchived, nil)
	if err != nil {
		return nil, fmt.Errorf("list container views: %w", mapErr(err))
	}
	views, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ContainerView, error) { return scanContainerView(r) })
	if err != nil {
		return nil, fmt.Errorf("list container views: %w", err)
	}
	return views, nil
}

func (db *DB) ContainerView(ctx context.Context, userID, containerID uuid.UUID) (ContainerView, error) {
	rows, err := db.Pool.Query(ctx, containerViewSQL, userID, true, containerID)
	if err != nil {
		return ContainerView{}, fmt.Errorf("get container view: %w", mapErr(err))
	}
	v, err := pgx.CollectOneRow(rows, func(r pgx.CollectableRow) (ContainerView, error) { return scanContainerView(r) })
	if err != nil {
		return ContainerView{}, fmt.Errorf("get container view: %w", mapErr(err))
	}
	return v, nil
}

func (db *DB) CountContainers(ctx context.Context) (channels, conversations int64, err error) {
	err = db.Pool.QueryRow(ctx, `select
		count(*) filter (where kind = 'channel'),
		count(*) filter (where kind = 'conversation')
		from containers`).Scan(&channels, &conversations)
	if err != nil {
		return 0, 0, fmt.Errorf("count containers: %w", mapErr(err))
	}
	return channels, conversations, nil
}
