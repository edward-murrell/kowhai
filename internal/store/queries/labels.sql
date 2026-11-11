-- name: GetLabel :one
SELECT * FROM labels WHERE id = ?;

-- name: ListBoardLabels :many
SELECT * FROM labels WHERE board_id = ? ORDER BY name, id;

-- name: CreateLabel :one
INSERT INTO labels (id, board_id, name, colour, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateLabel :one
UPDATE labels SET name = ?, colour = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;

-- name: DeleteLabel :exec
DELETE FROM labels WHERE id = ?;
