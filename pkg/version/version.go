// Package version holds build-time version information set via ldflags
// in main. Shared across packages so the runner can log it to
// benchmarkoor.log alongside the rest of the run output.
package version

// Set at build time via -ldflags in the main package.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
