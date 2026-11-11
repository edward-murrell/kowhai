-- name: GetCard :one
SELECT * FROM cards WHERE id = ?;

-- name: GetCardsByIDs :many
SELECT * FROM cards WHERE id IN (sqlc.slice('ids'));

-- name: ListBoardCards :many
SELECT * FROM cards
WHERE board_id = sqlc.arg('board_id') AND is_template = FALSE
  AND (CAST(sqlc.arg('include_archived') AS BOOLEAN) OR archived_at IS NULL)
ORDER BY position, id;

-- name: ListListCards :many
SELECT * FROM cards WHERE list_id = ? AND is_template = FALSE AND archived_at IS NULL ORDER BY position, id;

-- name: ListAllListCards :many
SELECT * FROM cards WHERE list_id = ? ORDER BY position, id;

-- name: ListBoardTemplateCards :many
SELECT * FROM cards WHERE board_id = ? AND is_template = TRUE AND archived_at IS NULL ORDER BY title, id;

-- name: CreateCard :one
INSERT INTO cards (id, list_id, board_id, title, description, position, due_at, start_at, is_template, recurrence_id, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateCard :one
UPDATE cards SET title = ?, description = ?, due_at = ?, due_done = ?, start_at = ?, cover_attachment_id = ?, is_template = ?,
    archived_at = ?, due_soon_notified_at = ?, overdue_notified_at = ?, updated_at = ?, version = version + 1
WHERE id = ?
RETURNING *;

-- name: MoveCard :one
UPDATE cards SET list_id = ?, board_id = ?, position = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;

-- name: SetCardPosition :exec
UPDATE cards SET position = ? WHERE id = ?;

-- name: DeleteCard :exec
DELETE FROM cards WHERE id = ?;

-- name: ClearCardCover :exec
UPDATE cards SET cover_attachment_id = NULL, updated_at = ?, version = version + 1 WHERE cover_attachment_id = ?;

-- name: LastCardPosition :one
SELECT CAST(coalesce(max(position), '') AS TEXT) FROM cards WHERE list_id = ?;

-- name: FirstCardPosition :one
SELECT CAST(coalesce(min(position), '') AS TEXT) FROM cards WHERE list_id = ?;

-- name: ListsWithLongCardPositions :many
SELECT DISTINCT list_id FROM cards WHERE length(position) > CAST(sqlc.arg('max_len') AS INTEGER);

-- name: ListCardMembers :many
SELECT user_id FROM card_members WHERE card_id = ? ORDER BY created_at;

-- name: ListBoardCardMembers :many
SELECT cm.card_id, cm.user_id FROM card_members cm JOIN cards c ON c.id = cm.card_id WHERE c.board_id = ? ORDER BY cm.created_at;

-- name: AddCardMember :execrows
INSERT INTO card_members (card_id, user_id, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING;

-- name: RemoveCardMember :execrows
DELETE FROM card_members WHERE card_id = ? AND user_id = ?;

-- name: ListCardWatchers :many
SELECT user_id FROM card_watchers WHERE card_id = ?;

-- name: AddCardWatcher :execrows
INSERT INTO card_watchers (card_id, user_id, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING;

-- name: RemoveCardWatcher :execrows
DELETE FROM card_watchers WHERE card_id = ? AND user_id = ?;

-- name: IsCardWatcher :one
SELECT count(*) FROM card_watchers WHERE card_id = ? AND user_id = ?;

-- name: ListCardLabelIDs :many
SELECT label_id FROM card_labels WHERE card_id = ?;

-- name: ListBoardCardLabels :many
SELECT cl.card_id, cl.label_id FROM card_labels cl JOIN cards c ON c.id = cl.card_id WHERE c.board_id = ?;

-- name: AddCardLabel :execrows
INSERT INTO card_labels (card_id, label_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: RemoveCardLabel :execrows
DELETE FROM card_labels WHERE card_id = ? AND label_id = ?;

-- name: ListCardsDueSoon :many
SELECT * FROM cards
WHERE due_at IS NOT NULL AND due_done = FALSE AND archived_at IS NULL AND is_template = FALSE
  AND due_at > sqlc.arg('now') AND due_at <= sqlc.arg('soon') AND due_soon_notified_at IS NULL;

-- name: ListCardsOverdue :many
SELECT * FROM cards
WHERE due_at IS NOT NULL AND due_done = FALSE AND archived_at IS NULL AND is_template = FALSE
  AND due_at <= sqlc.arg('now') AND overdue_notified_at IS NULL;

-- name: MarkCardDueSoonNotified :exec
UPDATE cards SET due_soon_notified_at = ? WHERE id = ?;

-- name: MarkCardOverdueNotified :exec
UPDATE cards SET overdue_notified_at = ? WHERE id = ?;

-- name: CountBoardCardChecklistItems :many
SELECT card_id, count(*) AS total, CAST(coalesce(sum(CASE WHEN done THEN 1 ELSE 0 END), 0) AS INTEGER) AS done
FROM checklist_items WHERE card_id IN (SELECT id FROM cards WHERE cards.board_id = ?) GROUP BY card_id;

-- name: CountBoardCardComments :many
SELECT card_id, count(*) AS total FROM comments
WHERE deleted_at IS NULL AND card_id IN (SELECT id FROM cards WHERE cards.board_id = ?) GROUP BY card_id;

-- name: CountBoardCardAttachments :many
SELECT card_id, count(*) AS total FROM attachments WHERE board_id = ? GROUP BY card_id;

-- name: SearchCardTitles :many
SELECT * FROM cards WHERE board_id IN (sqlc.slice('board_ids')) AND is_template = FALSE AND archived_at IS NULL
  AND title LIKE '%' || CAST(sqlc.arg('query') AS TEXT) || '%' ORDER BY updated_at DESC LIMIT sqlc.arg('limit');

-- name: GetCardCounts :one
SELECT
    CAST((SELECT count(*) FROM checklist_items i WHERE i.card_id = sqlc.arg('card_id')) AS INTEGER) AS checklist_items,
    CAST((SELECT count(*) FROM checklist_items i WHERE i.card_id = sqlc.arg('card_id') AND i.done) AS INTEGER) AS checklist_done,
    CAST((SELECT count(*) FROM comments c WHERE c.card_id = sqlc.arg('card_id') AND c.deleted_at IS NULL) AS INTEGER) AS comments,
    CAST((SELECT count(*) FROM attachments a WHERE a.card_id = sqlc.arg('card_id')) AS INTEGER) AS attachments;
