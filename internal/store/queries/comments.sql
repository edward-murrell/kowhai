-- name: GetComment :one
SELECT * FROM comments WHERE id = ?;

-- name: ListCardComments :many
SELECT * FROM comments
WHERE card_id = sqlc.arg('card_id') AND deleted_at IS NULL
  AND (CAST(sqlc.arg('before') AS TEXT) = '' OR id < sqlc.arg('before'))
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: ListAllCardComments :many
SELECT * FROM comments WHERE card_id = ? AND deleted_at IS NULL ORDER BY id;

-- name: CreateComment :one
INSERT INTO comments (id, card_id, author_id, token_id, actor_label, body, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateComment :one
UPDATE comments SET body = ?, edited_at = ?, deleted_at = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;
