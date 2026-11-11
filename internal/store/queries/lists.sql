-- name: GetList :one
SELECT * FROM lists WHERE id = ?;

-- name: ListBoardLists :many
SELECT * FROM lists WHERE board_id = sqlc.arg('board_id') AND (CAST(sqlc.arg('include_archived') AS BOOLEAN) OR archived_at IS NULL) ORDER BY position, id;

-- name: CreateList :one
INSERT INTO lists (id, board_id, name, position, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateList :one
UPDATE lists SET name = ?, archived_at = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;

-- name: MoveList :one
UPDATE lists SET position = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;

-- name: SetListPosition :exec
UPDATE lists SET position = ? WHERE id = ?;

-- name: LastListPosition :one
SELECT CAST(coalesce(max(position), '') AS TEXT) FROM lists WHERE board_id = ?;

-- name: FirstListPosition :one
SELECT CAST(coalesce(min(position), '') AS TEXT) FROM lists WHERE board_id = ?;

-- name: ListsWithLongPositions :many
SELECT DISTINCT board_id FROM lists WHERE length(position) > CAST(sqlc.arg('max_len') AS INTEGER);
