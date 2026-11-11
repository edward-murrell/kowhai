-- name: CreateActivity :one
INSERT INTO activities (id, workspace_id, board_id, card_id, actor_id, token_id, actor_label, type, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetActivity :one
SELECT * FROM activities WHERE id = ?;

-- name: ListBoardActivity :many
SELECT * FROM activities
WHERE board_id = sqlc.arg('board_id') AND (CAST(sqlc.arg('before') AS TEXT) = '' OR id < sqlc.arg('before'))
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: ListCardActivity :many
SELECT * FROM activities
WHERE card_id = sqlc.arg('card_id') AND (CAST(sqlc.arg('before') AS TEXT) = '' OR id < sqlc.arg('before'))
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: ListBoardActivitySince :many
SELECT * FROM activities WHERE board_id = ? AND id > ? ORDER BY id LIMIT ?;
