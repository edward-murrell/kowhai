-- name: GetCardLink :one
SELECT * FROM card_links WHERE id = ?;

-- name: GetCardLinkByKey :one
SELECT * FROM card_links WHERE kind = ? AND external_key = ?;

-- name: ListCardLinks :many
SELECT * FROM card_links WHERE card_id = ? ORDER BY created_at, id;

-- name: ListBoardLinks :many
SELECT * FROM card_links WHERE board_id = ? AND (status != 'ended' OR updated_at > ?) ORDER BY created_at, id;

-- name: CreateCardLink :one
INSERT INTO card_links (id, card_id, board_id, kind, external_key, title, repo, branch, host, status, status_detail, last_seen_at, meta, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateCardLink :one
UPDATE card_links SET card_id = ?, board_id = ?, title = ?, repo = ?, branch = ?, host = ?, status = ?, status_detail = ?, last_seen_at = ?, meta = ?,
    updated_at = ?, version = version + 1
WHERE id = ?
RETURNING *;

-- name: DeleteCardLink :exec
DELETE FROM card_links WHERE id = ?;

-- name: ListStaleLinks :many
SELECT * FROM card_links WHERE status IN ('working', 'waiting') AND last_seen_at < ?;
