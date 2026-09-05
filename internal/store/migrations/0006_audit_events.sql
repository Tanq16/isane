create table audit_events (
    id           uuid primary key,
    at           timestamptz not null default now(),
    actor_id     uuid null references users(id) on delete set null,
    actor_handle text not null,
    action       text not null,
    target_type  text null,
    target_id    uuid null,
    target_label text null,
    detail       jsonb null,
    ip           inet null,
    user_agent   text null
);
