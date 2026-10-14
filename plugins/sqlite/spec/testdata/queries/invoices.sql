-- name: get_invoice :one
-- param: id string
-- column: id string
-- column: customer string
-- column: currency string
-- column: state string
-- column: number ?string
-- column: issued_at ?time
-- column: paid int
-- column: void_reason ?string
SELECT id, customer, currency, state, number, issued_at, paid, void_reason
FROM invoices
WHERE id = ?;

-- name: list_invoices_by_customer :many
-- param: customer string
-- param: limit int
-- column: id string
-- column: state string
SELECT id, state
FROM invoices
WHERE customer = ? AND note <> 'what? it''s :fine'  -- a ? in a comment is not a placeholder
ORDER BY id
LIMIT ?;

-- name: save_invoice :exec
-- param: state string
-- param: paid int
-- param: id string
UPDATE invoices SET state = ?, paid = ? /* ? */ WHERE id = ?;
