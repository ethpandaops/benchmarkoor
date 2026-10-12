package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
		Cfg:      &config.DBCompactionConfig{ExtraArgs: []string{"--cache=16384"}},
		Phase:    config.DBCompactionBeforePreRuns,
		RunID:    "run-1",
	}
	report := &dbCompactionReport{
		Image:        "ethereum/client-go:stable",
		CompletedAt:  "2026-08-27T10:12:03Z",
		DurationMS:   4200,
		DatadirBytes: &dbCompactionSizes{Before: 200, After: 100},
		Prepare:      []dbCompactionStep{{Name: "seg-retire"}},
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

	// The settings that decided what the compaction did are recorded, so a later
	// run that skips this phase can say how the datadir was compacted.
	assert.Equal(t, []string{"seg-retire"}, entry.Prepare)
	assert.Equal(t, []string{"--cache=16384"}, entry.ExtraArgs)

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
	assert.Empty(t, cmds.Prepare, "geth compacts in one command")
	assert.Equal(t, []string{"db", "compact", "--datadir=/var/lib/geth"}, cmds.Compact)
	assert.Equal(t, []string{"db", "inspect", "--datadir=/var/lib/geth"}, cmds.Inspect)

	assert.True(t, client.SupportsDBCompaction(client.ClientGeth))
}

// TestErigonDBMaintenanceCommands pins the exact argv verified against
// erigon 3.7.0-dev, and that the retire is offered but not part of the
// compaction unless the config selects it.
func TestErigonDBMaintenanceCommands(t *testing.T) {
	spec, err := client.NewRegistry().Get(client.ClientErigon)
	require.NoError(t, err)

	cmds := spec.DBMaintenanceCommands("/var/lib/erigon")
	require.NotNil(t, cmds)

	assert.Equal(t, []string{"db", "compact", "--datadir=/var/lib/erigon"}, cmds.Compact)
	assert.Equal(
		t, []string{"seg", "du", "--datadir=/var/lib/erigon", "--verbose"}, cmds.Inspect,
	)

	// `seg retire` is offered, never run unless db_compaction.prepare says so.
	require.Len(t, cmds.Prepare, 1)
	assert.Equal(t, "seg-retire", cmds.Prepare[0].Name)
	assert.Equal(
		t, []string{"seg", "retire", "--datadir=/var/lib/erigon"}, cmds.Prepare[0].Args,
	)
	assert.NotEmpty(t, cmds.Prepare[0].Why, "validation prints this to the user")

	assert.True(t, client.SupportsDBCompaction(client.ClientErigon))
}

func TestSupportsDBCompaction_UnsupportedClients(t *testing.T) {
	for _, other := range []client.ClientType{
		client.ClientBesu, client.ClientNethermind,
		client.ClientReth, client.ClientNimbus, client.ClientEthrex,
	} {
		assert.False(t, client.SupportsDBCompaction(other), string(other))
	}
}

// TestDBCompactionCommand checks that extra_args land on the compaction and
// that the client's own slice is not mutated.
func TestDBCompactionCommand(t *testing.T) {
	cmds := &client.DBMaintenanceCommands{
		Compact: []string{"db", "compact", "--datadir=/data"},
	}

	got := dbCompactionCommand(cmds, &config.DBCompactionConfig{
		ExtraArgs: []string{"--cache=16384"},
	})

	assert.Equal(
		t, []string{"db", "compact", "--datadir=/data", "--cache=16384"}, got,
	)
	assert.Equal(t, []string{"db", "compact", "--datadir=/data"}, cmds.Compact)
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("12345"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "b"), []byte("123"), 0644))

	assert.Equal(t, int64(8), dirSize(dir))
	assert.Equal(t, int64(0), dirSize(filepath.Join(dir, "missing")))
}

// runnerWithDBCompaction builds a runner whose global db_compaction config is
// cfg, for the marker-skip tests below.
func runnerWithDBCompaction(cfg *config.DBCompactionConfig) *runner {
	return &runner{cfg: &Config{FullConfig: &config.Config{
		Runner: config.RunnerConfig{
			Client: config.ClientConfig{
				Config: config.ClientDefaults{DBCompaction: cfg},
			},
		},
	}}}
}

// writeMarkerFor drops a marker naming phase at the root of dir.
func writeMarkerFor(t *testing.T, dir, phase string) {
	t.Helper()

	marker := dbCompactionMarker{
		Version: dbCompactionMarkerVersion,
		Phases: map[string]dbCompactionMarkerEntry{
			phase: {Client: "geth", RunID: "earlier-run", CompletedAt: "2026-08-27T10:12:03Z"},
		},
	}

	data, err := json.Marshal(marker)
	require.NoError(t, err)
	require.NoError(
		t, os.WriteFile(filepath.Join(dir, config.DBCompactionMarkerFile), data, 0644),
	)
}

// A persisted baseline carries this phase's marker into every later run. The
// skip has to be decided from the marker alone, because the callers that must
// stop the client to compact consult it BEFORE stopping — see
// prepareDatadirBeforeBenchmarks. Deciding later would recycle the client on
// every run for a compaction that does nothing.
func TestDBCompactionSkipEntry(t *testing.T) {
	phase := config.DBCompactionBeforeBenchmarks
	instance := &config.ClientInstance{ID: "geth", Client: "geth"}

	persisting := func() *config.DBCompactionConfig {
		return &config.DBCompactionConfig{
			Enabled: true,
			When:    []string{phase},
			Persist: &config.DBCompactionPersistConfig{Enabled: true},
		}
	}

	t.Run("marked phase on a persisting config skips", func(t *testing.T) {
		dir := t.TempDir()
		writeMarkerFor(t, dir, phase)

		r := runnerWithDBCompaction(persisting())
		mount := docker.Mount{Type: "bind", Source: dir, Target: "/data"}

		entry := r.dbCompactionSkipEntry(instance, phase, mount)
		require.NotNil(t, entry)
		assert.Equal(t, "earlier-run", entry.RunID)
	})

	t.Run("no marker runs the compaction", func(t *testing.T) {
		r := runnerWithDBCompaction(persisting())
		mount := docker.Mount{Type: "bind", Source: t.TempDir(), Target: "/data"}

		assert.Nil(t, r.dbCompactionSkipEntry(instance, phase, mount))
	})

	t.Run("a marker for another phase runs the compaction", func(t *testing.T) {
		dir := t.TempDir()
		writeMarkerFor(t, dir, config.DBCompactionBeforePreRuns)

		r := runnerWithDBCompaction(persisting())
		mount := docker.Mount{Type: "bind", Source: dir, Target: "/data"}

		assert.Nil(t, r.dbCompactionSkipEntry(instance, phase, mount))
	})

	t.Run("without persist the marker cannot describe this datadir", func(t *testing.T) {
		dir := t.TempDir()
		writeMarkerFor(t, dir, phase)

		r := runnerWithDBCompaction(&config.DBCompactionConfig{
			Enabled: true,
			When:    []string{phase},
		})
		mount := docker.Mount{Type: "bind", Source: dir, Target: "/data"}

		assert.Nil(t, r.dbCompactionSkipEntry(instance, phase, mount))
	})

	t.Run("skip_if_marked false forces the compaction", func(t *testing.T) {
		dir := t.TempDir()
		writeMarkerFor(t, dir, phase)

		force := false
		cfg := persisting()
		cfg.SkipIfMarked = &force

		r := runnerWithDBCompaction(cfg)
		mount := docker.Mount{Type: "bind", Source: dir, Target: "/data"}

		assert.Nil(t, r.dbCompactionSkipEntry(instance, phase, mount))
	})

	t.Run("a volume datadir has no marker to read", func(t *testing.T) {
		r := runnerWithDBCompaction(persisting())
		mount := docker.Mount{Type: "volume", Source: "benchmarkoor-vol", Target: "/data"}

		assert.Nil(t, r.dbCompactionSkipEntry(instance, phase, mount))
	})

	t.Run("a phase that is not configured never skips", func(t *testing.T) {
		dir := t.TempDir()
		writeMarkerFor(t, dir, config.DBCompactionBeforePreRuns)

		r := runnerWithDBCompaction(persisting())
		mount := docker.Mount{Type: "bind", Source: dir, Target: "/data"}

		assert.Nil(
			t, r.dbCompactionSkipEntry(instance, config.DBCompactionBeforePreRuns, mount),
		)
	})
}

// fakeDBMaintenanceMgr is a docker.ContainerManager that only implements the
// one call the compaction makes. The embedded interface is nil on purpose: a
// call to anything else panics, which is what makes the test tell us if the
// compaction path grows a dependency it should not have.
type fakeDBMaintenanceMgr struct {
	docker.ContainerManager

	// failCommand fails any container whose command starts with this word.
	failCommand string

	ran []string
}

func (f *fakeDBMaintenanceMgr) RunInitContainer(
	_ context.Context, spec *docker.ContainerSpec, stdout, _ io.Writer,
) error {
	f.ran = append(f.ran, strings.Join(spec.Command, " "))

	_, _ = fmt.Fprintln(stdout, "fake container output")

	if f.failCommand != "" && len(spec.Command) > 0 && spec.Command[0] == f.failCommand {
		return fmt.Errorf("exit status 1")
	}

	return nil
}

// dbCompactionTestRunner wires a runner with a fake container manager and a
// temporary results dir, for the phases that only need the command sequence.
func dbCompactionTestRunner(
	t *testing.T, mgr docker.ContainerManager,
) (*runner, string) {
	t.Helper()

	resultsDir := t.TempDir()

	return &runner{
		log:          logrus.New(),
		cfg:          &Config{ResultsDir: resultsDir},
		containerMgr: mgr,
	}, resultsDir
}

func dbCompactionTestRequest(resultsDir string) *dbCompactionRequest {
	return &dbCompactionRequest{
		Instance:   &config.ClientInstance{ID: "erigon", Client: "erigon"},
		Cfg:        &config.DBCompactionConfig{Enabled: true, Timeout: "1m"},
		Phase:      config.DBCompactionBeforeBenchmarks,
		ImageName:  "erigontech/erigon:main-latest",
		RunID:      "run-1",
		Mount:      docker.Mount{Type: "bind", Source: resultsDir, Target: "/data"},
		ResultsDir: resultsDir,
	}
}

// TestRunDBCompactionContainers_Sequence pins the order of a whole phase, that
// extra_args reach only the compaction, and that each step writes its own log.
func TestRunDBCompactionContainers_Sequence(t *testing.T) {
	mgr := &fakeDBMaintenanceMgr{}
	r, resultsDir := dbCompactionTestRunner(t, mgr)

	cmds := &client.DBMaintenanceCommands{
		Compact: []string{"db", "compact"},
		Inspect: []string{"seg", "du"},
	}

	req := dbCompactionTestRequest(resultsDir)
	req.Cfg.ExtraArgs = []string{"--cache=16384"}

	require.NoError(t, r.runDBCompactionContainers(
		context.Background(), req, cmds, nil, resultsDir, r.log,
	))

	assert.Equal(t, []string{
		"seg du", "db compact --cache=16384", "seg du",
	}, mgr.ran)

	for _, name := range []string{
		"inspect-before.txt", "compact.log", "inspect-after.txt",
	} {
		assert.FileExists(t, filepath.Join(resultsDir, name))
	}
}

// TestRunDBCompactionContainers_CompactionFails checks that a failed compaction
// fails the phase, while a failed inspection does not.
func TestRunDBCompactionContainers_CompactionFails(t *testing.T) {
	t.Run("compaction failure fails the phase", func(t *testing.T) {
		mgr := &fakeDBMaintenanceMgr{failCommand: "db"}
		r, resultsDir := dbCompactionTestRunner(t, mgr)

		err := r.runDBCompactionContainers(
			context.Background(), dbCompactionTestRequest(resultsDir),
			&client.DBMaintenanceCommands{Compact: []string{"db", "compact"}},
			nil, resultsDir, r.log,
		)
		require.Error(t, err)
	})

	t.Run("inspection failure does not", func(t *testing.T) {
		mgr := &fakeDBMaintenanceMgr{failCommand: "seg"}
		r, resultsDir := dbCompactionTestRunner(t, mgr)

		err := r.runDBCompactionContainers(
			context.Background(), dbCompactionTestRequest(resultsDir),
			&client.DBMaintenanceCommands{
				Compact: []string{"db", "compact"},
				Inspect: []string{"seg", "du"},
			},
			nil, resultsDir, r.log,
		)
		require.NoError(t, err)
		assert.Equal(t, []string{"seg du", "db compact", "seg du"}, mgr.ran)
	})
}

// TestDBCompactionSelectedSteps covers the opt-in: nothing runs unless
// db_compaction.prepare names it, and the config's order wins.
func TestDBCompactionSelectedSteps(t *testing.T) {
	cmds := &client.DBMaintenanceCommands{
		Prepare: []client.DBMaintenanceStep{
			{Name: "seg-retire", Args: []string{"seg", "retire"}},
			{Name: "other", Args: []string{"other"}},
		},
		Compact: []string{"db", "compact"},
	}

	t.Run("no prepare selects nothing", func(t *testing.T) {
		steps, err := dbCompactionSelectedSteps(cmds, &config.DBCompactionConfig{})
		require.NoError(t, err)
		assert.Empty(t, steps)
	})

	t.Run("selects in the configured order", func(t *testing.T) {
		steps, err := dbCompactionSelectedSteps(cmds, &config.DBCompactionConfig{
			Prepare: []string{"other", "seg-retire"},
		})
		require.NoError(t, err)
		require.Len(t, steps, 2)
		assert.Equal(t, "other", steps[0].Name)
		assert.Equal(t, "seg-retire", steps[1].Name)
	})

	t.Run("an unknown name is refused", func(t *testing.T) {
		_, err := dbCompactionSelectedSteps(cmds, &config.DBCompactionConfig{
			Prepare: []string{"nope"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nope")
	})
}

// TestRunDBCompactionContainers_PrepareStep runs a selected step and checks the
// order, its own log, and that its failure fails the phase — the user asked for
// it by name, so the compaction alone is not what they configured.
func TestRunDBCompactionContainers_PrepareStep(t *testing.T) {
	cmds := &client.DBMaintenanceCommands{
		Prepare: []client.DBMaintenanceStep{
			{Name: "seg-retire", Args: []string{"seg", "retire"}},
		},
		Compact: []string{"db", "compact"},
	}
	steps := cmds.Prepare

	t.Run("runs before the compaction", func(t *testing.T) {
		mgr := &fakeDBMaintenanceMgr{}
		r, resultsDir := dbCompactionTestRunner(t, mgr)

		require.NoError(t, r.runDBCompactionContainers(
			context.Background(), dbCompactionTestRequest(resultsDir),
			cmds, steps, resultsDir, r.log,
		))

		assert.Equal(t, []string{"seg retire", "db compact"}, mgr.ran)
		assert.FileExists(t, filepath.Join(resultsDir, "seg-retire.log"))
	})

	t.Run("its failure fails the phase", func(t *testing.T) {
		mgr := &fakeDBMaintenanceMgr{failCommand: "seg"}
		r, resultsDir := dbCompactionTestRunner(t, mgr)

		err := r.runDBCompactionContainers(
			context.Background(), dbCompactionTestRequest(resultsDir),
			cmds, steps, resultsDir, r.log,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `preparation step "seg-retire"`)
		assert.Equal(t, []string{"seg retire"}, mgr.ran, "the compaction must not run")
	})
}

func TestDBCompactionPrepareReport(t *testing.T) {
	assert.Nil(t, dbCompactionPrepareReport(nil))

	got := dbCompactionPrepareReport([]client.DBMaintenanceStep{
		{Name: "seg-retire", Args: []string{"seg", "retire"}},
	})
	assert.Equal(t, []dbCompactionStep{
		{Name: "seg-retire", Command: []string{"seg", "retire"}},
	}, got)
}

// TestLogDBCompactionSkip covers the two shapes of the skip line: an INFO that
// says how the datadir was compacted, and a WARNING naming the settings this run
// configured that are therefore not going to take effect.
func TestLogDBCompactionSkip(t *testing.T) {
	entry := &dbCompactionMarkerEntry{
		Client:      "erigon",
		Image:       "ethpandaops/erigon:main",
		RunID:       "run-1",
		CompletedAt: "2026-09-09T20:21:21Z",
		Prepare:     []string{"seg-retire"},
		ExtraArgs:   []string{"--cache=16384"},
	}

	capture := func(e *dbCompactionMarkerEntry, cfg *config.DBCompactionConfig) *logrus.Entry {
		log := logrus.New()
		log.SetOutput(io.Discard)

		hook := &captureHook{}
		log.AddHook(hook)

		logDBCompactionSkip(logrus.NewEntry(log), e, cfg)

		require.Len(t, hook.entries, 1)

		return hook.entries[0]
	}

	t.Run("matching config reports how it ran", func(t *testing.T) {
		got := capture(entry, &config.DBCompactionConfig{
			Prepare:   []string{"seg-retire"},
			ExtraArgs: []string{"--cache=16384"},
		})

		assert.Equal(t, logrus.InfoLevel, got.Level)
		assert.Equal(t, "seg-retire", got.Data["prepare"])
		assert.Equal(t, "--cache=16384", got.Data["extra_args"])
		assert.Equal(t, "run-1", got.Data["run_id"])
		assert.Equal(t, "ethpandaops/erigon:main", got.Data["image"])
		assert.NotContains(t, got.Data, "changed")
	})

	t.Run("a changed prepare warns", func(t *testing.T) {
		got := capture(entry, &config.DBCompactionConfig{
			ExtraArgs: []string{"--cache=16384"},
		})

		assert.Equal(t, logrus.WarnLevel, got.Level)
		assert.Equal(t, "prepare: seg-retire -> none", got.Data["changed"])
		assert.Contains(t, got.Message, "do NOT take effect")
	})

	t.Run("changed extra_args warn", func(t *testing.T) {
		got := capture(entry, &config.DBCompactionConfig{
			Prepare:   []string{"seg-retire"},
			ExtraArgs: []string{"--cache=32768"},
		})

		assert.Equal(t, logrus.WarnLevel, got.Level)
		assert.Equal(t, "extra_args: --cache=16384 -> --cache=32768", got.Data["changed"])
	})

	t.Run("both changed are reported together", func(t *testing.T) {
		got := capture(entry, &config.DBCompactionConfig{})

		assert.Equal(t, logrus.WarnLevel, got.Level)
		assert.Equal(
			t,
			"prepare: seg-retire -> none; extra_args: --cache=16384 -> none",
			got.Data["changed"],
		)
	})

	t.Run("a settings-only change does not warn", func(t *testing.T) {
		// timeout, inspect, continue_on_error and friends govern the run, not
		// the bytes, so they must not read as a stale datadir.
		got := capture(entry, &config.DBCompactionConfig{
			Prepare:         []string{"seg-retire"},
			ExtraArgs:       []string{"--cache=16384"},
			Timeout:         "9h",
			ContinueOnError: true,
		})

		assert.Equal(t, logrus.InfoLevel, got.Level)
	})

	t.Run("an older marker with no settings reads as none", func(t *testing.T) {
		got := capture(
			&dbCompactionMarkerEntry{RunID: "run-0", CompletedAt: "2026-09-09T00:00:00Z"},
			&config.DBCompactionConfig{},
		)

		assert.Equal(t, logrus.InfoLevel, got.Level)
		assert.Equal(t, "none", got.Data["prepare"])
		assert.Equal(t, "none", got.Data["extra_args"])
	})
}

// captureHook collects the entries a logrus logger emits, so a test can assert
// on the level and fields of a single log line.
type captureHook struct {
	entries []*logrus.Entry
}

func (h *captureHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *captureHook) Fire(e *logrus.Entry) error {
	h.entries = append(h.entries, e)

	return nil
}

func TestDBCompactionStepNames(t *testing.T) {
	assert.Nil(t, dbCompactionStepNames(nil))
	assert.Equal(t, []string{"a", "b"}, dbCompactionStepNames([]dbCompactionStep{
		{Name: "a"}, {Name: "b"},
	}))
}

// TestDBCompactionRequestImage pins what the report and the marker record: the
// image that actually ran the compaction, which db_compaction.image overrides.
// Recording the instance image there would name a build that never ran.
func TestDBCompactionRequestImage(t *testing.T) {
	req := &dbCompactionRequest{
		ImageName: "ethpandaops/erigon:main",
		Cfg:       &config.DBCompactionConfig{},
	}
	assert.Equal(t, "ethpandaops/erigon:main", req.image())

	req.Cfg.Image = "erigontech/erigon:v3.7.0"
	assert.Equal(t, "erigontech/erigon:v3.7.0", req.image())

	req.Cfg = nil
	assert.Equal(t, "ethpandaops/erigon:main", req.image())
}

// TestDBCompactionMarkerRecordsTheOverriddenImage is the same fact end to end:
// the datadir marker names the image that compacted it.
func TestDBCompactionMarkerRecordsTheOverriddenImage(t *testing.T) {
	dir := t.TempDir()

	r := &runner{log: logrus.New(), cfg: &Config{}}

	req := &dbCompactionRequest{
		Instance:  &config.ClientInstance{ID: "erigon", Client: "erigon"},
		ImageName: "ethpandaops/erigon:main",
		Cfg:       &config.DBCompactionConfig{Image: "erigontech/erigon:v3.7.0"},
		Phase:     config.DBCompactionBeforePreRuns,
		RunID:     "run-1",
	}

	r.writeDBCompactionMarker(
		dir, req, &dbCompactionReport{Image: req.image()}, r.log,
	)

	marker := readDBCompactionMarker(dir)
	require.NotNil(t, marker)

	entry := marker.Phases[config.DBCompactionBeforePreRuns]
	assert.Equal(t, "erigontech/erigon:v3.7.0", entry.Image)
}
