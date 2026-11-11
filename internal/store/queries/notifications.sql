-- name: CreateNotification :one
INSERT INTO notifications (id, user_id, activity_id, created_at) VALUES (?, ?, ?, ?) RETURNING *;

-- name: GetNotification :one
SELECT * FROM notifications WHERE id = ?;

-- name: ListUserNotifications :many
SELECT sqlc.embed(notifications), sqlc.embed(activities) FROM notifications
JOIN activities ON activities.id = notifications.activity_id
WHERE notifications.user_id = sqlc.arg('user_id')
  AND (CAST(sqlc.arg('before') AS TEXT) = '' OR notifications.id < sqlc.arg('before'))
  AND (CAST(sqlc.arg('unread_only') AS BOOLEAN) = FALSE OR notifications.read_at IS NULL)
ORDER BY notifications.id DESC
LIMIT sqlc.arg('limit');

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = ? AND read_at IS NULL;

-- name: SetNotificationRead :one
UPDATE notifications SET read_at = ? WHERE id = ? AND user_id = ? RETURNING *;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL;

-- name: GetNotificationPrefs :one
SELECT * FROM notification_prefs WHERE user_id = ?;

-- name: UpsertNotificationPrefs :exec
INSERT INTO notification_prefs (user_id, settings, updated_at) VALUES (?, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET settings = excluded.settings, updated_at = excluded.updated_at;
