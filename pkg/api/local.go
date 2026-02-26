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
// filesystem. Each discovery path is an absolute directory root; incoming
// request paths are resolved relative to these roots.
type localFileServer struct {
	log            logrus.FieldLogger
	discoveryPaths []string
}

// newLocalFileServer creates a new local file server from the given config.
func newLocalFileServer(
	log logrus.FieldLogger,
	cfg *config.APILocalStorageConfig,
) *localFileServer {
	paths := make([]string, 0, len(cfg.DiscoveryPaths))
	for _, p := range cfg.DiscoveryPaths {
		paths = append(paths, filepath.Clean(p))
	}

	return &localFileServer{
		log:            log.WithField("component", "local-file-server"),
		discoveryPaths: paths,
	}
}

// ServeFile locates filePath under one of the discovery roots and serves
// it via http.ServeFile. Returns an error when the path is disallowed or
// not found under any root.
func (l *localFileServer) ServeFile(
	w http.ResponseWriter,
	r *http.Request,
	filePath string,
) error {
	if !l.isAllowedPath(filePath) {
		return fmt.Errorf("path %q is not allowed", filePath)
	}

	for _, root := range l.discoveryPaths {
		full := filepath.Join(root, filePath)

		// Defense-in-depth: ensure the resolved path stays under root.
		if !strings.HasPrefix(full, root+string(filepath.Separator)) &&
			full != root {
			continue
		}

		if _, err := os.Stat(full); err != nil {
			continue
		}

		http.ServeFile(w, r, full)

		return nil
	}

	return fmt.Errorf("file %q not found in any discovery path", filePath)
}

// isAllowedPath rejects empty, absolute, unclean, or traversal request paths.
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
	return path.Clean(filePath) == filePath
}
