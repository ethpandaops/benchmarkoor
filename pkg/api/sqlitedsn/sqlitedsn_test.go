package sqlitedsn_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/sqlitedsn"
)

func TestBuild(t *testing.T) {
	pragmas := []sqlitedsn.Pragma{
		{Name: "busy_timeout", Value: "5000"},
		{Name: "temp_store", Value: "MEMORY"},
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "plain path",
			path: "/app/data/index.db",
			want: "/app/data/index.db" +
				"?_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)",
		},
		{
			name: "relative path",
			path: "data/index.db",
			want: "data/index.db" +
				"?_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)",
		},
		{
			name: "file prefix already there",
			path: "file:/app/data/index.db",
			want: "file:/app/data/index.db" +
				"?_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)",
		},
		{
			name: "in memory",
			path: ":memory:",
			want: ":memory:" +
				"?_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)",
		},
		{
			name: "existing options are kept",
			path: "file:/app/data/index.db?cache=shared",
			want: "file:/app/data/index.db?cache=shared" +
				"&_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)",
		},
		{
			// An operator who set a pragma in config meant it, so the
			// default for that one is dropped rather than appended twice.
			name: "a configured pragma wins",
			path: "file:/app/data/index.db?_pragma=busy_timeout(30000)",
			want: "file:/app/data/index.db?_pragma=busy_timeout(30000)" +
				"&_pragma=temp_store(MEMORY)",
		},
		{
			name: "empty path is left alone",
			path: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sqlitedsn.Build(tt.path, pragmas))
		})
	}
}

func TestBuild_NoPragmas(t *testing.T) {
	assert.Equal(t, "/app/data/index.db",
		sqlitedsn.Build("/app/data/index.db", nil))
	assert.Equal(t, "file:/app/data/index.db",
		sqlitedsn.Build("file:/app/data/index.db", nil))
}

// TestBuild_OpensTheConfiguredFile opens a real database through the driver.
// A plain path must reach SQLite as a literal filename. As a "file:" URI,
// SQLite would stop the name at a "#" and decode "%41" to "A", and so open a
// different file than the one configured.
func TestBuild_OpensTheConfiguredFile(t *testing.T) {
	// The subtests have plain names, because t.TempDir puts the name in the
	// directory path.
	for label, name := range map[string]string{
		"hash":    "index#1.db",
		"percent": "index%41.db",
	} {
		t.Run(label, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)

			db, err := sql.Open("sqlite", sqlitedsn.Build(path, []sqlitedsn.Pragma{
				{Name: "temp_store", Value: "MEMORY"},
			}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			var tempStore int
			require.NoError(t, db.QueryRow("PRAGMA temp_store").Scan(&tempStore))
			assert.Equal(t, 2, tempStore, "the pragma must still apply")

			_, err = db.Exec("CREATE TABLE t (id INTEGER)")
			require.NoError(t, err)

			assert.FileExists(t, path)

			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)

			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}

			assert.Equal(t, []string{name}, names, "no other file was created")
		})
	}
}
