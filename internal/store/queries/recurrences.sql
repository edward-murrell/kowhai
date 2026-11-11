-- name: GetRecurrence :one
SELECT * FROM card_recurrences WHERE id = ?;

-- name: ListBoardRecurrences :many
SELECT * FROM card_recurrences WHERE board_id = ? ORDER BY created_at, id;

-- name: ListTemplateRecurrences :many
SELECT * FROM card_recurrences WHERE template_card_id = ?;

-- name: CreateRecurrence :one
INSERT INTO card_recurrences (id, board_id, template_card_id, target_list_id, rrule, dtstart, timezone, due_offset_minutes, next_run_at, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateRecurrence :one
UPDATE card_recurrences SET template_card_id = ?, target_list_id = ?, rrule = ?, dtstart = ?, timezone = ?, due_offset_minutes = ?,
    next_run_at = ?, last_run_at = ?, paused_at = ?, updated_at = ?, version = version + 1
WHERE id = ?
RETURNING *;

-- name: DeleteRecurrence :exec
DELETE FROM card_recurrences WHERE id = ?;

-- name: ListDueRecurrences :many
SELECT * FROM card_recurrences WHERE paused_at IS NULL AND next_run_at IS NOT NULL AND next_run_at <= ? ORDER BY next_run_at LIMIT ?;
