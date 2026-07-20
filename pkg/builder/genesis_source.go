package builder

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// isHTTPURL reports whether ref is an http(s) URL rather than a local path.
func isHTTPURL(ref string) bool {
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

// resolveGenesisFile resolves a genesis/chainspec reference to a local file
// path. An http(s) URL is downloaded to a temp file under the mount temp dir
// (0644, so the container UID can bind-mount + read it) and returned with a
// cleanup that removes it. A local path (or empty ref) passes through unchanged
// with a no-op cleanup, so callers can always defer the cleanup unconditionally.
func resolveGenesisFile(ctx context.Context, log logrus.FieldLogger, ref string) (string, func(), error) {
	noop := func() {}

	if ref == "" || !isHTTPURL(ref) {
		return ref, noop, nil
	}

	data, err := httpGetBytes(ctx, ref)
	if err != nil {
		return "", noop, fmt.Errorf("downloading genesis %q: %w", ref, err)
	}

	f, err := os.CreateTemp(mountTempDir(), "benchmarkoor-genesis-*.json")
	if err != nil {
		return "", noop, fmt.Errorf("creating temp genesis file: %w", err)
	}

	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()

		return "", noop, fmt.Errorf("writing temp genesis file: %w", err)
	}

	if err := f.Close(); err != nil {
		cleanup()

		return "", noop, fmt.Errorf("closing temp genesis file: %w", err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		cleanup()

		return "", noop, fmt.Errorf("chmod temp genesis file: %w", err)
	}

	log.WithFields(logrus.Fields{"url": ref, "path": path, "bytes": len(data)}).
		Info("Downloaded genesis from URL")

	return path, cleanup, nil
}

// httpGetBytes fetches url and returns the response body, erroring on non-200.
func httpGetBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
