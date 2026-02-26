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
		discoveryPaths: []string{"/data/results"},
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "valid simple path", path: "runs/abc/results.json", expected: true},
		{name: "valid nested path", path: "index.json", expected: true},
		{name: "empty path", path: "", expected: false},
		{name: "path traversal", path: "runs/../../etc/passwd", expected: false},
		{name: "dot dot only", path: "..", expected: false},
		{name: "absolute path", path: "/etc/passwd", expected: false},
		{name: "trailing slash", path: "runs/abc/", expected: false},
		{name: "double slash", path: "runs//abc", expected: false},
		{name: "dot segment", path: "runs/./abc", expected: false},
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
		DiscoveryPaths: []string{root},
	})

	t.Run("serves existing file", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runs/abc/results.json", nil)
		rec := httptest.NewRecorder()

		err := srv.ServeFile(rec, req, "runs/abc/results.json")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `{"ok":true}`)
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runs/abc/nope.json", nil)
		rec := httptest.NewRecorder()

		err := srv.ServeFile(rec, req, "runs/abc/nope.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		_ = rec // response not written
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/../../etc/passwd", nil)
		rec := httptest.NewRecorder()

		err := srv.ServeFile(rec, req, "../../etc/passwd")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed")
		_ = rec
	})

	t.Run("searches multiple roots", func(t *testing.T) {
		root2 := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root2, "archive"), 0o755))
		require.NoError(
			t, os.WriteFile(
				filepath.Join(root2, "archive", "old.json"),
				[]byte(`{"old":true}`), 0o644,
			),
		)

		multi := newLocalFileServer(logrus.New(), &config.APILocalStorageConfig{
			Enabled:        true,
			DiscoveryPaths: []string{root, root2},
		})

		req := httptest.NewRequest(http.MethodGet, "/archive/old.json", nil)
		rec := httptest.NewRecorder()

		err := multi.ServeFile(rec, req, "archive/old.json")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `{"old":true}`)
	})
}
