// Package migrations holds the control plane's SQL migrations, in file-name
// order. The sqlite generator reads the same files from disk to check the
// queries against the schema they produce.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
