package storage

import "context"

// Reader provides read access to benchmark run data stored in a backend
// (local filesystem or S3). It is used by the indexer to discover and
// read run/suite files without knowing the underlying storage details.
type Reader interface {
	// ListRunIDs returns the run IDs (directory names) under the runs
	// directory for the given discovery path.
	ListRunIDs(ctx context.Context, discoveryPath string) ([]string, error)

	// GetRunFile reads a file from a specific run directory.
	// Returns (nil, nil) when the file does not exist.
	GetRunFile(
		ctx context.Context, discoveryPath, runID, filename string,
	) ([]byte, error)

	// GetSuiteFile reads a file from a specific suite directory.
	// Returns (nil, nil) when the file does not exist.
	GetSuiteFile(
		ctx context.Context, discoveryPath, suiteHash, filename string,
	) ([]byte, error)

	// DiscoveryPaths returns all configured discovery paths.
	DiscoveryPaths() []string
}
