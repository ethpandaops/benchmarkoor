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
// The driver reads everything after the first "?" as DSN options, whether or
// not the path has a "file:" prefix. Build keeps the form it is given. A plain
// path stays plain: the driver cuts the options off and SQLite gets a literal
// filename. A "file:" prefix would make SQLite parse the name as a URI, which
// ends it at a "#" and decodes "%XX", so a valid path could open a different
// file.
func Build(path string, pragmas []Pragma) string {
	if path == "" {
		return path
	}

	file, query, _ := strings.Cut(path, "?")

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
		return file
	}

	return file + "?" + strings.Join(opts, "&")
}
