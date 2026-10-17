-- name: list_lines :many
-- param: invoice_id string
-- column: description string
-- column: quantity int
-- column: unit_price int
SELECT description, quantity, unit_price FROM lines WHERE invoice_id = ? ORDER BY position;
