-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, expires_at, last_seen_at, user_agent, created_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ?;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < ?;

-- name: CreateOIDCState :exec
INSERT INTO oidc_states (state, nonce, code_verifier, return_to, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: TakeOIDCState :one
DELETE FROM oidc_states WHERE state = ? RETURNING *;

-- name: DeleteExpiredOIDCStates :execrows
DELETE FROM oidc_states WHERE expires_at < ?;

-- name: CreateToken :one
INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, scopes, workspace_id, board_id, actor_label, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetTokenByHash :one
SELECT * FROM api_tokens WHERE token_hash = ?;

-- name: GetToken :one
SELECT * FROM api_tokens WHERE id = ?;

-- name: ListUserTokens :many
SELECT * FROM api_tokens WHERE user_id = ? ORDER BY id DESC;

-- name: RevokeToken :exec
UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL;

-- name: TouchToken :exec
UPDATE api_tokens SET last_used_at = ? WHERE id = ?;
