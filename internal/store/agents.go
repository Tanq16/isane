package store

import (
	"context"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const agentColumns = `user_id, state, claim_token_hash, allow_history, argv, registered_at, last_seen_at`

const agentInfoColumns = `u.id, u.kind, u.handle, u.display_name, u.avatar_id, u.email,
	u.password_hash, u.is_admin, u.created_at, u.deactivated_at,
	a.user_id, a.state, a.claim_token_hash, a.allow_history, a.argv, a.registered_at, a.last_seen_at`

const agentInfoFrom = ` from agents a join users u on u.id = a.user_id`

const agentJobColumns = `id, agent_id, container_id, trigger_msg_id, state, prompt, result, error,
	created_at, dispatched_at, finished_at`

func scanAgent(row pgx.Row) (Agent, error) {
	var a Agent
	err := row.Scan(&a.UserID, &a.State, &a.ClaimTokenHash, &a.AllowHistory, &a.Argv,
		&a.RegisteredAt, &a.LastSeenAt)
	return a, err
}

func scanAgentInfo(row pgx.Row) (AgentInfo, error) {
	var i AgentInfo
	err := row.Scan(&i.User.ID, &i.User.Kind, &i.User.Handle, &i.User.DisplayName, &i.User.AvatarID,
		&i.User.Email, &i.User.PasswordHash, &i.User.IsAdmin, &i.User.CreatedAt, &i.User.DeactivatedAt,
		&i.Agent.UserID, &i.Agent.State, &i.Agent.ClaimTokenHash, &i.Agent.AllowHistory, &i.Agent.Argv,
		&i.Agent.RegisteredAt, &i.Agent.LastSeenAt)
	return i, err
}

func scanAgentJob(row pgx.Row) (AgentJob, error) {
	var j AgentJob
	err := row.Scan(&j.ID, &j.AgentID, &j.ContainerID, &j.TriggerMsgID, &j.State, &j.Prompt,
		&j.Result, &j.Error, &j.CreatedAt, &j.DispatchedAt, &j.FinishedAt)
	return j, err
}

func (db *DB) queryAgentJobs(ctx context.Context, op, sql string, args ...any) ([]AgentJob, error) {
	rows, err := db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, mapErr(err))
	}
	jobs, err := pgx.CollectRows(rows,
		func(r pgx.CollectableRow) (AgentJob, error) { return scanAgentJob(r) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return jobs, nil
}

func (db *DB) ReserveAgent(ctx context.Context, handle, displayName string, claimTokenHash []byte) (AgentInfo, error) {
	var info AgentInfo
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		u, err := insertUser(ctx, tx.QueryRow, User{
			Kind:        UserAgent,
			Handle:      handle,
			DisplayName: displayName,
		})
		if err != nil {
			return err
		}
		a := Agent{UserID: u.ID, State: AgentReserved, ClaimTokenHash: claimTokenHash}
		if _, err := tx.Exec(ctx, `insert into agents (user_id, state, claim_token_hash)
			values ($1, $2, $3)`, a.UserID, a.State, a.ClaimTokenHash); err != nil {
			return mapErr(err)
		}
		info = AgentInfo{User: u, Agent: a}
		return nil
	})
	if err != nil {
		return AgentInfo{}, fmt.Errorf("reserve agent: %w", err)
	}
	return info, nil
}

func (db *DB) GetAgent(ctx context.Context, userID uuid.UUID) (Agent, error) {
	a, err := scanAgent(db.Pool.QueryRow(ctx,
		`select `+agentColumns+` from agents where user_id = $1`, userID))
	if err != nil {
		return Agent{}, fmt.Errorf("get agent: %w", mapErr(err))
	}
	return a, nil
}

func (db *DB) AgentByHandle(ctx context.Context, handle string) (AgentInfo, error) {
	info, err := scanAgentInfo(db.Pool.QueryRow(ctx,
		`select `+agentInfoColumns+agentInfoFrom+` where u.handle = $1`, handle))
	if err != nil {
		return AgentInfo{}, fmt.Errorf("get agent by handle: %w", mapErr(err))
	}
	return info, nil
}

func (db *DB) ListAgents(ctx context.Context) ([]AgentInfo, error) {
	rows, err := db.Pool.Query(ctx, `select `+agentInfoColumns+agentInfoFrom+` order by u.handle`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", mapErr(err))
	}
	list, err := pgx.CollectRows(rows,
		func(r pgx.CollectableRow) (AgentInfo, error) { return scanAgentInfo(r) })
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return list, nil
}

func (db *DB) RegisterAgent(ctx context.Context, userID uuid.UUID, argv []string, allowHistory bool) error {
	return db.execOne(ctx, "register agent", `update agents
		set state = $2, argv = $3, allow_history = $4, registered_at = now(), last_seen_at = now()
		where user_id = $1`, userID, AgentServing, argv, allowHistory)
}

func (db *DB) DeregisterAgent(ctx context.Context, userID uuid.UUID) error {
	return db.execOne(ctx, "deregister agent",
		`update agents set state = $2, registered_at = null where user_id = $1`,
		userID, AgentReserved)
}

func (db *DB) TouchAgent(ctx context.Context, userID uuid.UUID) error {
	return db.execOne(ctx, "touch agent",
		`update agents set last_seen_at = now() where user_id = $1`, userID)
}

func (db *DB) DeleteAgent(ctx context.Context, userID uuid.UUID) (bool, error) {
	var hardDeleted bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var posted bool
		err := tx.QueryRow(ctx,
			`select exists(select 1 from messages where author_id = $1)`, userID).Scan(&posted)
		if err != nil {
			return mapErr(err)
		}
		tag, err := tx.Exec(ctx, `delete from agents where user_id = $1`, userID)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if posted {
			_, err = tx.Exec(ctx, `update users set deactivated_at = now() where id = $1`, userID)
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `delete from users where id = $1`, userID); err != nil {
			return mapErr(err)
		}
		hardDeleted = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("delete agent: %w", err)
	}
	return hardDeleted, nil
}

func (db *DB) CountAgents(ctx context.Context) (int64, error) {
	var n int64
	if err := db.Pool.QueryRow(ctx, `select count(*) from agents`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count agents: %w", err)
	}
	return n, nil
}

func (db *DB) CreateAgentJob(ctx context.Context, j AgentJob, attachmentIDs []uuid.UUID) (AgentJob, error) {
	if j.ID == uuid.Nil() {
		j.ID = uuid.New()
	}
	var out AgentJob
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		created, err := scanAgentJob(tx.QueryRow(ctx, `insert into agent_jobs
			(id, agent_id, container_id, trigger_msg_id, state, prompt, result, error)
			values ($1, $2, $3, $4, $5, $6, $7, $8)
			returning `+agentJobColumns,
			j.ID, j.AgentID, j.ContainerID, j.TriggerMsgID, j.State, j.Prompt, j.Result, j.Error))
		if err != nil {
			return mapErr(err)
		}
		out = created
		if len(attachmentIDs) == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `insert into agent_job_attachments (job_id, attachment_id)
			select $1, unnest($2::uuid[]) on conflict do nothing`, j.ID, attachmentIDs)
		return mapErr(err)
	})
	if err != nil {
		return AgentJob{}, fmt.Errorf("create agent job: %w", err)
	}
	return out, nil
}

func (db *DB) GetAgentJob(ctx context.Context, id uuid.UUID) (AgentJob, error) {
	j, err := scanAgentJob(db.Pool.QueryRow(ctx,
		`select `+agentJobColumns+` from agent_jobs where id = $1`, id))
	if err != nil {
		return AgentJob{}, fmt.Errorf("get agent job: %w", mapErr(err))
	}
	return j, nil
}

func (db *DB) HasUnfinishedJob(ctx context.Context, agentID, containerID uuid.UUID) (bool, error) {
	var found bool
	err := db.Pool.QueryRow(ctx, `select exists(select 1 from agent_jobs
		where agent_id = $1 and container_id = $2 and state in ('queued', 'dispatched'))`,
		agentID, containerID).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("check unfinished job: %w", err)
	}
	return found, nil
}

func (db *DB) ClaimQueuedJobs(ctx context.Context, agentID uuid.UUID, limit int) ([]AgentJob, error) {
	jobs, err := db.queryAgentJobs(ctx, "claim queued jobs", `with claimed as (
			select id as claimed_id from agent_jobs
			where agent_id = $1 and state = 'queued'
			order by created_at
			for update skip locked
			limit $2
		)
		update agent_jobs set state = 'dispatched', dispatched_at = now()
		from claimed
		where agent_jobs.id = claimed.claimed_id
		returning `+agentJobColumns, agentID, limit)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(jobs, func(a, b AgentJob) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return jobs, nil
}

func (db *DB) FinishAgentJob(ctx context.Context, id uuid.UUID, state AgentJobState, result, errText *string) (AgentJob, error) {
	j, err := scanAgentJob(db.Pool.QueryRow(ctx, `update agent_jobs
		set state = $2, result = $3, error = $4, finished_at = now()
		where id = $1
		returning `+agentJobColumns, id, state, result, errText))
	if err != nil {
		return AgentJob{}, fmt.Errorf("finish agent job: %w", mapErr(err))
	}
	return j, nil
}

func (db *DB) ExpireStaleJobs(ctx context.Context, timeout time.Duration) ([]AgentJob, error) {
	return db.queryAgentJobs(ctx, "expire stale jobs", `update agent_jobs
		set state = 'timeout', finished_at = now(), error = coalesce(error, 'job timed out')
		where (state = 'dispatched' and dispatched_at < $1)
		   or (state = 'queued' and created_at < $1)
		returning `+agentJobColumns, time.Now().Add(-timeout))
}

func (db *DB) JobAllowsAttachment(ctx context.Context, jobID, attachmentID uuid.UUID) (bool, error) {
	var allowed bool
	err := db.Pool.QueryRow(ctx, `select exists(select 1 from agent_job_attachments
		where job_id = $1 and attachment_id = $2)`, jobID, attachmentID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check job attachment: %w", err)
	}
	return allowed, nil
}
