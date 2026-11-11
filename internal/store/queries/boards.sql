-- name: GetBoard :one
SELECT * FROM boards WHERE id = ?;

-- name: CreateBoard :one
INSERT INTO boards (id, workspace_id, name, description, visibility, background, is_template, settings, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateBoard :one
UPDATE boards SET name = ?, description = ?, visibility = ?, background = ?, is_template = ?, settings = ?, archived_at = ?,
    updated_at = ?, version = version + 1
WHERE id = ?
RETURNING *;

-- name: DeleteBoard :exec
DELETE FROM boards WHERE id = ?;

-- name: ListWorkspaceBoards :many
SELECT * FROM boards
WHERE workspace_id = sqlc.arg('workspace_id')
  AND is_template = sqlc.arg('is_template')
  AND (CAST(sqlc.arg('include_archived') AS BOOLEAN) OR archived_at IS NULL)
ORDER BY name, id;

-- name: ListAllBoards :many
SELECT * FROM boards WHERE archived_at IS NULL AND is_template = FALSE ORDER BY workspace_id, name;

-- name: GetBoardRole :one
SELECT role FROM board_members WHERE board_id = ? AND user_id = ?;

-- name: ListBoardRolesForUser :many
SELECT board_id, role FROM board_members WHERE user_id = ?;

-- name: ListBoardMembers :many
SELECT sqlc.embed(users), m.role, m.created_at AS joined_at FROM board_members m
JOIN users ON users.id = m.user_id
WHERE m.board_id = ?
ORDER BY users.display_name, users.id;

-- name: UpsertBoardMember :exec
INSERT INTO board_members (board_id, user_id, role, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT (board_id, user_id) DO UPDATE SET role = excluded.role;

-- name: DeleteBoardMember :exec
DELETE FROM board_members WHERE board_id = ? AND user_id = ?;

-- name: CountBoardAdmins :one
SELECT count(*) FROM board_members WHERE board_id = ? AND role = 'admin';

-- name: ListAllAttachmentKeysForBoard :many
SELECT object_key FROM attachments WHERE board_id = ?;

-- name: ListAllAttachmentKeysForWorkspace :many
SELECT a.object_key FROM attachments a JOIN boards b ON b.id = a.board_id WHERE b.workspace_id = ?;
