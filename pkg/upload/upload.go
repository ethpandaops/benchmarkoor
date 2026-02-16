package upload

import "context"

// Uploader uploads a local result directory to remote storage.
type Uploader interface {
	// Preflight verifies that the remote storage is reachable and writable.
	// Writes a small test object to the bucket to fail fast on misconfiguration.
	Preflight(ctx context.Context) error

	// Upload uploads all files in localDir. The directory basename is
	// used as a sub-prefix under the configured remote prefix.
	Upload(ctx context.Context, localDir string) error

	// UploadSuiteDir uploads a suite directory to remote storage under
	// prefix + "/suites/" + dirname.
	UploadSuiteDir(ctx context.Context, localSuiteDir string) error
}
