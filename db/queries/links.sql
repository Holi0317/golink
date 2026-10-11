-- Queries for the links table. Generated Go code lives in server/db
-- (golink-1yx); run `pitchfork start gen-sqlc` (or `mise run gen:sqlc`)
-- after editing this file.
--
-- Redirect resolution is by slug; edits and deletes target the id, since the
-- slug is what gets published and should not be a edit handle.

-- name: GetLink :one
select * from links where slug = $1;

-- name: GetLinkByID :one
select * from links where id = $1;

-- name: ListLinks :many
select * from links order by slug;

-- name: CreateLink :one
insert into links (slug, target_url, default_url, created_by)
values ($1, $2, $3, $4)
returning *;

-- name: UpdateLink :one
update links
set target_url = $2, default_url = $3, updated_at = now()
where id = $1
returning *;

-- name: DeleteLink :execrows
delete from links where id = $1;
