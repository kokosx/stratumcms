-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = ?;
