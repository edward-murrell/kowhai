-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = ?;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspaces WHERE slug = ?;

-- name: CountWorkspaceSlug :one
SELECT count(*) FROM workspaces WHERE slug = ?;

-- name: CreateWorkspace :one
INSERT INTO workspaces (id, name, slug, description, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateWorkspace :one
UPDATE workspaces SET name = ?, description = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;

-- name: DeleteWorkspace :exec
DELETE FROM workspaces WHERE id = ?;

-- name: ListUserWorkspaces :many
SELECT w.*, m.role FROM workspaces w
JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = sqlc.arg('user_id') AND w.id > sqlc.arg('after')
ORDER BY w.id
LIMIT sqlc.arg('limit');

-- name: GetWorkspaceRole :one
SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?;

-- name: ListWorkspaceMembers :many
SELECT sqlc.embed(users), m.role, m.created_at AS joined_at FROM workspace_members m
JOIN users ON users.id = m.user_id
WHERE m.workspace_id = ?
ORDER BY users.display_name, users.id;

-- name: UpsertWorkspaceMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = excluded.role;

-- name: DeleteWorkspaceMember :exec
DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?;

-- name: CountWorkspaceOwners :one
SELECT count(*) FROM workspace_members WHERE workspace_id = ? AND role = 'owner';

-- name: UpsertWorkspaceInvite :one
INSERT INTO workspace_invites (id, workspace_id, email, role, invited_by, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (workspace_id, email) DO UPDATE SET role = excluded.role, invited_by = excluded.invited_by, expires_at = excluded.expires_at
RETURNING *;

-- name: ListWorkspaceInvites :many
SELECT * FROM workspace_invites WHERE workspace_id = ? AND expires_at > ? ORDER BY email;

-- name: ListInvitesForEmail :many
SELECT * FROM workspace_invites WHERE lower(email) = lower(sqlc.arg('email')) AND expires_at > sqlc.arg('now');

-- name: DeleteWorkspaceInvite :exec
DELETE FROM workspace_invites WHERE id = ?;

-- name: DeleteWorkspaceInviteByID :exec
DELETE FROM workspace_invites WHERE id = ? AND workspace_id = ?;
