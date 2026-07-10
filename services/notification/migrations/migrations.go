// Package migrations embeds the notifier's SQL migrations so they ship inside
// the binary and resolve independently of the process working directory.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
