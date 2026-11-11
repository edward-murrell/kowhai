-- name: GetChecklist :one
SELECT * FROM checklists WHERE id = ?;

-- name: ListCardChecklists :many
SELECT * FROM checklists WHERE card_id = ? ORDER BY position, id;

-- name: CreateChecklist :one
INSERT INTO checklists (id, card_id, title, position, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateChecklist :one
UPDATE checklists SET title = ?, position = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;

-- name: DeleteChecklist :exec
DELETE FROM checklists WHERE id = ?;

-- name: LastChecklistPosition :one
SELECT CAST(coalesce(max(position), '') AS TEXT) FROM checklists WHERE card_id = ?;

-- name: GetChecklistItem :one
SELECT * FROM checklist_items WHERE id = ?;

-- name: ListCardChecklistItems :many
SELECT * FROM checklist_items WHERE card_id = ? ORDER BY position, id;

-- name: ListChecklistItems :many
SELECT * FROM checklist_items WHERE checklist_id = ? ORDER BY position, id;

-- name: CreateChecklistItem :one
INSERT INTO checklist_items (id, checklist_id, card_id, text, done, position, assignee_id, due_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateChecklistItem :one
UPDATE checklist_items SET text = ?, done = ?, position = ?, assignee_id = ?, due_at = ?, updated_at = ?, version = version + 1
WHERE id = ?
RETURNING *;

-- name: DeleteChecklistItem :exec
DELETE FROM checklist_items WHERE id = ?;

-- name: LastChecklistItemPosition :one
SELECT CAST(coalesce(max(position), '') AS TEXT) FROM checklist_items WHERE checklist_id = ?;
