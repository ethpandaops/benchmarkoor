package runner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
)

// fakeSchelkPromoteMgr is a docker.ContainerManager for the promote path: it
// answers the container lifecycle calls and runs the compaction container the
// way fakeDBMaintenanceMgr does. The embedded interface is nil on purpose, so a
// call to anything else panics and the test says the path grew a dependency.
type fakeSchelkPromoteMgr struct {
	docker.ContainerManager

	// compacted holds the maintenance commands, lifecycle the stop/start calls.
	compacted []string
	lifecycle []string
}

func (f *fakeSchelkPromoteMgr) RunInitContainer(
	_ context.Context, spec *docker.ContainerSpec, stdout, _ io.Writer,
) error {
	f.compacted = append(f.compacted, strings.Join(spec.Command, " "))

	_, _ = fmt.Fprintln(stdout, "fake container output")

	return nil
}

func (f *fakeSchelkPromoteMgr) StopContainer(
	_ context.Context, containerID string, _ *int,
) error {
	f.lifecycle = append(f.lifecycle, "stop "+containerID)

	return nil
}

func (f *fakeSchelkPromoteMgr) StartContainer(
	_ context.Context, containerID string,
) error {
	f.lifecycle = append(f.lifecycle, "start "+containerID)

	return nil
}

// stubRPCSpec is a real client spec with the RPC port pointed at a test server,
// so getLatestBlock reads a head the test chooses. Everything else — the
// datadir, the maintenance commands — stays the client's own.
type stubRPCSpec struct {
	client.Spec

	rpcPort int
}

func (s stubRPCSpec) RPCPort() int { return s.rpcPort }

// headServer answers eth_getBlockByNumber with one fixed block.
func headServer(t *testing.T, number uint64, hash string) (host string, port int) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{`+
				`"number":"%#x","hash":%q,"stateRoot":"0xstate"}}`, number, hash)
		},
	))
	t.Cleanup(srv.Close)

	hostPart, portPart, err := splitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)

	return hostPart, portPart
}

func splitHostPort(addr string) (string, int, error) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("no port in %q", addr)
	}

	port, err := strconv.Atoi(addr[idx+1:])
	if err != nil {
		return "", 0, err
	}

	return addr[:idx], port, nil
}

// schelkStub points the schelk binary at a script that records its arguments
// and fails. Nothing in these tests may run the real `schelk promote`, and the
// failure ends the function at a known point, just past the work under test.
func schelkStub(t *testing.T) *string {
	t.Helper()

	dir := t.TempDir()
	record := filepath.Join(dir, "calls.txt")
	script := filepath.Join(dir, "schelk")

	require.NoError(t, os.WriteFile(script, []byte(
		"#!/bin/sh\necho \"$@\" >> \""+record+"\"\nexit 1\n",
	), 0o755))

	t.Setenv(datadir.SchelkBinaryEnv, script)

	return &record
}

// promoteTestRunner wires a runner whose datadir sits at the pre-run bundle's
// end, so promoteSchelkAfterPreRuns takes the "nothing to promote" branch.
func promoteTestRunner(
	t *testing.T, mgr docker.ContainerManager, compaction *config.DBCompactionConfig,
) (*runner, *containerRunParams, client.Spec, string, string) {
	t.Helper()

	bundleDir := bundleAt(t)
	dataDir := t.TempDir()

	host, port := headServer(t, 181, "0xend")

	erigon, err := client.NewRegistry().Get(client.ClientErigon)
	require.NoError(t, err)

	spec := stubRPCSpec{Spec: erigon, rpcPort: port}

	r := runnerWithBundle(bundleDir)
	r.log = logrus.New()
	r.containerMgr = mgr
	r.cfg.FullConfig.Runner.Client.Config.DBCompaction = compaction

	instance := &config.ClientInstance{ID: "erigon-bal-full", Client: "erigon"}

	params := &containerRunParams{
		Instance:  instance,
		RunID:     "run-1",
		ImageName: "ethpandaops/erigon:main",
		ContainerSpec: &docker.ContainerSpec{
			Name: "benchmarkoor-erigon",
			Mounts: []docker.Mount{
				{Type: "bind", Source: dataDir, Target: "/data"},
			},
		},
		DataDirCfg: &config.DataDirConfig{
			Method:        "schelk",
			ContainerDir:  "/data",
			SchelkOptions: &config.SchelkOptions{PromotePostPreRuns: true},
		},
	}

	return r, params, spec, dataDir, host
}

// callPromoteSchelk runs the function under test with the log plumbing a real
// run would pass it.
func callPromoteSchelk(
	t *testing.T, r *runner, params *containerRunParams, spec client.Spec,
	containerIP, resultsDir string,
) (string, bool, error) {
	t.Helper()

	logDone := make(chan struct{})
	close(logDone)

	cancel := context.CancelFunc(func() {})
	cleanupFuncs := []func(){}

	return r.promoteSchelkAfterPreRuns(
		context.Background(), params, spec, "container-1", containerIP,
		resultsDir, nil, &cleanupFuncs, make(chan struct{}),
		&cancel, &logDone, r.log,
	)
}

// The regression behind the erigon run that compacted nothing: the compaction
// hung off the promote, so a datadir whose baseline already carried the pre-run
// state returned at "nothing to promote" and skipped a configured compaction —
// with no log line at all, because nothing that logs a skip had run.
func TestPromoteSchelkAfterPreRuns_CompactsWhenThereIsNothingToPromote(t *testing.T) {
	schelkSettleBeforeStop = 0
	t.Cleanup(func() { schelkSettleBeforeStop = 20 * time.Second })

	calls := schelkStub(t)

	mgr := &fakeSchelkPromoteMgr{}
	r, params, spec, dataDir, host := promoteTestRunner(t, mgr, &config.DBCompactionConfig{
		Enabled: true,
		When:    []string{config.DBCompactionBeforeBenchmarks},
		Timeout: "1m",
		Persist: &config.DBCompactionPersistConfig{Enabled: true},
	})

	resultsDir := t.TempDir()

	_, _, err := callPromoteSchelk(t, r, params, spec, host, resultsDir)

	// The stub schelk fails, which ends the function right after the work under
	// test. Everything below is what ran before it.
	require.ErrorContains(t, err, "schelk promote")

	assert.Equal(
		t, []string{
			"seg du --datadir=/data --verbose",
			"db compact --datadir=/data",
			"seg du --datadir=/data --verbose",
		}, mgr.compacted,
		"the datadir needs no promote, but the configured compaction must still run",
	)
	assert.Equal(t, []string{"stop container-1"}, mgr.lifecycle)

	// The promote is what writes the compacted database into the baseline; every
	// per-test `schelk restore` would otherwise discard it.
	recorded, readErr := os.ReadFile(*calls)
	require.NoError(t, readErr)
	assert.Contains(t, string(recorded), "promote")

	// The compaction reports where the run collects them.
	assert.FileExists(t, filepath.Join(
		resultsDir, config.DBCompactionResultsDir,
		config.DBCompactionBeforeBenchmarks, "compaction.json",
	))
	assert.FileExists(t, filepath.Join(dataDir, config.DBCompactionMarkerFile))
}

// A datadir the marker already covers must not cost a client recycle: the skip
// is decided before the stop, and the function returns as it always did.
func TestPromoteSchelkAfterPreRuns_MarkedDatadirNeitherCompactsNorStops(t *testing.T) {
	mgr := &fakeSchelkPromoteMgr{}
	r, params, spec, dataDir, host := promoteTestRunner(t, mgr, &config.DBCompactionConfig{
		Enabled: true,
		When:    []string{config.DBCompactionBeforeBenchmarks},
		Timeout: "1m",
		Persist: &config.DBCompactionPersistConfig{Enabled: true},
	})

	writeMarkerFor(t, dataDir, config.DBCompactionBeforeBenchmarks)

	newIP, baked, err := callPromoteSchelk(t, r, params, spec, host, t.TempDir())

	require.NoError(t, err)
	assert.Empty(t, newIP)
	assert.True(t, baked, "the baseline still carries the pre-run state")
	assert.Empty(t, mgr.compacted)
	assert.Empty(t, mgr.lifecycle, "the client is left running")
}

// Without persist the compaction cannot reach the baseline, and the first
// per-test `schelk restore` would discard it. Skip it rather than spend the
// stop, the compaction and the restart on work nothing keeps.
func TestPromoteSchelkAfterPreRuns_UnpersistedCompactionIsSkipped(t *testing.T) {
	mgr := &fakeSchelkPromoteMgr{}
	r, params, spec, _, host := promoteTestRunner(t, mgr, &config.DBCompactionConfig{
		Enabled: true,
		When:    []string{config.DBCompactionBeforeBenchmarks},
		Timeout: "1m",
	})

	newIP, baked, err := callPromoteSchelk(t, r, params, spec, host, t.TempDir())

	require.NoError(t, err)
	assert.Empty(t, newIP)
	assert.True(t, baked)
	assert.Empty(t, mgr.compacted)
	assert.Empty(t, mgr.lifecycle)
}

// A configured compaction with no datadir mount to compact is an error, not a
// silent skip: the promote would otherwise publish a baseline the operator
// believes is compacted.
func TestPromoteSchelkAfterPreRuns_MissingDatadirMountFails(t *testing.T) {
	mgr := &fakeSchelkPromoteMgr{}
	r, params, spec, _, host := promoteTestRunner(t, mgr, &config.DBCompactionConfig{
		Enabled: true,
		When:    []string{config.DBCompactionBeforeBenchmarks},
		Timeout: "1m",
		Persist: &config.DBCompactionPersistConfig{Enabled: true},
	})

	params.ContainerSpec.Mounts = nil

	_, _, err := callPromoteSchelk(t, r, params, spec, host, t.TempDir())

	require.ErrorContains(t, err, "no datadir mount to compact")
	assert.Empty(t, mgr.lifecycle)
}
