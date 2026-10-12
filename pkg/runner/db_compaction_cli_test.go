package runner

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cliCompactionMgr struct {
	fakeDBMaintenanceMgr
	pulled  []string
	specs   []*docker.ContainerSpec
	pullErr error
}

func (m *cliCompactionMgr) PullImage(_ context.Context, image, _ string) error {
	m.pulled = append(m.pulled, image)

	return m.pullErr
}

func (m *cliCompactionMgr) RunInitContainer(
	ctx context.Context, spec *docker.ContainerSpec, stdout, stderr io.Writer,
) error {
	m.specs = append(m.specs, spec)

	return m.fakeDBMaintenanceMgr.RunInitContainer(ctx, spec, stdout, stderr)
}

func TestCompactDatadir_ClientCommands(t *testing.T) {
	for _, ct := range []client.ClientType{client.ClientGeth, client.ClientErigon} {
		t.Run(string(ct), func(t *testing.T) {
			dir := t.TempDir()
			mgr := &cliCompactionMgr{}
			cfg := &config.DBCompactionConfig{ExtraArgs: []string{"--example-option"}}
			require.NoError(t, CompactDatadir(context.Background(), logrus.New(), mgr, &DatadirCompaction{
				Client: ct, DataDir: dir, Image: "client:pinned", Cfg: cfg,
			}))
			assert.Equal(t, []string{"client:pinned"}, mgr.pulled)
			require.Len(t, mgr.specs, 3)

			for _, spec := range mgr.specs {
				assert.Equal(t, "client:pinned", spec.Image)
				assert.Empty(t, spec.Entrypoint, "use the client image's entrypoint")
				require.Len(t, spec.Mounts, 1)
				assert.Equal(t, docker.Mount{Type: "bind", Source: dir, Target: "/data"}, spec.Mounts[0])
			}

			assert.Equal(t, []string{"db", "compact", "--datadir=/data", "--example-option"}, mgr.specs[1].Command)
			assert.NotContains(t, mgr.specs[0].Command, "--example-option")
			assert.NotContains(t, mgr.specs[2].Command, "--example-option")
		})
	}
}

func TestCompactDatadir_RejectsUnsupportedClients(t *testing.T) {
	for _, ct := range []client.ClientType{client.ClientBesu, client.ClientNethermind, client.ClientReth, client.ClientEthrex} {
		t.Run(string(ct), func(t *testing.T) {
			mgr := &cliCompactionMgr{}
			err := CompactDatadir(context.Background(), logrus.New(), mgr, &DatadirCompaction{Client: ct})
			require.ErrorContains(t, err, "no database compaction command")
			assert.Empty(t, mgr.pulled)
			assert.Empty(t, mgr.specs)
		})
	}
}

func TestCompactDatadir_Failures(t *testing.T) {
	for _, stage := range []string{"pull", "compact"} {
		t.Run(stage, func(t *testing.T) {
			mgr := &cliCompactionMgr{}
			if stage == "pull" {
				mgr.pullErr = fmt.Errorf("image unavailable")
			} else {
				mgr.failCommand = "db"
			}

			inspect := false
			err := CompactDatadir(context.Background(), logrus.New(), mgr, &DatadirCompaction{
				Client: client.ClientGeth, DataDir: t.TempDir(), Image: "client:pinned",
				Cfg: &config.DBCompactionConfig{Inspect: &inspect},
			})
			require.Error(t, err)

			if stage == "pull" {
				require.ErrorContains(t, err, "image unavailable")
				assert.Empty(t, mgr.specs)
			} else {
				require.ErrorContains(t, err, "exit status 1")
				require.Len(t, mgr.specs, 1)
			}
		})
	}
}

func TestCompactDatadir_RejectsEmptyDatadir(t *testing.T) {
	mgr := &cliCompactionMgr{}
	err := CompactDatadir(context.Background(), logrus.New(), mgr, &DatadirCompaction{
		Client: client.ClientGeth, Image: "client:pinned",
		Cfg: &config.DBCompactionConfig{},
	})
	require.ErrorContains(t, err, "datadir must not be empty")
	assert.Empty(t, mgr.pulled)
	assert.Empty(t, mgr.specs)
}
