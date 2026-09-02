alter table messages add column call_id uuid null references calls(id) on delete set null;
