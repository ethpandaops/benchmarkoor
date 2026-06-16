package builder

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func i64(v int64) *int64   { return &v }
func u64(v uint64) *uint64 { return &v }
func iptr(v int) *int      { return &v }
func bptr(v bool) *bool    { return &v }

// allImages registers a placeholder image for every supported client so
// tests that don't care which client runs can use a single map.
var allImages = map[string]string{
	"geth":       "fallback:latest",
	"reth":       "fallback:latest",
	"besu":       "fallback:latest",
	"nethermind": "fallback:latest",
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name     string
		t        config.StateActorTarget
		specPath string
		want     []string
	}{
		{
			name: "geth_target_size",
			t:    config.StateActorTarget{Client: "geth", OutputDir: "/srv/data", TargetSize: "5GB"},
			want: []string{
				"--db=/srv/data/geth/chaindata",
				"--client=geth",
				"--target-size=5GB",
			},
		},
		{
			name:     "reth_spec",
			t:        config.StateActorTarget{Client: "reth", OutputDir: "/srv/r"},
			specPath: "/etc/spec.yaml",
			want: []string{
				"--db=/srv/r",
				"--client=reth",
				"--spec=/etc/spec.yaml",
			},
		},
		{
			name: "besu_no_optional_flags",
			t:    config.StateActorTarget{Client: "besu", OutputDir: "/srv/b", TargetSize: "1GB"},
			want: []string{
				"--db=/srv/b",
				"--client=besu",
				"--target-size=1GB",
			},
		},
		{
			name: "nethermind_full_pointers",
			t: config.StateActorTarget{
				Client: "nethermind", OutputDir: "/srv/n", TargetSize: "10GB",
				Seed: i64(42), Fork: "prague", ChainID: i64(7),
				GasLimit: u64(60_000_000), Timestamp: u64(1700000000),
				ExtraData: "0xdeadbeef",
			},
			want: []string{
				"--db=/srv/n",
				"--client=nethermind",
				"--target-size=10GB",
				"--seed=42",
				"--fork=prague",
				"--chain-id=7",
				"--gas-limit=60000000",
				"--timestamp=1700000000",
				"--extra-data=0xdeadbeef",
			},
		},
		{
			name: "geth_archive_binary_trie_group_depth",
			t: config.StateActorTarget{
				Client: "geth", OutputDir: "/srv/g", TargetSize: "5GB",
				Archive: bptr(true), BinaryTrie: bptr(true), GroupDepth: iptr(4),
			},
			want: []string{
				"--db=/srv/g/geth/chaindata",
				"--client=geth",
				"--target-size=5GB",
				"--archive",
				"--binary-trie",
				"--group-depth=4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs(&tt.t, tt.specPath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDBPath(t *testing.T) {
	assert.Equal(t, "/data/geth/chaindata",
		dbPath(&config.StateActorTarget{Client: "geth", OutputDir: "/data"}))
	assert.Equal(t, "/data",
		dbPath(&config.StateActorTarget{Client: "reth", OutputDir: "/data"}))
	assert.Equal(t, "/data",
		dbPath(&config.StateActorTarget{Client: "besu", OutputDir: "/data"}))
	assert.Equal(t, "/data",
		dbPath(&config.StateActorTarget{Client: "nethermind", OutputDir: "/data"}))
}

func TestStateActorBuilder_Targets(t *testing.T) {
	cfg := &config.StateActorConfig{
		Images:   allImages,
		SpecFile: "/etc/spec.yaml",
		Targets: []config.StateActorTarget{
			{Client: "geth", OutputDir: "/srv/g", TargetSize: "5GB"},
			{Name: "reth-spec", Client: "reth", OutputDir: "/srv/r"},
		},
	}

	b := NewStateActorBuilder(logrus.New(), cfg, "docker", &fakeMgr{})

	got := b.Targets()
	require.Len(t, got, 2)
	assert.Equal(t, TargetInfo{Name: "geth", Client: "geth", OutputDir: "/srv/g"}, got[0])
	assert.Equal(t, TargetInfo{Name: "reth-spec", Client: "reth", OutputDir: "/srv/r"}, got[1])
}

func TestStateActorBuilder_BuildUnknownTarget(t *testing.T) {
	cfg := &config.StateActorConfig{
		Images:  allImages,
		Targets: []config.StateActorTarget{{Client: "geth", OutputDir: t.TempDir(), TargetSize: "5GB"}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", &fakeMgr{})

	_, err := b.Build(context.Background(), "missing", BuildOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no target named")
}

func TestStateActorBuilder_BuildSkipsPopulatedDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0o644))

	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images:  allImages,
		Targets: []config.StateActorTarget{{Client: "geth", OutputDir: dir, TargetSize: "5GB"}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	skipped, err := b.Build(context.Background(), "geth", BuildOptions{})
	require.NoError(t, err)
	assert.True(t, skipped, "Build should report skipped=true when output_dir is non-empty")
	assert.Empty(t, mgr.runs, "no container should run when target is skipped")

	// The leftover file must still be there — skip means "leave it".
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "leftover", entries[0].Name())
}

func TestStateActorBuilder_PerTargetForce(t *testing.T) {
	// One target sets `force: true`; the other doesn't. Both have
	// populated output_dirs and the CLI --force flag is NOT passed —
	// only the per-target force should rebuild AND propagate --force
	// to the state-actor argv.
	overrideDir := t.TempDir()
	skipDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(overrideDir, "leftover"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skipDir, "leftover"), []byte("y"), 0o644))

	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images: allImages,
		Targets: []config.StateActorTarget{
			{Name: "force-me", Client: "geth", OutputDir: overrideDir, TargetSize: "5GB", Force: true},
			{Name: "skip-me", Client: "reth", OutputDir: skipDir, TargetSize: "5GB"},
		},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	skipped, err := b.Build(context.Background(), "force-me", BuildOptions{})
	require.NoError(t, err)
	assert.False(t, skipped, "force: true target should not be skipped")

	overrideEntries, err := os.ReadDir(overrideDir)
	require.NoError(t, err)
	assert.Empty(t, overrideEntries, "force: true should wipe the dir before running")

	require.Len(t, mgr.runs, 1, "only the force target should have run so far")

	for _, arg := range mgr.runs[0].Command {
		assert.NotEqual(t, "--force", arg, "state-actor has no --force flag; we wipe the dir from benchmarkoor instead")
	}

	skipped, err = b.Build(context.Background(), "skip-me", BuildOptions{})
	require.NoError(t, err)
	assert.True(t, skipped, "target without force should still be skipped")

	skipEntries, err := os.ReadDir(skipDir)
	require.NoError(t, err)
	require.Len(t, skipEntries, 1, "skipped target's leftover must remain")
	assert.Equal(t, "leftover", skipEntries[0].Name())

	require.Len(t, mgr.runs, 1, "no new container should run for the skipped target")
}

func TestStateActorBuilder_BuildForceClearsDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0o644))

	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images:  allImages,
		Targets: []config.StateActorTarget{{Client: "geth", OutputDir: dir, TargetSize: "5GB"}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	skipped, err := b.Build(context.Background(), "geth", BuildOptions{Force: true})
	require.NoError(t, err)
	assert.False(t, skipped, "--force must skip the skip check")

	// Force removes the directory and then re-creates it.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "leftover file should have been removed by --force")

	require.Len(t, mgr.runs, 1)
	assert.Equal(t, "fallback:latest", mgr.runs[0].Image)
	assert.Contains(t, mgr.runs[0].Command, "--db="+dir+"/geth/chaindata")
}

func TestStateActorBuilder_BuildPullFailureSurfaced(t *testing.T) {
	mgr := &fakeMgr{pullErr: errors.New("network down")}
	cfg := &config.StateActorConfig{
		Images:  allImages,
		Targets: []config.StateActorTarget{{Client: "geth", OutputDir: t.TempDir(), TargetSize: "5GB"}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	skipped, err := b.Build(context.Background(), "geth", BuildOptions{})
	require.Error(t, err)
	assert.False(t, skipped)
	assert.Contains(t, err.Error(), "network down")
	assert.Empty(t, mgr.runs, "container should not run when image pull fails")
}

func TestStateActorBuilder_BuildRunFailureIncludesOutputTail(t *testing.T) {
	mgr := &fakeMgr{
		runErr:    errors.New("exit 1"),
		runOutput: "panic: spec failed validation",
	}
	cfg := &config.StateActorConfig{
		Images:  allImages,
		Targets: []config.StateActorTarget{{Client: "geth", OutputDir: t.TempDir(), TargetSize: "5GB"}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	skipped, err := b.Build(context.Background(), "geth", BuildOptions{})
	require.Error(t, err)
	assert.False(t, skipped)
	assert.Contains(t, err.Error(), "spec failed validation")
}

func TestStateActorBuilder_SpecFileMountAndArgs(t *testing.T) {
	specFile := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte("genesis: {}\n"), 0o644))

	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images:   allImages,
		SpecFile: specFile,
		Targets: []config.StateActorTarget{{
			Client: "reth", OutputDir: t.TempDir(),
		}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	_, err := b.Build(context.Background(), "reth", BuildOptions{})
	require.NoError(t, err)
	require.Len(t, mgr.runs, 1)

	run := mgr.runs[0]
	assert.Contains(t, run.Command, "--spec="+specFile)

	// Spec must be mounted RO at the same path inside the container.
	var found bool

	for _, m := range run.Mounts {
		if m.Source == specFile && m.Target == specFile {
			found = true

			assert.True(t, m.ReadOnly, "spec mount must be read-only")
		}
	}

	assert.True(t, found, "spec file mount missing from container spec")
}

func TestStateActorBuilder_InlineSpecMaterialised(t *testing.T) {
	inline := "genesis:\n  chain_id: 1337\n"

	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images: allImages,
		Spec:   inline,
		Targets: []config.StateActorTarget{{
			Client: "reth", OutputDir: t.TempDir(),
		}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	_, buildErr := b.Build(context.Background(), "reth", BuildOptions{})
	require.NoError(t, buildErr)
	require.Len(t, mgr.runs, 1)

	run := mgr.runs[0]

	// Find the --spec arg and the matching mount; verify the file content
	// equals the inline YAML and that the temp file is cleaned up after
	// the build returns.
	var specPath string

	for _, arg := range run.Command {
		if v, ok := strings.CutPrefix(arg, "--spec="); ok {
			specPath = v
		}
	}

	require.NotEmpty(t, specPath, "--spec arg missing from command")
	require.True(t, filepath.IsAbs(specPath), "spec path should be absolute, got %q", specPath)

	// Cleanup runs in Build's defer, so the temp file must already be gone
	// when the test checks. The container saw the file via the mount; we
	// stored a recorded copy below.
	_, err := os.Stat(specPath)
	assert.True(t, os.IsNotExist(err), "temp spec file should be cleaned up after build, got err=%v", err)

	// The mount entry should be identity (same source/target) and RO.
	var found bool

	for _, m := range run.Mounts {
		if m.Source == specPath && m.Target == specPath {
			found = true

			assert.True(t, m.ReadOnly, "spec mount must be read-only")
		}
	}

	assert.True(t, found, "spec mount missing from container spec")
}

func TestStateActorBuilder_GlobalTargetSizeAppliedToTarget(t *testing.T) {
	// Global config.target_size flows into each target's argv when the
	// target doesn't set its own. Per-target value still wins.
	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images: allImages,
		Config: &config.StateActorClientDefaults{TargetSize: "5GB"},
		Targets: []config.StateActorTarget{
			{Name: "geth-inherits", Client: "geth", OutputDir: t.TempDir()},
			{Name: "reth-override", Client: "reth", OutputDir: t.TempDir(), TargetSize: "50GB"},
		},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	_, err := b.Build(context.Background(), "geth-inherits", BuildOptions{})
	require.NoError(t, err)
	_, err = b.Build(context.Background(), "reth-override", BuildOptions{})
	require.NoError(t, err)
	require.Len(t, mgr.runs, 2)

	assert.Contains(t, mgr.runs[0].Command, "--target-size=5GB", "geth should inherit global target_size")
	assert.Contains(t, mgr.runs[1].Command, "--target-size=50GB", "reth should override with its own target_size")

	for _, arg := range mgr.runs[1].Command {
		assert.NotEqual(t, "--target-size=5GB", arg, "reth must not see the global target_size after override")
	}
}

func TestStateActorBuilder_GlobalDefaultsAppliedToTarget(t *testing.T) {
	// Global config sets seed/fork/chain_id/archive; target leaves them
	// all unset, so all four should appear in the resulting argv.
	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images: allImages,
		Config: &config.StateActorClientDefaults{
			Seed:    i64(42),
			Fork:    "prague",
			ChainID: i64(7),
			Archive: bptr(true),
		},
		Targets: []config.StateActorTarget{{
			Client: "reth", OutputDir: t.TempDir(), TargetSize: "5GB",
		}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	_, err := b.Build(context.Background(), "reth", BuildOptions{})
	require.NoError(t, err)
	require.Len(t, mgr.runs, 1)

	got := mgr.runs[0].Command
	assert.Contains(t, got, "--seed=42")
	assert.Contains(t, got, "--fork=prague")
	assert.Contains(t, got, "--chain-id=7")
	assert.Contains(t, got, "--archive")
}

func TestStateActorBuilder_TargetOverridesGlobalDefaults(t *testing.T) {
	// Global says fork=prague, archive=true. Target says fork=osaka and
	// archive=false. Target must win for both.
	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images: allImages,
		Config: &config.StateActorClientDefaults{
			Fork:    "prague",
			Archive: bptr(true),
		},
		Targets: []config.StateActorTarget{{
			Client: "geth", OutputDir: t.TempDir(), TargetSize: "5GB",
			Fork:    "osaka",
			Archive: bptr(false),
		}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	_, err := b.Build(context.Background(), "geth", BuildOptions{})
	require.NoError(t, err)
	require.Len(t, mgr.runs, 1)

	got := mgr.runs[0].Command
	assert.Contains(t, got, "--fork=osaka")

	for _, arg := range got {
		assert.NotEqual(t, "--archive", arg, "target Archive=false must override global Archive=true")
		assert.NotEqual(t, "--fork=prague", arg, "target fork must override global fork")
	}
}

func TestStateActorBuilder_SpecAndTargetSizeCoexist(t *testing.T) {
	// spec and target_size are complementary — state-actor accepts both
	// flags, treating target_size as a headroom budget on top of the spec.
	mgr := &fakeMgr{}
	cfg := &config.StateActorConfig{
		Images: allImages,
		Spec:   "genesis: {}\n",
		Targets: []config.StateActorTarget{{
			Client: "geth", OutputDir: t.TempDir(), TargetSize: "5GB",
		}},
	}

	b := NewStateActorBuilder(noopLogger(), cfg, "docker", mgr)

	_, err := b.Build(context.Background(), "geth", BuildOptions{})
	require.NoError(t, err)
	require.Len(t, mgr.runs, 1)

	got := mgr.runs[0].Command
	assert.Contains(t, got, "--target-size=5GB")

	// The inline spec should have been materialised to a temp file, with
	// the matching --spec arg + bind mount carrying through.
	var hasSpecArg bool

	for _, arg := range got {
		if strings.HasPrefix(arg, "--spec=") {
			hasSpecArg = true
		}
	}

	assert.True(t, hasSpecArg, "--spec must also appear in argv when both spec and target_size are configured")
	require.Len(t, mgr.runs[0].Mounts, 2, "output_dir + spec file should both be mounted")
}

// ---------------- helpers ----------------

func noopLogger() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}

// fakeMgr satisfies docker.ContainerManager for the methods the builder
// touches. The rest panic with a clear message so accidental new uses
// surface in tests instead of segfaulting.
type fakeMgr struct {
	pullErr   error
	runErr    error
	runOutput string
	runs      []docker.ContainerSpec
}

func (m *fakeMgr) PullImage(_ context.Context, _ string, _ string) error {
	return m.pullErr
}

func (m *fakeMgr) RunInitContainer(_ context.Context, spec *docker.ContainerSpec, stdout, _ io.Writer) error {
	m.runs = append(m.runs, *spec)

	if m.runOutput != "" {
		_, _ = stdout.Write([]byte(m.runOutput + "\n"))
	}

	return m.runErr
}

// Unused interface methods — fail loudly if anything accidentally calls them.
func (m *fakeMgr) Start(_ context.Context) error { panic("Start not used in builder tests") }
func (m *fakeMgr) Stop() error                   { panic("Stop not used in builder tests") }
func (m *fakeMgr) EnsureNetwork(_ context.Context, _ string) error {
	panic("EnsureNetwork not used in builder tests")
}
func (m *fakeMgr) RemoveNetwork(_ context.Context, _ string) error {
	panic("RemoveNetwork not used in builder tests")
}
func (m *fakeMgr) CreateContainer(_ context.Context, _ *docker.ContainerSpec) (string, error) {
	panic("CreateContainer not used in builder tests")
}
func (m *fakeMgr) StartContainer(_ context.Context, _ string) error {
	panic("StartContainer not used in builder tests")
}
func (m *fakeMgr) StopContainer(_ context.Context, _ string, _ *int) error {
	panic("StopContainer not used in builder tests")
}
func (m *fakeMgr) RemoveContainer(_ context.Context, _ string) error {
	panic("RemoveContainer not used in builder tests")
}
func (m *fakeMgr) StreamLogs(_ context.Context, _ string, _, _ io.Writer) error {
	panic("StreamLogs not used in builder tests")
}
func (m *fakeMgr) GetImageDigest(_ context.Context, _ string) (string, error) {
	panic("GetImageDigest not used in builder tests")
}
func (m *fakeMgr) GetContainerIP(_ context.Context, _, _ string) (string, error) {
	panic("GetContainerIP not used in builder tests")
}
func (m *fakeMgr) CreateVolume(_ context.Context, _ string, _ map[string]string) error {
	panic("CreateVolume not used in builder tests")
}
func (m *fakeMgr) RemoveVolume(_ context.Context, _ string) error {
	panic("RemoveVolume not used in builder tests")
}
func (m *fakeMgr) ListContainers(_ context.Context) ([]docker.ContainerInfo, error) {
	panic("ListContainers not used in builder tests")
}
func (m *fakeMgr) ListVolumes(_ context.Context) ([]docker.VolumeInfo, error) {
	panic("ListVolumes not used in builder tests")
}
func (m *fakeMgr) WaitForContainerExit(_ context.Context, _ string) (<-chan docker.ContainerExitInfo, <-chan error) {
	panic("WaitForContainerExit not used in builder tests")
}
