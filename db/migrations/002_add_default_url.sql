-- migrate:up
-- Template links (targets with {0}, {1}, ... fragment references) can carry
-- a default location: visiting the bare slug, e.g. go/jira, redirects there
-- instead of failing on a zero-fragment substitution. Empty means no
-- default; static links never consult it.
alter table links add column default_url text not null default '';

-- migrate:down
alter table links drop column default_url;
