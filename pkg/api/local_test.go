package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalFileServer_IsAllowedPath(t *testing.T) {
	srv := &localFileServer{
		log:            logrus.New(),
		discoveryPaths: map[string]string{"results": "/data/results"},
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid prefixed path", path: "results/runs/abc/results.json", expected: true},
		{name: "valid prefixed index", path: "results/index.json", expected: true},
		{name: "empty path", path: "", expected: false},
		{name: "path traversal", path: "results/../../etc/passwd", expected: false},
		{name: "dot dot only", path: "..", expected: false},
		{name: "absolute path", path: "/etc/passwd", expected: false},
		{name: "trailing slash", path: "results/runs/abc/", expected: false},
		{name: "double slash", path: "results//abc", expected: false},
		{name: "dot segment", path: "results/./abc", expected: false},
		{name: "unknown prefix", path: "unknown/index.json", expected: false},
		{name: "prefix only no slash", path: "results", expected: false},
		{name: "prefix only with slash", path: "results/", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, srv.isAllowedPath(tt.path))
		})
	}
}

func TestLocalFileServer_ServeFile(t *testing.T) {
	// Create temp directory structure.
	root := t.TempDir()
	runsDir := filepath.Join(root, "runs", "abc")
	require.NoError(t, os.MkdirAll(runsDir, 0o755))
	require.NoError(
		t, os.WriteFile(
			filepath.Join(runsDir, "results.json"),
			[]byte(`{"ok":true}`), 0o644,
		),
	)

	srv := newLocalFileServer(logrus.New(), &config.APILocalStorageConfig{
		Enabled:        true,
		DiscoveryPaths: map[string]string{"mydata": root},
	})

	t.Run("serves existing file via prefix", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/mydata/runs/abc/results.json", nil,
		)
		rec := httptest.NewRecorder()

		err := srv.ServeFile(rec, req, "mydata/runs/abc/results.json")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `{"ok":true}`)
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/mydata/runs/abc/nope.json", nil,
		)
		rec := httptest.NewRecorder()

		err := srv.ServeFile(rec, req, "mydata/runs/abc/nope.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		_ = rec // response not written
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/mydata/../../etc/passwd", nil,
		)
		rec := httptest.NewRecorder()

		err := srv.ServeFile(rec, req, "mydata/../../etc/passwd")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed")
		_ = rec
	})

	t.Run("returns error for unknown prefix", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/unknown/index.json", nil,
		)
		rec := httptest.NewRecorder()

		err := srv.ServeFile(rec, req, "unknown/index.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed")
		_ = rec
	})

	t.Run("routes multiple prefixes to different roots", func(t *testing.T) {
		root2 := t.TempDir()
		require.NoError(
			t, os.MkdirAll(filepath.Join(root2, "archive"), 0o755),
		)
		require.NoError(
			t, os.WriteFile(
				filepath.Join(root2, "archive", "old.json"),
				[]byte(`{"old":true}`), 0o644,
			),
		)

		multi := newLocalFileServer(
			logrus.New(), &config.APILocalStorageConfig{
				Enabled: true,
				DiscoveryPaths: map[string]string{
					"mydata":  root,
					"archive": root2,
				},
			},
		)

		// First prefix.
		req := httptest.NewRequest(
			http.MethodGet,
			"/mydata/runs/abc/results.json", nil,
		)
		rec := httptest.NewRecorder()

		err := multi.ServeFile(rec, req, "mydata/runs/abc/results.json")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `{"ok":true}`)

		// Second prefix.
		req2 := httptest.NewRequest(
			http.MethodGet,
			"/archive/archive/old.json", nil,
		)
		rec2 := httptest.NewRecorder()

		err = multi.ServeFile(rec2, req2, "archive/archive/old.json")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec2.Code)
		assert.Contains(t, rec2.Body.String(), `{"old":true}`)
	})
}
