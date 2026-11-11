-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByOIDC :one
SELECT * FROM users WHERE oidc_issuer = ? AND oidc_subject = ?;

-- name: GetUsersByIDs :many
SELECT * FROM users WHERE id IN (sqlc.slice('ids'));

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower(?) AND disabled_at IS NULL ORDER BY created_at LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (id, oidc_issuer, oidc_subject, email, display_name, avatar_url, groups, is_admin, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateUserFromLogin :one
UPDATE users SET email = ?, display_name = ?, avatar_url = ?, groups = ?, is_admin = ?, updated_at = ?, version = version + 1
WHERE id = ?
RETURNING *;

-- name: SetUserDisabled :one
UPDATE users SET disabled_at = ?, updated_at = ?, version = version + 1 WHERE id = ? RETURNING *;

-- name: ListUsers :many
SELECT * FROM users
WHERE id > sqlc.arg('after')
  AND (CAST(sqlc.arg('query') AS TEXT) = '' OR display_name LIKE '%' || sqlc.arg('query') || '%' OR email LIKE '%' || sqlc.arg('query') || '%')
ORDER BY id
LIMIT sqlc.arg('limit');
