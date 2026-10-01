package iocost

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCgroup creates io.cost files with the given content. The files are
// plain files, so they hold only the last line that was written.
func fakeCgroup(t *testing.T, model, qos string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, modelFile), []byte(model), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, qosFile), []byte(qos), 0o600))

	return dir
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)

	return string(data)
}

func newTestManager(t *testing.T, cgroupPath string) (Manager, string) {
	t.Helper()

	log := logrus.New()
	log.SetOutput(&discard{})
	cacheDir := t.TempDir()

	return NewManager(log, cacheDir, cgroupPath), cacheDir
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestModelString(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		want  string
	}{
		{
			name:  "all limits",
			model: Model{ReadBps: 100, ReadIOPS: 10, WriteBps: 200, WriteIOPS: 20},
			want:  "ctrl=user model=linear rbps=100 rseqiops=10 rrandiops=10 wbps=200 wseqiops=20 wrandiops=20",
		},
		{
			name:  "reads only, writes free",
			model: Model{ReadBps: 3573547008, ReadIOPS: 630000},
			want: "ctrl=user model=linear rbps=3573547008 rseqiops=630000 rrandiops=630000 " +
				"wbps=107374182400 wseqiops=10000000 wrandiops=10000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.model.String())
		})
	}
}

func TestApplyAndRestoreUnconfiguredDevice(t *testing.T) {
	ctx := context.Background()
	dir := fakeCgroup(t, "8:0 ctrl=auto model=linear rbps=1\n", "8:0 enable=0 ctrl=auto\n")
	mgr, cacheDir := newTestManager(t, dir)

	line, err := mgr.Apply(ctx, "259:0", Model{ReadBps: 1000, ReadIOPS: 100})
	require.NoError(t, err)
	assert.Equal(t, "259:0 "+line, readFile(t, dir, modelFile))
	assert.Equal(t, "259:0 "+qosFixedRate, readFile(t, dir, qosFile))

	files, err := ListOrphanedStateFiles(cacheDir)
	require.NoError(t, err)
	require.Len(t, files, 1, "Apply must leave a state file for crash recovery")

	require.NoError(t, mgr.Restore(ctx))
	assert.Equal(t, "259:0 enable=0 ctrl=auto", readFile(t, dir, qosFile))
	assert.Equal(t, "259:0 ctrl=auto", readFile(t, dir, modelFile))

	files, err = ListOrphanedStateFiles(cacheDir)
	require.NoError(t, err)
	assert.Empty(t, files, "Restore must remove the state file")
}

func TestRestoreUserSettings(t *testing.T) {
	ctx := context.Background()
	origModel := "ctrl=user model=linear rbps=1 rseqiops=2 rrandiops=3 wbps=4 wseqiops=5 wrandiops=6"
	origQoS := "enable=1 ctrl=user rpct=95.00 rlat=5000 wpct=95.00 wlat=5000 min=50.00 max=150.00"
	dir := fakeCgroup(t, "259:0 "+origModel+"\n", "259:0 "+origQoS+"\n")
	mgr, _ := newTestManager(t, dir)

	_, err := mgr.Apply(ctx, "259:0", Model{ReadIOPS: 100})
	require.NoError(t, err)

	// A second Apply must not replace the saved originals with its own model.
	_, err = mgr.Apply(ctx, "259:0", Model{ReadIOPS: 200})
	require.NoError(t, err)

	require.NoError(t, mgr.Restore(ctx))
	assert.Equal(t, "259:0 "+origModel, readFile(t, dir, modelFile))
	assert.Equal(t, "259:0 "+origQoS, readFile(t, dir, qosFile))
}

func TestRestoreSettings(t *testing.T) {
	tests := []struct {
		name      string
		orig      DeviceSettings
		wantQoS   string
		wantModel string
	}{
		{
			name:      "no settings",
			orig:      DeviceSettings{MajMin: "259:0"},
			wantQoS:   "259:0 enable=0 ctrl=auto",
			wantModel: "259:0 ctrl=auto",
		},
		{
			name: "automatic settings, enabled",
			orig: DeviceSettings{
				MajMin: "259:0",
				Model:  "ctrl=auto model=linear rbps=1",
				QoS:    "enable=1 ctrl=auto rpct=0.00 rlat=250 wpct=0.00 wlat=250 min=1.00 max=10000.00",
			},
			wantQoS:   "259:0 enable=1 ctrl=auto",
			wantModel: "259:0 ctrl=auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := fakeCgroup(t, "", "")
			require.NoError(t, restoreSettings(dir, tt.orig))
			assert.Equal(t, tt.wantQoS, readFile(t, dir, qosFile))
			assert.Equal(t, tt.wantModel, readFile(t, dir, modelFile))
		})
	}
}

func TestCleanupOrphanedState(t *testing.T) {
	ctx := context.Background()
	origModel := "ctrl=user model=linear rbps=1 rseqiops=2 rrandiops=3 wbps=4 wseqiops=5 wrandiops=6"
	dir := fakeCgroup(t, "259:0 "+origModel+"\n", "259:0 enable=0 ctrl=user\n")
	mgr, cacheDir := newTestManager(t, dir)

	// The run stops without a Restore, as after a crash.
	_, err := mgr.Apply(ctx, "259:0", Model{ReadIOPS: 100})
	require.NoError(t, err)

	files, err := ListOrphanedStateFiles(cacheDir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	require.NoError(t, CleanupOrphanedState(ctx, logrus.New(), files))
	assert.Equal(t, "259:0 "+origModel, readFile(t, dir, modelFile))
	assert.Equal(t, "259:0 enable=0 ctrl=user", readFile(t, dir, qosFile))

	files, err = ListOrphanedStateFiles(cacheDir)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestStartRestoresOrphanedState(t *testing.T) {
	ctx := context.Background()
	dir := fakeCgroup(t, "", "")
	crashed, cacheDir := newTestManager(t, dir)

	_, err := crashed.Apply(ctx, "259:0", Model{ReadIOPS: 100})
	require.NoError(t, err)

	log := logrus.New()
	log.SetOutput(&discard{})
	mgr := NewManager(log, cacheDir, dir)
	require.NoError(t, mgr.Start(ctx))

	assert.Equal(t, "259:0 enable=0 ctrl=auto", readFile(t, dir, qosFile))
	assert.Equal(t, "259:0 ctrl=auto", readFile(t, dir, modelFile))

	files, err := ListOrphanedStateFiles(cacheDir)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestApplyFailsWithoutIOCost(t *testing.T) {
	mgr, _ := newTestManager(t, t.TempDir())

	_, err := mgr.Apply(context.Background(), "259:0", Model{ReadIOPS: 100})
	require.Error(t, err)
}

func TestApplyForgetsRejectedDevice(t *testing.T) {
	ctx := context.Background()
	dir := fakeCgroup(t, "", "")
	mgr, cacheDir := newTestManager(t, dir)

	// A read-only model file stands for a device that the kernel rejects.
	// Root can write it anyway.
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only file")
	}

	require.NoError(t, os.Chmod(filepath.Join(dir, modelFile), 0o400))

	_, err := mgr.Apply(ctx, "253:0", Model{ReadIOPS: 100})
	require.Error(t, err)

	files, err := ListOrphanedStateFiles(cacheDir)
	require.NoError(t, err)
	assert.Empty(t, files, "a rejected device must not leave a state file")
	require.NoError(t, mgr.Restore(ctx), "Restore must not touch the rejected device")
}

func TestIsSupportedAndWriteAccess(t *testing.T) {
	assert.False(t, IsSupported(t.TempDir()))

	dir := fakeCgroup(t, "", "")
	assert.True(t, IsSupported(dir))
	require.NoError(t, HasWriteAccess(dir))
}

func TestParamValue(t *testing.T) {
	line := "enable=1 ctrl=user rpct=0.00 min=100.00"
	assert.Equal(t, "1", paramValue(line, "enable"))
	assert.Equal(t, "user", paramValue(line, "ctrl"))
	assert.Equal(t, "", paramValue(line, "max"))
}
