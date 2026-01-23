package datadir

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

// Provider prepares a data directory and returns the mount path.
type Provider interface {
	// Prepare prepares the data directory and returns a PreparedDir.
	Prepare(ctx context.Context, cfg *ProviderConfig) (*PreparedDir, error)
}

// ProviderConfig contains configuration for preparing a data directory.
type ProviderConfig struct {
	SourceDir  string
	InstanceID string
	TmpDir     string
}

// PreparedDir represents a prepared data directory ready for mounting.
type PreparedDir struct {
	MountPath string
	Cleanup   func() error
}

// NewProvider creates a new Provider based on the method.
// Supported methods: "copy" (default), "overlayfs", "fuse-overlayfs", "zfs".
func NewProvider(log logrus.FieldLogger, method string) (Provider, error) {
	switch method {
	case "", "copy":
		return NewCopyProvider(log), nil
	case "overlayfs":
		return NewOverlayFSProvider(log), nil
	case "fuse-overlayfs":
		return NewFuseOverlayFSProvider(log), nil
	case "zfs":
		return NewZFSProvider(log), nil
	default:
		return nil, fmt.Errorf("unknown datadir method: %q", method)
	}
}
