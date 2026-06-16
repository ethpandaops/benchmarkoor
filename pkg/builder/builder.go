// Package builder materialises pre-populated client datadirs declared
// in benchmarkoor's `builder.*` config block. The only implementation
// today wraps https://github.com/ethereum/state-actor.
//
// Builders are deliberately decoupled from the runner: `benchmarkoor build`
// invokes a builder to produce datadirs on disk, and `benchmarkoor run`
// later consumes them through the regular datadir.* providers. There is
// no auto-build path on the run side — a missing datadir surfaces as a
// validation error there.
package builder

import "context"

// Builder materialises one pre-populated client datadir per call.
// Implementations are safe to invoke serially across distinct targets;
// concurrent builds against the same OutputDir are not supported.
type Builder interface {
	// Name returns a short identifier for the builder (e.g. "state-actor").
	Name() string

	// Targets returns the read-only summary of declared targets, in
	// declaration order, for the build command to log and filter on.
	Targets() []TargetInfo

	// Build runs the build for the target identified by name. When the
	// target's OutputDir already contains entries and opts.Force is
	// false, Build is a no-op: it returns (true, nil) so the caller can
	// surface a "skipped" status without treating it as failure.
	// Otherwise the directory is created (wiped first when Force=true)
	// and the build runs.
	Build(ctx context.Context, name string, opts BuildOptions) (skipped bool, err error)
}

// TargetInfo is the read-only summary the build command uses for logging
// and CLI-target filtering.
type TargetInfo struct {
	Name      string
	Client    string
	OutputDir string
}

// BuildOptions controls per-target build behaviour.
type BuildOptions struct {
	// Force, when true, removes OutputDir before mkdir.
	Force bool
}
