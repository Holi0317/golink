-- migrate:up
create table if not exists links (
  slug       text primary key,
  target_url text        not null,
  created_by text        not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

-- migrate:down
drop table if exists links;
