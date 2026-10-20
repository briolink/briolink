-- name: insert_token :one
-- param: name string
-- param: hash string
-- param: scope string
-- param: created_at string
-- param: expires_at ?string
-- param: remind_at ?string
-- column: id int
INSERT INTO tokens (name, hash, scope, created_at, expires_at, remind_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id;

-- name: get_token_by_hash :one
-- param: hash string
-- column: id int
-- column: name string
-- column: scope string
-- column: expires_at ?string
-- column: revoked_at ?string
SELECT id, name, scope, expires_at, revoked_at FROM tokens WHERE hash = ?;

-- name: revoke_token :one
-- param: revoked_at string
-- param: name string
-- column: id int
UPDATE tokens SET revoked_at = ? WHERE name = ? AND revoked_at IS NULL RETURNING id;

-- name: list_tokens :many
-- column: id int
-- column: name string
-- column: scope string
-- column: created_at string
-- column: expires_at ?string
-- column: remind_at ?string
-- column: revoked_at ?string
SELECT id, name, scope, created_at, expires_at, remind_at, revoked_at FROM tokens ORDER BY id;
