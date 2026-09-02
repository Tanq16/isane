alter table messages add column recording_id uuid null references call_recordings(id) on delete set null;
create unique index messages_recording_idx on messages (recording_id) where recording_id is not null;

alter table call_recordings add column mime text not null default 'audio/ogg';
alter table call_recordings alter column mime drop default;
