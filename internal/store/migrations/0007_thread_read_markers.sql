create table thread_read_markers (
    user_id        uuid not null references users(id) on delete cascade,
    thread_root_id uuid not null references messages(id) on delete cascade,
    last_read_seq  bigint not null default 0,
    updated_at     timestamptz not null default now(),
    primary key (user_id, thread_root_id)
);

insert into thread_subscriptions (user_id, thread_root_id, state)
select u, r, 'subscribed' from (
    select author_id as u, id as r from messages where thread_reply_count > 0
    union
    select x.user_id, x.message_id
    from mentions x join messages m on m.id = x.message_id
    where m.thread_reply_count > 0
    union
    select x.user_id, m.thread_root_id
    from mentions x join messages m on m.id = x.message_id
    where m.thread_root_id is not null
) s
on conflict do nothing;

insert into thread_read_markers (user_id, thread_root_id, last_read_seq)
select s.user_id, s.thread_root_id, max(m.seq)
from thread_subscriptions s join messages m on m.thread_root_id = s.thread_root_id
group by s.user_id, s.thread_root_id;
