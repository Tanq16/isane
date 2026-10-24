create table message_reactions (
    message_id uuid not null references messages(id) on delete cascade,
    user_id    uuid not null references users(id) on delete cascade,
    emoji      text not null check (char_length(emoji) between 1 and 32),
    created_at timestamptz not null default now(),
    primary key (message_id, user_id, emoji)
);
