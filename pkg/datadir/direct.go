package datadir

import (
	"context"

	"github.com/sirupsen/logrus"
)

// DirectProvider implements Provider by mounting source_dir directly,
// without copying, snapshotting, or cloning. The container writes
// directly to the user-supplied path, so any changes persist after
// the run.
//
// Intended for inspection / resume workflows (e.g. continuing a
// pre-run replay against an existing ZFS clone left behind by
// --debug.stop-after-prerun). Not suitable for normal benchmarking.
type DirectProvider interface {
	Provider
}

// NewDirectProvider creates a new direct provider.
func NewDirectProvider(log logrus.FieldLogger) DirectProvider {
	return &directProvider{
		log: log.WithField("component", "datadir-direct"),
	}
}

type directProvider struct {
	log logrus.FieldLogger
}

var _ DirectProvider = (*directProvider)(nil)

// Prepare returns the source directory unchanged. No copy or snapshot.
// Cleanup is a no-op so the directory survives the run.
func (p *directProvider) Prepare(_ context.Context, cfg *ProviderConfig) (*PreparedDir, error) {
	p.log.WithFields(logrus.Fields{
		"source":   cfg.SourceDir,
		"instance": cfg.InstanceID,
	}).Warn("Using datadir method=direct: container will write directly to source_dir, changes will persist")

	return &PreparedDir{
		MountPath: cfg.SourceDir,
		Cleanup:   func() error { return nil },
	}, nil
}
