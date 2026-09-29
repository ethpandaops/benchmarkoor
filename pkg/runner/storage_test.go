package runner

import (
	"context"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// volumeMgr answers VolumeMountpoint. Other calls panic on the nil interface.
type volumeMgr struct {
	docker.ContainerManager
	mountpoints map[string]string
}

func (m *volumeMgr) VolumeMountpoint(_ context.Context, name string) (string, error) {
	return m.mountpoints[name], nil
}

func TestDataMountHostPath(t *testing.T) {
	r := &runner{containerMgr: &volumeMgr{mountpoints: map[string]string{
		"benchmarkoor-run-geth": "/var/lib/docker/volumes/benchmarkoor-run-geth/_data",
	}}}

	tests := []struct {
		name      string
		mount     docker.Mount
		want      string
		errSubstr string
	}{
		{
			name:  "volume uses its mountpoint",
			mount: docker.Mount{Type: "volume", Source: "benchmarkoor-run-geth"},
			want:  "/var/lib/docker/volumes/benchmarkoor-run-geth/_data",
		},
		{
			name:      "volume without a mountpoint",
			mount:     docker.Mount{Type: "volume", Source: "other"},
			errSubstr: "no mountpoint",
		},
		{
			name:  "bind uses its source",
			mount: docker.Mount{Type: "bind", Source: "/data/overlay/merged"},
			want:  "/data/overlay/merged",
		},
		{
			name:      "tmpfs has no host path",
			mount:     docker.Mount{Type: "tmpfs"},
			errSubstr: "no host path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.dataMountHostPath(context.Background(), tt.mount)
			if tt.errSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
