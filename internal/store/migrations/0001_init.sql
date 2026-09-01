create type user_kind as enum ('human', 'agent');
create type container_kind as enum ('channel', 'conversation');
create type notification_level as enum ('all', 'mentions', 'none');
create type thread_sub_state as enum ('subscribed', 'muted');
create type attachment_kind as enum ('image', 'video', 'audio', 'file');
create type attachment_state as enum ('staged', 'processing', 'ready', 'failed');
create type agent_state as enum ('reserved', 'serving');
create type agent_job_state as enum ('queued', 'dispatched', 'done', 'failed', 'timeout');
create type recording_state as enum ('off', 'recording', 'processing', 'ready', 'failed');

create table users (
    id             uuid primary key,
    kind           user_kind   not null,
    handle         text        not null unique,
    display_name   text        not null,
    avatar_id      uuid        null,
    email          text        null unique,
    password_hash  text        null,
    is_admin       boolean     not null default false,
    created_at     timestamptz not null default now(),
    deactivated_at timestamptz null,
    constraint users_handle_format check (handle ~ '^[a-z0-9][a-z0-9_-]{0,31}$')
);

create table sessions (
    id           uuid  primary key,
    token_hash   bytea not null unique,
    user_id      uuid  not null references users(id) on delete cascade,
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    expires_at   timestamptz not null,
    user_agent   text  null,
    ip           inet  null
);
create index sessions_user_id_idx on sessions (user_id);
create index sessions_expires_at_idx on sessions (expires_at);

create table invites (
    token_hash bytea primary key,
    created_by uuid  null references users(id),
    note       text  null,
    expires_at timestamptz not null,
    used_by    uuid  null references users(id),
    used_at    timestamptz null,
    created_at timestamptz not null default now()
);

create table containers (
    id          uuid primary key,
    kind        container_kind not null,
    slug        text null unique,
    name        text null,
    topic       text null,
    last_seq    bigint not null default 0,
    created_by  uuid not null references users(id),
    created_at  timestamptz not null default now(),
    archived_at timestamptz null,
    constraint containers_slug_format check (slug is null or slug ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
    constraint containers_channel_shape check (
        (kind = 'channel' and slug is not null and name is not null)
        or (kind = 'conversation' and slug is null and name is null)
    )
);

create table conversation_participants (
    container_id uuid not null references containers(id) on delete cascade,
    user_id      uuid not null references users(id),
    added_at     timestamptz not null default now(),
    primary key (container_id, user_id)
);
create index conversation_participants_user_idx on conversation_participants (user_id);

create table conversation_keys (
    participant_key text primary key,
    container_id    uuid not null references containers(id) on delete cascade
);

create table messages (
    id                   uuid   primary key,
    container_id         uuid   not null references containers(id) on delete cascade,
    seq                  bigint not null,
    author_id            uuid   not null references users(id),
    body                 text   not null,
    reply_to_id          uuid   null references messages(id),
    thread_root_id       uuid   null references messages(id),
    client_id            text   not null,
    is_system            boolean not null default false,
    thread_reply_count   int    not null default 0,
    thread_last_reply_at timestamptz null,
    search_tsv           tsvector generated always as (to_tsvector('english', body)) stored,
    created_at           timestamptz not null default now(),
    edited_at            timestamptz null,
    deleted_at           timestamptz null,
    unique (container_id, seq),
    unique (author_id, client_id)
);
create index messages_timeline_idx on messages (container_id, seq desc) where thread_root_id is null;
create index messages_thread_idx on messages (thread_root_id, seq);
create index messages_search_idx on messages using gin (search_tsv);
create index messages_created_at_idx on messages (created_at);

create table mentions (
    message_id uuid not null references messages(id) on delete cascade,
    user_id    uuid not null references users(id) on delete cascade,
    primary key (message_id, user_id)
);
create index mentions_user_idx on mentions (user_id, message_id);

create table read_markers (
    user_id       uuid   not null references users(id) on delete cascade,
    container_id  uuid   not null references containers(id) on delete cascade,
    last_read_seq bigint not null,
    updated_at    timestamptz not null default now(),
    primary key (user_id, container_id)
);

create table notification_prefs (
    user_id      uuid not null references users(id) on delete cascade,
    container_id uuid not null references containers(id) on delete cascade,
    level        notification_level not null,
    primary key (user_id, container_id)
);

create table thread_subscriptions (
    user_id        uuid not null references users(id) on delete cascade,
    thread_root_id uuid not null references messages(id) on delete cascade,
    state          thread_sub_state not null,
    primary key (user_id, thread_root_id)
);

create table attachments (
    id            uuid primary key,
    message_id    uuid   null references messages(id) on delete set null,
    uploader_id   uuid   not null references users(id),
    kind          attachment_kind  not null,
    original_name text   not null,
    mime          text   not null,
    size_bytes    bigint not null,
    width         int    null,
    height        int    null,
    duration_ms   int    null,
    storage_path  text   not null,
    thumb_path    text   null,
    state         attachment_state not null,
    error         text   null,
    created_at    timestamptz not null default now()
);
create index attachments_staged_idx on attachments (state, created_at) where message_id is null;
create index attachments_message_idx on attachments (message_id);

alter table users add constraint users_avatar_fk foreign key (avatar_id) references attachments(id) on delete set null;

create table agents (
    user_id          uuid primary key references users(id) on delete cascade,
    state            agent_state not null,
    claim_token_hash bytea   not null,
    allow_history    boolean not null default false,
    argv             text[]  null,
    registered_at    timestamptz null,
    last_seen_at     timestamptz null
);

create table agent_jobs (
    id             uuid primary key,
    agent_id       uuid not null references users(id) on delete cascade,
    container_id   uuid not null references containers(id) on delete cascade,
    trigger_msg_id uuid not null references messages(id) on delete cascade,
    state          agent_job_state not null,
    prompt         text not null,
    result         text null,
    error          text null,
    created_at     timestamptz not null default now(),
    dispatched_at  timestamptz null,
    finished_at    timestamptz null
);
create index agent_jobs_pending_idx on agent_jobs (agent_id, state) where state in ('queued', 'dispatched');
create index agent_jobs_container_idx on agent_jobs (container_id, agent_id);

create table agent_job_attachments (
    job_id        uuid not null references agent_jobs(id) on delete cascade,
    attachment_id uuid not null references attachments(id) on delete cascade,
    primary key (job_id, attachment_id)
);

create table calls (
    id              uuid primary key,
    container_id    uuid not null references containers(id) on delete cascade,
    room_name       text not null unique,
    started_by      uuid not null references users(id),
    started_at      timestamptz not null default now(),
    ended_at        timestamptz null,
    recording_state recording_state not null default 'off'
);
create unique index calls_one_live_per_container on calls (container_id) where ended_at is null;

create table call_participants (
    call_id   uuid not null references calls(id) on delete cascade,
    user_id   uuid not null references users(id),
    joined_at timestamptz not null default now(),
    left_at   timestamptz null,
    primary key (call_id, user_id, joined_at)
);
create index call_participants_live_idx on call_participants (call_id) where left_at is null;

create table call_recordings (
    id           uuid primary key,
    call_id      uuid not null references calls(id) on delete cascade,
    user_id      uuid null references users(id),
    egress_id    text null,
    storage_path text not null,
    size_bytes   bigint not null default 0,
    duration_ms  int  null,
    created_at   timestamptz not null default now()
);
create index call_recordings_call_idx on call_recordings (call_id);

create table push_subscriptions (
    id              uuid primary key,
    user_id         uuid not null references users(id) on delete cascade,
    endpoint        text not null unique,
    p256dh          text not null,
    auth            text not null,
    user_agent      text null,
    enabled         boolean not null default true,
    created_at      timestamptz not null default now(),
    last_success_at timestamptz null,
    last_failure_at timestamptz null
);
create index push_subscriptions_user_idx on push_subscriptions (user_id) where enabled;

create table server_meta (
    key        text primary key,
    value      text not null,
    updated_at timestamptz not null default now()
);
