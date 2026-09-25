// Package sqlitedsn builds SQLite DSNs that carry their own pragmas.
//
// SQLite applies most pragmas per connection. A pragma run once against a
// *sql.DB therefore reaches only the single pooled connection that happened
// to serve it, and every connection the pool opens afterwards starts on the
// defaults. Carrying the pragmas in the DSN makes the driver apply them as it
// opens each connection, which is the only way a read pool gets them all.
package sqlitedsn

import (
	"fmt"
	"strings"
)

// Pragma is one pragma and the value to set it to.
type Pragma struct {
	Name  string
	Value string
}

// Build returns path as a DSN that carries pragmas. A pragma the path already
// sets is left alone, so an operator can still override one through config.
//
// The driver reads everything after the first "?" as DSN options, so a plain
// path, a "file:" path and a full DSN all come back with the pragmas appended
// and the file itself untouched.
func Build(path string, pragmas []Pragma) string {
	if path == "" {
		return path
	}

	file, query, _ := strings.Cut(strings.TrimPrefix(path, "file:"), "?")

	opts := make([]string, 0, len(pragmas)+1)
	if query != "" {
		opts = append(opts, query)
	}

	for _, pragma := range pragmas {
		if strings.Contains(query, "_pragma="+pragma.Name+"(") {
			continue
		}

		opts = append(opts, fmt.Sprintf(
			"_pragma=%s(%s)", pragma.Name, pragma.Value,
		))
	}

	if len(opts) == 0 {
		return "file:" + file
	}

	return "file:" + file + "?" + strings.Join(opts, "&")
}
