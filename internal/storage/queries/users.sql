-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: GetUserByLogin :one
SELECT id, email, username, password_hash, display_name, role, created_at, updated_at
FROM users WHERE email = ? OR username = ? LIMIT 1;

-- name: GetUserBySessionTokenHash :one
SELECT u.id, u.email, u.username, u.password_hash, u.display_name, u.role, u.created_at, u.updated_at
FROM users u JOIN sessions s ON s.user_id = u.id
WHERE s.token_hash = ? AND s.expires_at > ? LIMIT 1;

-- name: CreateUser :exec
INSERT INTO users (id, email, username, password_hash, display_name, role, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);
