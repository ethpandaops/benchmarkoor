package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBCompactionMarker_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	r := &runner{log: logrus.New(), cfg: &Config{}}
	log := logrus.New()

	req := &dbCompactionRequest{
		Instance: &config.ClientInstance{ID: "geth", Client: "geth"},
		Phase:    config.DBCompactionBeforePreRuns,
		RunID:    "run-1",
	}
	report := &dbCompactionReport{
		Image:        "ethereum/client-go:stable",
		CompletedAt:  "2026-08-27T10:12:03Z",
		DurationMS:   4200,
		DatadirBytes: &dbCompactionSizes{Before: 200, After: 100},
	}

	r.writeDBCompactionMarker(dir, req, report, log)

	marker := readDBCompactionMarker(dir)
	require.NotNil(t, marker)
	assert.Equal(t, dbCompactionMarkerVersion, marker.Version)

	entry, ok := marker.Phases[config.DBCompactionBeforePreRuns]
	require.True(t, ok)
	assert.Equal(t, "geth", entry.Client)
	assert.Equal(t, "run-1", entry.RunID)
	assert.Equal(t, "2026-08-27T10:12:03Z", entry.CompletedAt)
	assert.Equal(t, int64(4200), entry.DurationMS)
	require.NotNil(t, entry.DatadirBytes)
	assert.Equal(t, int64(100), entry.DatadirBytes.After)

	// A second phase is added, never replacing the first.
	req.Phase = config.DBCompactionBeforeBenchmarks
	r.writeDBCompactionMarker(dir, req, report, log)

	marker = readDBCompactionMarker(dir)
	require.NotNil(t, marker)
	assert.Len(t, marker.Phases, 2)
}

func TestReadDBCompactionMarker_MissingOrCorrupt(t *testing.T) {
	dir := t.TempDir()

	assert.Nil(t, readDBCompactionMarker(dir))

	path := filepath.Join(dir, config.DBCompactionMarkerFile)
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0644))

	assert.Nil(t, readDBCompactionMarker(dir))
}

func TestDBCompactionRequest_HostPath(t *testing.T) {
	bind := &dbCompactionRequest{
		Mount: docker.Mount{Type: "bind", Source: "/snapshots/geth", Target: "/data"},
	}
	assert.Equal(t, "/snapshots/geth", bind.hostPath())

	volume := &dbCompactionRequest{
		Mount: docker.Mount{Type: "volume", Source: "benchmarkoor-vol", Target: "/data"},
	}
	assert.Empty(t, volume.hostPath())
}

func TestDatadirMountFor(t *testing.T) {
	spec, err := client.NewRegistry().Get(client.ClientGeth)
	require.NoError(t, err)

	dataMount := docker.Mount{Type: "bind", Source: "/snapshots/geth", Target: "/data"}
	containerSpec := &docker.ContainerSpec{
		Mounts: []docker.Mount{
			{Type: "bind", Source: "/tmp/jwt", Target: "/tmp/jwtsecret"},
			dataMount,
		},
	}

	t.Run("finds the client's default datadir", func(t *testing.T) {
		mount, ok := datadirMountFor(containerSpec, spec, nil)
		require.True(t, ok)
		assert.Equal(t, dataMount, mount)
	})

	t.Run("honours a custom container_dir", func(t *testing.T) {
		custom := &docker.ContainerSpec{
			Mounts: []docker.Mount{{Type: "bind", Source: "/snap", Target: "/var/lib/geth"}},
		}

		mount, ok := datadirMountFor(
			custom, spec, &config.DataDirConfig{ContainerDir: "/var/lib/geth"},
		)
		require.True(t, ok)
		assert.Equal(t, "/snap", mount.Source)

		_, ok = datadirMountFor(custom, spec, nil)
		assert.False(t, ok)
	})

	t.Run("no container spec", func(t *testing.T) {
		_, ok := datadirMountFor(nil, spec, nil)
		assert.False(t, ok)
	})
}

func TestRunnerDBCompactionFor(t *testing.T) {
	instance := &config.ClientInstance{ID: "geth", Client: "geth"}

	r := &runner{cfg: &Config{FullConfig: &config.Config{
		Runner: config.RunnerConfig{
			Client: config.ClientConfig{Config: config.ClientDefaults{
				DBCompaction: &config.DBCompactionConfig{
					Enabled: true,
					When:    []string{config.DBCompactionBeforePreRuns},
				},
			}},
		},
	}}}

	assert.NotNil(t, r.dbCompactionFor(instance, config.DBCompactionBeforePreRuns))
	assert.Nil(t, r.dbCompactionFor(instance, config.DBCompactionBeforeBenchmarks))

	bare := &runner{cfg: &Config{}}
	assert.Nil(t, bare.dbCompactionFor(instance, config.DBCompactionBeforePreRuns))
}

func TestGethDBMaintenanceCommands(t *testing.T) {
	spec, err := client.NewRegistry().Get(client.ClientGeth)
	require.NoError(t, err)

	cmds := spec.DBMaintenanceCommands("/var/lib/geth")
	require.NotNil(t, cmds)
	assert.Equal(t, []string{"db", "compact", "--datadir=/var/lib/geth"}, cmds.Compact)
	assert.Equal(t, []string{"db", "inspect", "--datadir=/var/lib/geth"}, cmds.Inspect)

	assert.True(t, client.SupportsDBCompaction(client.ClientGeth))

	for _, other := range []client.ClientType{
		client.ClientBesu, client.ClientNethermind, client.ClientErigon,
		client.ClientReth, client.ClientNimbus, client.ClientEthrex,
	} {
		assert.False(t, client.SupportsDBCompaction(other), string(other))
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("12345"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "b"), []byte("123"), 0644))

	assert.Equal(t, int64(8), dirSize(dir))
	assert.Equal(t, int64(0), dirSize(filepath.Join(dir, "missing")))
}
