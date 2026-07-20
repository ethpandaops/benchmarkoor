package datadir

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// DefaultSchelkStatePath is the default schelk state file location.
const DefaultSchelkStatePath = "/var/lib/schelk/state.json"

// SchelkBinaryEnv overrides the schelk executable used by the provider
// and by config preflight. Useful when running under sudo with a
// sanitized PATH that does not include ~/.cargo/bin or wherever
// `schelk` is installed.
const SchelkBinaryEnv = "BENCHMARKOOR_SCHELK_BIN"

// DefaultSchelkBinary is the binary name used when SchelkBinaryEnv is unset.
const DefaultSchelkBinary = "schelk"

// SchelkGracePeriod is the wall-clock budget granted to an in-flight
// schelk command after the caller context is cancelled (typically by
// SIGTERM). Killing schelk mid-recover/restore leaves dm-era and
// state.json out of sync — usually only recoverable via
// `schelk full-recover` — so we wait for it to finish naturally rather
// than yanking it.
//
// var (not const) so tests can shorten it.
var SchelkGracePeriod = 60 * time.Second

// SchelkBinary returns the schelk executable path: SchelkBinaryEnv if
// set, otherwise the unqualified binary name (resolved via PATH).
func SchelkBinary() string {
	if p := os.Getenv(SchelkBinaryEnv); p != "" {
		return p
	}

	return DefaultSchelkBinary
}

// RunSchelk invokes the schelk binary with the given args. Schelk is
// launched in its own process group (Setpgid) so a SIGTERM delivered
// to benchmarkoor's group is not propagated, and the parent ctx is
// only used to start a SchelkGracePeriod countdown — schelk is allowed
// to finish for up to SchelkGracePeriod after ctx is cancelled before
// it is killed. log may be nil.
//
// Returns combined stdout+stderr and the exec error (if any).
func RunSchelk(ctx context.Context, log logrus.FieldLogger, args ...string) ([]byte, error) {
	return runSchelkWithGrace(ctx, log, SchelkGracePeriod, args...)
}

func runSchelkWithGrace(
	ctx context.Context,
	log logrus.FieldLogger,
	grace time.Duration,
	args ...string,
) ([]byte, error) {
	bin := SchelkBinary()

	//nolint:gosec // bin resolves to a controlled binary name or env override.
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf bytes.Buffer

	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting `%s %s`: %w", bin, strings.Join(args, " "), err)
	}

	waitErrCh := make(chan error, 1)
	go func() { waitErrCh <- cmd.Wait() }()

	select {
	case waitErr := <-waitErrCh:
		return buf.Bytes(), waitErr
	case <-ctx.Done():
		if log != nil {
			log.WithFields(logrus.Fields{
				"args":  args,
				"grace": grace,
			}).Warn("Context cancelled; waiting for schelk to finish before killing")
		}
	}

	select {
	case waitErr := <-waitErrCh:
		return buf.Bytes(), waitErr
	case <-time.After(grace):
		// schelk is the leader of its own process group (Setpgid above),
		// so signal the entire group to also bring down any child it
		// forked (e.g. dmsetup, rsync). Without this, cmd.Wait() blocks
		// on the stdout pipe held open by grandchildren.
		if killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); killErr != nil && log != nil {
			log.WithError(killErr).Warn("Failed to kill schelk process group after grace period")
		}

		<-waitErrCh

		return buf.Bytes(), fmt.Errorf(
			"`%s %s` did not complete within %s after shutdown signal",
			bin, strings.Join(args, " "), grace,
		)
	}
}

// SchelkProvider implements Provider by binding to a schelk-managed mount.
//
// schelk maintains a single scratch mount at a fixed mount point, restored
// from a pristine virgin baseline via `schelk recover`. Unlike the ZFS or
// copy providers, Prepare does not produce an isolated clone per call —
// instead it verifies that the schelk scratch volume is mounted (mounting
// it if necessary) and returns the configured source_dir, which must live
// under the schelk mount point. Cleanup runs `schelk recover` to restore
// the scratch volume to baseline.
//
// Pairing with rollback_strategy:
//   - container-recreate: Prepare/Cleanup runs per iteration, so recover
//     happens between every test.
//   - none / rpc-debug-setHead: Prepare/Cleanup runs once per container
//     lifecycle, so recover happens at end of run.
type SchelkProvider interface {
	Provider
}

// NewSchelkProvider creates a new schelk-backed datadir provider.
func NewSchelkProvider(log logrus.FieldLogger) SchelkProvider {
	return &schelkProvider{
		log: log.WithField("component", "datadir-schelk"),
	}
}

// SchelkState mirrors the fields we care about from the schelk state file
// (see /var/lib/schelk/state.json). schelk owns the schema; we only
// inspect mount_point to validate the source path.
type SchelkState struct {
	MountPoint string `json:"mount_point"`
	IsMounted  bool   `json:"is_mounted"`
}

type schelkProvider struct {
	log logrus.FieldLogger
}

// Ensure interface compliance.
var _ SchelkProvider = (*schelkProvider)(nil)

// Prepare guarantees the schelk scratch volume is at the virgin baseline
// and mounted before returning. It always runs `schelk restore` (recover
// + mount) so a prior dirty or inconsistent state from a crashed run
// cannot leak into this benchmark. cfg.SourceDir must be the schelk
// mount point or a subdirectory of it.
func (p *schelkProvider) Prepare(ctx context.Context, cfg *ProviderConfig) (*PreparedDir, error) {
	state, err := ReadSchelkState(SchelkStatePath())
	if err != nil {
		return nil, fmt.Errorf("reading schelk state: %w", err)
	}

	if state.MountPoint == "" {
		return nil, fmt.Errorf("schelk state has no mount_point — run `schelk init-new` or `schelk init-from` first")
	}

	sourceAbs, err := filepath.Abs(cfg.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolving source_dir: %w", err)
	}

	if !pathIsUnderMountpoint(sourceAbs, state.MountPoint) {
		return nil, fmt.Errorf(
			"source_dir %q is not under schelk mount point %q",
			sourceAbs, state.MountPoint,
		)
	}

	p.log.WithFields(logrus.Fields{
		"mount_point": state.MountPoint,
		"instance_id": cfg.InstanceID,
	}).Info("Running `schelk restore` to ensure baseline + mounted")

	// `restore` = recover (surgical block restore from virgin) + mount.
	// Idempotent and fast when already at baseline. If the state is
	// inconsistent (e.g. post-crash), schelk will surface the error and
	// the user must run `schelk full-recover` once before retrying.
	if err := p.runSchelk(ctx, "restore", "-y"); err != nil {
		return nil, fmt.Errorf("schelk restore: %w", err)
	}

	// Sanity-check the mount actually appeared. Cheap insurance against
	// silent schelk-side regressions.
	mounted, err := IsMountedAt(state.MountPoint)
	if err != nil {
		return nil, fmt.Errorf("checking mount status: %w", err)
	}

	if !mounted {
		return nil, fmt.Errorf(
			"schelk restore reported success but %q is not mounted",
			state.MountPoint,
		)
	}

	p.log.WithFields(logrus.Fields{
		"mount_point": state.MountPoint,
		"source_dir":  sourceAbs,
		"instance_id": cfg.InstanceID,
	}).Info("schelk datadir ready")

	return &PreparedDir{
		MountPath: sourceAbs,
		Cleanup: func() error {
			return p.cleanup(state.MountPoint)
		},
	}, nil
}

// cleanup runs `schelk recover` to restore the scratch volume to the
// virgin baseline. Uses background context so cleanup completes even
// when the caller's context has been cancelled.
func (p *schelkProvider) cleanup(mountPoint string) error {
	p.log.WithField("mount_point", mountPoint).Info("Running `schelk recover` to restore baseline")

	if err := p.runSchelk(context.Background(), "recover", "-y"); err != nil {
		return fmt.Errorf("schelk recover: %w", err)
	}

	return nil
}

// runSchelk invokes the schelk binary with the given args, isolating
// it from benchmarkoor's process group and giving it up to
// SchelkGracePeriod to finish after ctx is cancelled.
func (p *schelkProvider) runSchelk(ctx context.Context, args ...string) error {
	output, err := RunSchelk(ctx, p.log, args...)
	if err != nil {
		return fmt.Errorf("`%s %s` failed: %w (output: %s)",
			SchelkBinary(), strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	p.log.WithFields(logrus.Fields{
		"args":   args,
		"output": strings.TrimSpace(string(output)),
	}).Debug("schelk command succeeded")

	return nil
}

// SchelkStatePath returns the state-file path, honouring SCHELK_STATE
// to match schelk's own behaviour.
func SchelkStatePath() string {
	if p := os.Getenv("SCHELK_STATE"); p != "" {
		return p
	}

	return DefaultSchelkStatePath
}

// ReadSchelkState reads and decodes the schelk JSON state file.
func ReadSchelkState(path string) (*SchelkState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("schelk state file %q not found — run `schelk init-new` or `schelk init-from` first", path)
		}

		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	var s SchelkState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}

	return &s, nil
}

// SchelkDir reports whether outputDir lives under a configured schelk mount,
// returning the mount point when it does. When schelk is not set up (no state
// file) it returns isSchelk=false with no error, so callers that don't use
// schelk are unaffected. A malformed/unreadable existing state file is an error.
func SchelkDir(outputDir string) (mountPoint string, isSchelk bool, err error) {
	statePath := SchelkStatePath()
	if _, statErr := os.Stat(statePath); errors.Is(statErr, os.ErrNotExist) {
		return "", false, nil
	}

	state, err := ReadSchelkState(statePath)
	if err != nil {
		return "", false, err
	}

	if state.MountPoint == "" {
		return "", false, nil
	}

	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return "", false, fmt.Errorf("resolving output_dir %q: %w", outputDir, err)
	}

	return state.MountPoint, pathIsUnderMountpoint(abs, state.MountPoint), nil
}

// EnsureSchelkMounted makes sure the schelk scratch volume is mounted, running
// `schelk mount` when it is not. It errors when schelk's recorded state and the
// kernel disagree (a crash artefact that only `schelk full-recover` resolves).
func EnsureSchelkMounted(ctx context.Context, log logrus.FieldLogger) error {
	bin := SchelkBinary()

	state, err := ReadSchelkState(SchelkStatePath())
	if err != nil {
		return err
	}

	if state.MountPoint == "" {
		return fmt.Errorf("schelk state has no mount_point — run `schelk init-new` or `schelk init-from` first")
	}

	mounted, err := IsMountedAt(state.MountPoint)
	if err != nil {
		return fmt.Errorf("checking mount status: %w", err)
	}

	switch {
	case state.IsMounted && !mounted:
		// schelk state says mounted but the kernel disagrees — typically a
		// crash or interrupted recover left dm-era and state out of sync.
		// `schelk mount` will refuse ("Volume is already mounted"); only
		// `schelk full-recover` can re-establish a consistent baseline.
		return fmt.Errorf(
			"schelk state at %q says is_mounted=true but %q is not in /proc/mounts "+
				"(inconsistent, likely from a prior crash) — run `%s full-recover` to reset, then retry",
			SchelkStatePath(), state.MountPoint, bin,
		)
	case !mounted:
		output, runErr := RunSchelk(ctx, log, "mount", "-y")
		if runErr != nil {
			return fmt.Errorf("`%s mount` failed: %w (output: %s)",
				bin, runErr, strings.TrimSpace(string(output)))
		}
	}

	return nil
}

// RestoreSchelk restores the schelk scratch volume to the virgin baseline AND
// mounts it, via `schelk restore` (= recover + mount). Unlike EnsureSchelkMounted
// (mount only), it discards any dirty scratch state first, so a caller that
// advances the volume always starts from a clean baseline. `restore` also
// repairs an inconsistent post-crash state that plain `mount` would refuse, so
// no separate consistency pre-check is needed. It errors if the mount does not
// appear afterwards.
func RestoreSchelk(ctx context.Context, log logrus.FieldLogger) error {
	bin := SchelkBinary()

	state, err := ReadSchelkState(SchelkStatePath())
	if err != nil {
		return err
	}

	if state.MountPoint == "" {
		return fmt.Errorf("schelk state has no mount_point — run `schelk init-new` or `schelk init-from` first")
	}

	output, err := RunSchelk(ctx, log, "restore", "-y")
	if err != nil {
		return fmt.Errorf("`%s restore` failed: %w (output: %s)",
			bin, err, strings.TrimSpace(string(output)))
	}

	mounted, err := IsMountedAt(state.MountPoint)
	if err != nil {
		return fmt.Errorf("checking mount status: %w", err)
	}

	if !mounted {
		return fmt.Errorf("`%s restore` reported success but %q is not mounted", bin, state.MountPoint)
	}

	return nil
}

// SchelkPromote persists the current scratch contents as the new virgin
// baseline via `schelk promote`, so subsequent recover/restore reset to it.
func SchelkPromote(ctx context.Context, log logrus.FieldLogger) error {
	output, err := RunSchelk(ctx, log, "promote", "-y")
	if err != nil {
		return fmt.Errorf("`%s promote` failed: %w (output: %s)",
			SchelkBinary(), err, strings.TrimSpace(string(output)))
	}

	return nil
}

// IsMountedAt reports whether the given mount point appears as a mount
// in /proc/mounts. The schelk state's is_mounted flag is unreliable
// after a crash, so we consult the kernel directly.
func IsMountedAt(mountPoint string) (bool, error) {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return false, fmt.Errorf("opening /proc/mounts: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == mountPoint {
			return true, nil
		}
	}

	return false, scanner.Err()
}
