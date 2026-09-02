create table server_settings (
    id                    boolean primary key default true,
    allow_member_channels boolean not null default true,
    updated_at            timestamptz not null default now(),
    updated_by            uuid null references users(id) on delete set null,
    constraint server_settings_singleton check (id)
);

insert into server_settings (id) values (true);
