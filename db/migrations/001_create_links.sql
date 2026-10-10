-- migrate:up
create table links (
  id         int generated always as identity primary key,
  target_url text        not null,
  created_by text        not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  slug       text        not null
);
-- Slugs are unique (and are what redirect service resolves by); every other
-- mutation addresses a row by its id.
create unique index links_slug_key on links (slug);

-- migrate:down
drop table links;
