package api

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// localFileServer serves benchmark result files directly from the local
// filesystem. Each discovery path maps a URL prefix name to an absolute
// directory root; incoming request paths are resolved by extracting the
// prefix and looking up the corresponding directory.
type localFileServer struct {
	log            logrus.FieldLogger
	discoveryPaths map[string]string
}

// newLocalFileServer creates a new local file server from the given config.
func newLocalFileServer(
	log logrus.FieldLogger,
	cfg *config.APILocalStorageConfig,
) *localFileServer {
	paths := make(map[string]string, len(cfg.DiscoveryPaths))
	for name, dir := range cfg.DiscoveryPaths {
		paths[name] = filepath.Clean(dir)
	}

	return &localFileServer{
		log:            log.WithField("component", "local-file-server"),
		discoveryPaths: paths,
	}
}

// ServeFile locates filePath by extracting the first path segment as a
// prefix name, looking up the directory from the map, and resolving the
// remainder relative to that directory. Returns an error when the path is
// disallowed, the prefix is unknown, or the file does not exist.
func (l *localFileServer) ServeFile(
	w http.ResponseWriter,
	r *http.Request,
	filePath string,
) error {
	if !l.isAllowedPath(filePath) {
		return fmt.Errorf("path %q is not allowed", filePath)
	}

	// Split into prefix and remainder (e.g. "results/runs/abc/results.json"
	// → prefix="results", remainder="runs/abc/results.json").
	prefix, remainder, _ := strings.Cut(filePath, "/")

	root, ok := l.discoveryPaths[prefix]
	if !ok {
		return fmt.Errorf(
			"file %q not found: unknown prefix %q", filePath, prefix,
		)
	}

	full := filepath.Join(root, remainder)

	// Defense-in-depth: ensure the resolved path stays under root.
	if !strings.HasPrefix(full, root+string(filepath.Separator)) &&
		full != root {
		return fmt.Errorf("path %q is not allowed", filePath)
	}

	if _, err := os.Stat(full); err != nil {
		return fmt.Errorf(
			"file %q not found in discovery path %q", filePath, prefix,
		)
	}

	http.ServeFile(w, r, full)

	return nil
}

// isAllowedPath rejects empty, absolute, unclean, or traversal request
// paths. Also requires at least two segments (prefix + something) and
// that the prefix exists in the map.
func (l *localFileServer) isAllowedPath(filePath string) bool {
	if filePath == "" {
		return false
	}

	if strings.Contains(filePath, "..") {
		return false
	}

	// Reject paths that start with a slash (absolute paths).
	if filepath.IsAbs(filePath) {
		return false
	}

	// Ensure the path is clean (no double slashes, trailing slashes, etc.).
	if path.Clean(filePath) != filePath {
		return false
	}

	// Require at least two segments: prefix/something.
	prefix, remainder, hasSep := strings.Cut(filePath, "/")
	if !hasSep || remainder == "" {
		return false
	}

	// The prefix must exist in the discovery paths map.
	_, ok := l.discoveryPaths[prefix]

	return ok
}
