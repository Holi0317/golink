-- Queries for the links table. Generated Go code lives in server/db
-- (golink-1yx); run `mise run sqlc-generate` after editing this file.

-- name: GetLink :one
select * from links where slug = $1;

-- name: ListLinks :many
select * from links order by slug;

-- name: CreateLink :one
insert into links (slug, target_url, created_by)
values ($1, $2, $3)
returning *;

-- name: UpdateLink :one
update links
set target_url = $2, updated_at = now()
where slug = $1
returning *;

-- name: DeleteLink :execrows
delete from links where slug = $1;
