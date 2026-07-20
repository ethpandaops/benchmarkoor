package builder

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}

func TestIsHTTPURL(t *testing.T) {
	assert.True(t, isHTTPURL("http://example.com/chainspec.json"))
	assert.True(t, isHTTPURL("https://example.com/chainspec.json"))
	assert.False(t, isHTTPURL("/abs/path/chainspec.json"))
	assert.False(t, isHTTPURL("relative/chainspec.json"))
	assert.False(t, isHTTPURL(""))
}

func TestResolveGenesisFile_LocalPassthrough(t *testing.T) {
	for _, in := range []string{"", "/some/abs/chainspec.json", "relative.json"} {
		path, cleanup, err := resolveGenesisFile(context.Background(), discardLogger(), in)
		require.NoError(t, err)

		defer cleanup()

		assert.Equal(t, in, path, "local/empty refs pass through unchanged")
	}
}

func TestResolveGenesisFile_DownloadsURL(t *testing.T) {
	const body = `{"params":{"eip1559Transition":"0x0"}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	path, cleanup, err := resolveGenesisFile(context.Background(), discardLogger(), srv.URL+"/chainspec.json")
	require.NoError(t, err)

	assert.NotEqual(t, srv.URL+"/chainspec.json", path, "URL resolves to a local temp path")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, body, string(data), "downloaded content is written to the temp file")

	// Cleanup removes the temp file.
	cleanup()
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "cleanup removes the temp file")
}

func TestResolveGenesisFile_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := resolveGenesisFile(context.Background(), discardLogger(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading genesis")
}
