-- name: insert_secret :one
-- param: name string
-- param: sealed string
-- param: created_at string
-- param: expires_at ?string
-- param: remind_at ?string
-- column: name string
INSERT INTO secrets (name, sealed, created_at, expires_at, remind_at) VALUES (?, ?, ?, ?, ?) RETURNING name;

-- name: get_secret :one
-- param: name string
-- column: sealed string
SELECT sealed FROM secrets WHERE name = ?;

-- name: rotate_secret :one
-- param: sealed string
-- param: rotated_at string
-- param: expires_at ?string
-- param: remind_at ?string
-- param: name string
-- column: name string
UPDATE secrets SET sealed = ?, rotated_at = ?, expires_at = ?, remind_at = ? WHERE name = ? RETURNING name;

-- name: delete_secret :one
-- param: name string
-- column: name string
DELETE FROM secrets WHERE name = ? RETURNING name;

-- name: list_secrets :many
-- column: name string
-- column: created_at string
-- column: rotated_at ?string
-- column: expires_at ?string
-- column: remind_at ?string
SELECT name, created_at, rotated_at, expires_at, remind_at FROM secrets ORDER BY name;

-- name: list_sealed :many
-- column: name string
-- column: sealed string
SELECT name, sealed FROM secrets ORDER BY name;
