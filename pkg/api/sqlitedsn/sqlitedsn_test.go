package sqlitedsn_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
			want: "file:/app/data/index.db" +
				"?_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)",
		},
		{
			name: "relative path",
			path: "data/index.db",
			want: "file:data/index.db" +
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
			want: "file::memory:" +
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
	assert.Equal(t, "file:/app/data/index.db",
		sqlitedsn.Build("/app/data/index.db", nil))
}
