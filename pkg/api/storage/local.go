package storage

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// Compile-time interface check.
var _ Reader = (*localReader)(nil)

type localReader struct {
	// paths maps discovery path names to absolute directory paths.
	paths map[string]string
}

// NewLocalReader creates a Reader backed by local filesystem directories.
func NewLocalReader(cfg *config.APILocalStorageConfig) Reader {
	paths := make(map[string]string, len(cfg.DiscoveryPaths))
	maps.Copy(paths, cfg.DiscoveryPaths)

	return &localReader{paths: paths}
}

// DiscoveryPaths returns the configured discovery path names sorted.
func (r *localReader) DiscoveryPaths() []string {
	keys := make([]string, 0, len(r.paths))
	for k := range r.paths {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// ListRunIDs returns run directory names under {dirPath}/runs/.
func (r *localReader) ListRunIDs(
	_ context.Context, discoveryPath string,
) ([]string, error) {
	dirPath, ok := r.paths[discoveryPath]
	if !ok {
		return nil, fmt.Errorf(
			"unknown discovery path: %q", discoveryPath,
		)
	}

	runsDir := filepath.Join(dirPath, "runs")

	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("reading runs directory: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}

	return ids, nil
}

// GetRunFile reads a file from {dirPath}/runs/{runID}/{filename}.
// Returns (nil, nil) when the file does not exist.
func (r *localReader) GetRunFile(
	_ context.Context, discoveryPath, runID, filename string,
) ([]byte, error) {
	dirPath, ok := r.paths[discoveryPath]
	if !ok {
		return nil, fmt.Errorf(
			"unknown discovery path: %q", discoveryPath,
		)
	}

	p := filepath.Join(dirPath, "runs", runID, filename)

	data, err := os.ReadFile(p) //nolint:gosec // trusted paths from config
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("reading file %s: %w", p, err)
	}

	return data, nil
}

// GetSuiteFile reads a file from {dirPath}/suites/{suiteHash}/{filename}.
// Returns (nil, nil) when the file does not exist.
func (r *localReader) GetSuiteFile(
	_ context.Context, discoveryPath, suiteHash, filename string,
) ([]byte, error) {
	dirPath, ok := r.paths[discoveryPath]
	if !ok {
		return nil, fmt.Errorf(
			"unknown discovery path: %q", discoveryPath,
		)
	}

	p := filepath.Join(dirPath, "suites", suiteHash, filename)

	data, err := os.ReadFile(p) //nolint:gosec // trusted paths from config
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("reading file %s: %w", p, err)
	}

	return data, nil
}
