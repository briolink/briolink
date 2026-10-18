-- name: insert_audit :one
-- param: at string
-- param: actor string
-- param: action string
-- param: subject string
-- param: detail string
-- column: id int
INSERT INTO audit (at, actor, action, subject, detail) VALUES (?, ?, ?, ?, ?) RETURNING id;

-- name: list_audit :many
-- param: limit int
-- column: id int
-- column: at string
-- column: actor string
-- column: action string
-- column: subject string
-- column: detail string
SELECT id, at, actor, action, subject, detail FROM audit ORDER BY id DESC LIMIT ?;
