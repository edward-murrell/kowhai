-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = ?;

-- name: ListCardAttachments :many
SELECT * FROM attachments WHERE card_id = ? ORDER BY id;

-- name: CreateAttachment :one
INSERT INTO attachments (id, card_id, board_id, uploader_id, object_key, filename, content_type, size_bytes, sha256, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = ?;

-- name: ListCardAttachmentKeys :many
SELECT object_key FROM attachments WHERE card_id = ?;

-- name: QueueBlobDeletion :exec
INSERT INTO blob_deletions (object_key, created_at) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: ListBlobDeletions :many
SELECT * FROM blob_deletions WHERE attempts < 20 ORDER BY created_at LIMIT ?;

-- name: DeleteBlobDeletion :exec
DELETE FROM blob_deletions WHERE object_key = ?;

-- name: BumpBlobDeletion :exec
UPDATE blob_deletions SET attempts = attempts + 1 WHERE object_key = ?;
