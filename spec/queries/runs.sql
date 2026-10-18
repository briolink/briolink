-- name: insert_run :one
-- param: task int
-- param: started_at string
-- param: tier string
-- param: job string
-- param: model string
-- param: outcome string
-- column: id int
INSERT INTO runs (task, started_at, tier, job, model, outcome) VALUES (?, ?, ?, ?, ?, ?) RETURNING id;

-- name: list_runs :many
-- param: limit int
-- column: id int
-- column: task int
-- column: started_at string
-- column: tier string
-- column: job string
-- column: model string
-- column: outcome string
SELECT id, task, started_at, tier, job, model, outcome FROM runs ORDER BY id DESC LIMIT ?;

-- name: list_runs_by_task :many
-- param: task int
-- param: limit int
-- column: id int
-- column: task int
-- column: started_at string
-- column: tier string
-- column: job string
-- column: model string
-- column: outcome string
SELECT id, task, started_at, tier, job, model, outcome FROM runs WHERE task = ? ORDER BY id DESC LIMIT ?;
