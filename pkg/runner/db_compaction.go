package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/sirupsen/logrus"
)

// dbCompactionMarkerVersion is the schema version of the marker file written
// at the root of a compacted datadir.
const dbCompactionMarkerVersion = 1

// dbCompactionRequest describes one compaction phase against a datadir whose
// client is NOT running. The caller owns the client lifecycle: the client's
// compaction command takes the database lock, so a live node makes it fail.
type dbCompactionRequest struct {
	Instance  *config.ClientInstance
	Spec      client.Spec
	Cfg       *config.DBCompactionConfig
	Phase     string
	ImageName string
	RunID     string

	// Mount is the datadir as the compaction container sees it. It is the same
	// mount the client uses, so a bind mount compacts the host path and a
	// volume mount compacts the volume.
	Mount docker.Mount

	// ResultsDir is the run results directory that receives the inspection
	// reports and the compaction report.
	ResultsDir string

	// BenchmarkoorLog receives the container output when client logs are
	// mirrored to stdout.
	BenchmarkoorLog *os.File

	// Persisting reports whether this phase's result is written back to the
	// datadir baseline. It only changes what the report and the marker record;
	// the caller performs the persist itself.
	Persisting bool

	// Head is the datadir head at compaction time, when the caller already
	// knows it. Only the before_benchmarks phase does: it stops a running
	// client, so the head is one RPC call it has already made. It is recorded
	// in the run report, never in the datadir marker.
	Head *dbCompactionHead
}

// dbCompactionHead is the datadir head at compaction time. Compaction never
// changes the chain state, so one head per phase describes both sides of it.
type dbCompactionHead struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

// dbCompactionSizes is the on-disk size of the datadir either side of a
// compaction. Absent for a datadir the runner cannot see from the host (a
// container volume).
type dbCompactionSizes struct {
	Before int64 `json:"before"`
	After  int64 `json:"after"`
}

// dbCompactionStep is one preparation command of a compaction phase, as the run
// report records it.
type dbCompactionStep struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
}

// dbCompactionReport is the per-run record of one compaction phase, written to
// <results>/db-compaction/<phase>/compaction.json.
type dbCompactionReport struct {
	Phase        string             `json:"phase"`
	Client       string             `json:"client"`
	Image        string             `json:"image"`
	RunID        string             `json:"run_id"`
	StartedAt    string             `json:"started_at"`
	CompletedAt  string             `json:"completed_at,omitempty"`
	DurationMS   int64              `json:"duration_ms"`
	Persisted    bool               `json:"persisted"`
	Prepare      []dbCompactionStep `json:"prepare,omitempty"`
	Command      []string           `json:"command"`
	DatadirBytes *dbCompactionSizes `json:"datadir_bytes,omitempty"`
	Head         *dbCompactionHead  `json:"head,omitempty"`
	Error        string             `json:"error,omitempty"`
}

// dbCompactionMarker records the compactions a datadir has already had. It
// lives at the root of the datadir, so a persisted compaction carries it into
// the baseline and a later run can skip the work.
//
// It holds only what is known the moment the compaction finishes, so it is
// written once and never patched. The head is deliberately absent: at
// before_pre_runs no client has booted to report one, and a field that is
// null in half the baselines is worse than no field.
type dbCompactionMarker struct {
	Version int                                `json:"version"`
	Phases  map[string]dbCompactionMarkerEntry `json:"phases"`
}

// dbCompactionMarkerEntry is one phase's entry in the marker file.
type dbCompactionMarkerEntry struct {
	Client string `json:"client"`
	Image  string `json:"image"`
	RunID  string `json:"run_id"`

	// Prepare names the db_compaction.prepare steps that ran, so a later run
	// that skips this phase can say HOW it was compacted, and notice that its
	// own prepare config would have done something else.
	//
	// Absent means no step ran: a marker written before prepare existed cannot
	// have had one either, so the two cases coincide.
	Prepare []string `json:"prepare,omitempty"`

	CompletedAt  string             `json:"completed_at"`
	DurationMS   int64              `json:"duration_ms"`
	DatadirBytes *dbCompactionSizes `json:"datadir_bytes,omitempty"`
}

// prepareSummary renders the entry's steps for a log line.
func (e *dbCompactionMarkerEntry) prepareSummary() string {
	if len(e.Prepare) == 0 {
		return "none"
	}

	return strings.Join(e.Prepare, ",")
}

// hostPath returns the host path of the datadir mount, or "" when
// the datadir is a container volume. The marker and the size measurements need
// a path the runner can read.
func (req *dbCompactionRequest) hostPath() string {
	if req.Mount.Type == "bind" {
		return req.Mount.Source
	}

	return ""
}

// runDBCompaction compacts the datadir for one phase, and reports whether the
// compaction actually ran — it does not when the datadir marker already names
// this phase.
//
// The client must be stopped before the call and is left stopped after it: the
// caller decides when to start it again. A failure is returned unless
// continue_on_error is set, in which case it is logged and the run goes on
// with an uncompacted database.
func (r *runner) runDBCompaction(
	ctx context.Context, req *dbCompactionRequest,
) (bool, error) {
	log := r.log.WithFields(logrus.Fields{
		"instance": req.Instance.ID,
		"phase":    req.Phase,
	})

	cmds := req.Spec.DBMaintenanceCommands(req.Mount.Target)
	if cmds == nil || len(cmds.Compact) == 0 {
		// Validation rejects this combination, so reaching it means the config
		// was never validated. Refusing beats silently skipping.
		return false, fmt.Errorf(
			"client %s has no database compaction command", req.Instance.Client,
		)
	}

	steps, err := dbCompactionSelectedSteps(cmds, req.Cfg)
	if err != nil {
		return false, err
	}

	hostPath := req.hostPath()

	if entry := r.dbCompactionSkipEntry(req.Instance, req.Phase, req.Mount); entry != nil {
		logDBCompactionSkip(log, entry, req.Cfg)

		return false, nil
	}

	phaseDir := filepath.Join(req.ResultsDir, config.DBCompactionResultsDir, req.Phase)
	if err := fsutil.MkdirAll(phaseDir, 0755, r.cfg.ResultsOwner); err != nil {
		return false, fmt.Errorf("creating db-compaction results dir: %w", err)
	}

	started := time.Now()

	report := &dbCompactionReport{
		Phase:     req.Phase,
		Client:    req.Instance.Client,
		Image:     req.ImageName,
		RunID:     req.RunID,
		StartedAt: started.UTC().Format(time.RFC3339),
		Persisted: req.Persisting,
		Prepare:   dbCompactionPrepareReport(steps),
		Command:   dbCompactionCommand(cmds, req.Cfg),
		Head:      req.Head,
	}

	if hostPath != "" {
		report.DatadirBytes = &dbCompactionSizes{Before: dirSize(hostPath)}
	}

	runErr := r.runDBCompactionContainers(ctx, req, cmds, steps, phaseDir, log)

	if hostPath != "" {
		report.DatadirBytes.After = dirSize(hostPath)
	}

	report.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	report.DurationMS = time.Since(started).Milliseconds()

	if runErr != nil {
		report.Error = runErr.Error()
	}

	r.writeDBCompactionReport(phaseDir, report, log)

	if runErr != nil {
		if req.Cfg.ContinueOnError {
			log.WithError(runErr).Warn(
				"Database compaction failed; continuing with an uncompacted database" +
					" (db_compaction.continue_on_error is set)",
			)

			return false, nil
		}

		return false, fmt.Errorf(
			"compacting the %s database: %w", req.Instance.Client, runErr,
		)
	}

	fields := logrus.Fields{"duration": time.Duration(report.DurationMS) * time.Millisecond}
	if report.DatadirBytes != nil {
		fields["bytes_before"] = report.DatadirBytes.Before
		fields["bytes_after"] = report.DatadirBytes.After
	}

	log.WithFields(fields).Info("Database compaction completed")

	if hostPath != "" {
		r.writeDBCompactionMarker(hostPath, req, report, log)
	}

	return true, nil
}

// dbCompactionSelectedSteps resolves db_compaction.prepare against the steps
// the client offers, in the order the config names them.
//
// Validation already rejects an unknown name, so reaching one here means the
// config was never validated. Refusing beats silently skipping the step the
// user asked for.
func dbCompactionSelectedSteps(
	cmds *client.DBMaintenanceCommands, cfg *config.DBCompactionConfig,
) ([]client.DBMaintenanceStep, error) {
	if len(cfg.Prepare) == 0 {
		return nil, nil
	}

	byName := make(map[string]client.DBMaintenanceStep, len(cmds.Prepare))
	for _, step := range cmds.Prepare {
		byName[step.Name] = step
	}

	steps := make([]client.DBMaintenanceStep, 0, len(cfg.Prepare))

	for _, name := range cfg.Prepare {
		step, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown db_compaction.prepare step %q", name)
		}

		steps = append(steps, step)
	}

	return steps, nil
}

// dbCompactionStepNames returns the step names of a report's prepare list, in
// order, for the datadir marker.
func dbCompactionStepNames(steps []dbCompactionStep) []string {
	if len(steps) == 0 {
		return nil
	}

	names := make([]string, 0, len(steps))
	for _, step := range steps {
		names = append(names, step.Name)
	}

	return names
}

// dbCompactionPrepareReport records the steps that run, for the run report.
func dbCompactionPrepareReport(steps []client.DBMaintenanceStep) []dbCompactionStep {
	if len(steps) == 0 {
		return nil
	}

	out := make([]dbCompactionStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, dbCompactionStep{
			Name:    step.Name,
			Command: append([]string{}, step.Args...),
		})
	}

	return out
}

// dbCompactionCommand returns the compaction argv with the configured extra
// arguments appended, without mutating the client's own slice.
func dbCompactionCommand(
	cmds *client.DBMaintenanceCommands, cfg *config.DBCompactionConfig,
) []string {
	return append(append([]string{}, cmds.Compact...), cfg.ExtraArgs...)
}

// runDBCompactionContainers runs the inspection either side of the compaction,
// the selected preparation steps, and the compaction itself, each in its own
// one-shot container.
//
// A preparation step is one the user asked for by name, so its failure fails the
// phase: the compaction that follows would not be the operation they configured.
// continue_on_error still downgrades it.
//
// An inspection failure is never fatal: it is a report, and losing it must not
// cost the run its compaction. geth 1.17.5 exits 1 on `db inspect` against a
// datadir whose freezer is empty ("unknown tail group"), which a
// freshly-initialised datadir has, so this is a case that really happens.
func (r *runner) runDBCompactionContainers(
	ctx context.Context,
	req *dbCompactionRequest,
	cmds *client.DBMaintenanceCommands,
	steps []client.DBMaintenanceStep,
	phaseDir string,
	log logrus.FieldLogger,
) error {
	ctx, cancel := context.WithTimeout(ctx, req.Cfg.EffectiveTimeout())
	defer cancel()

	if req.Cfg.InspectEnabled() && len(cmds.Inspect) > 0 {
		log.Info("Inspecting the database before compaction")

		if err := r.runDBMaintenanceContainer(
			ctx, req, "inspect-before", cmds.Inspect,
			filepath.Join(phaseDir, "inspect-before.txt"),
		); err != nil {
			log.WithError(err).Warn("Database inspection before compaction failed")
		}
	}

	// The timeout covers the whole sequence, so every step logs it: a step the
	// timeout kills names the budget it ran out of.
	timeout := req.Cfg.EffectiveTimeout()

	for i, step := range steps {
		log.WithFields(logrus.Fields{
			"step":    step.Name,
			"index":   fmt.Sprintf("%d/%d", i+1, len(steps)),
			"timeout": timeout,
		}).Info("Preparing the database for compaction")

		if err := r.runDBMaintenanceContainer(
			ctx, req, step.Name, step.Args,
			filepath.Join(phaseDir, step.Name+".log"),
		); err != nil {
			return fmt.Errorf("preparation step %q: %w", step.Name, err)
		}
	}

	log.WithField("timeout", timeout).Info("Compacting the database")

	if err := r.runDBMaintenanceContainer(
		ctx, req, "compact", dbCompactionCommand(cmds, req.Cfg),
		filepath.Join(phaseDir, "compact.log"),
	); err != nil {
		return err
	}

	if req.Cfg.InspectEnabled() && len(cmds.Inspect) > 0 {
		log.Info("Inspecting the database after compaction")

		if err := r.runDBMaintenanceContainer(
			ctx, req, "inspect-after", cmds.Inspect,
			filepath.Join(phaseDir, "inspect-after.txt"),
		); err != nil {
			log.WithError(err).Warn("Database inspection after compaction failed")
		}
	}

	return nil
}

// runDBMaintenanceContainer runs one database command in a one-shot container
// on the datadir mount and writes its output to outputFile.
//
// No resource limits are applied. The compaction is setup, not measurement,
// and a memory cap sized for the client can make it fail outright.
func (r *runner) runDBMaintenanceContainer(
	ctx context.Context,
	req *dbCompactionRequest,
	step string,
	command []string,
	outputFile string,
) error {
	name := fmt.Sprintf(
		"benchmarkoor-%s-%s-dbc-%s-%s", req.RunID, req.Instance.ID, req.Phase, step,
	)

	image := req.ImageName
	if req.Cfg.Image != "" {
		image = req.Cfg.Image
	}

	spec := &docker.ContainerSpec{
		Name:        name,
		Image:       image,
		Entrypoint:  req.Instance.Entrypoint,
		Command:     command,
		Mounts:      []docker.Mount{req.Mount},
		NetworkName: r.cfg.ContainerNetwork,
		SecurityOpt: []string{"seccomp=unconfined"},
		Labels: map[string]string{
			"benchmarkoor.instance":   req.Instance.ID,
			"benchmarkoor.client":     req.Instance.Client,
			"benchmarkoor.run-id":     req.RunID,
			"benchmarkoor.type":       "db-compaction",
			"benchmarkoor.managed-by": "benchmarkoor",
		},
	}

	out, err := fsutil.Create(outputFile, r.cfg.ResultsOwner)
	if err != nil {
		return fmt.Errorf("creating %s: %w", outputFile, err)
	}

	defer func() {
		_ = out.Close()
	}()

	_, _ = fmt.Fprintf(
		out, "# %s %s\n# %v\n\n", image, step, command,
	)

	var stdout, stderr io.Writer = out, out

	if r.cfg.ClientLogsToStdout {
		pfxFn := clientLogPrefix(req.Instance.ID + "-dbc")
		stdoutPW := &prefixedWriter{prefixFn: pfxFn, writer: os.Stdout}
		writers := []io.Writer{out, stdoutPW}

		if req.BenchmarkoorLog != nil {
			writers = append(
				writers, &prefixedWriter{prefixFn: pfxFn, writer: req.BenchmarkoorLog},
			)
		}

		stdout = io.MultiWriter(writers...)
		stderr = stdout
	}

	if err := r.containerMgr.RunInitContainer(ctx, spec, stdout, stderr); err != nil {
		return fmt.Errorf("running %s container: %w", step, err)
	}

	return nil
}

// writeDBCompactionReport writes the per-run record of one phase. A failure is
// logged, never fatal: the compaction itself already happened.
func (r *runner) writeDBCompactionReport(
	phaseDir string, report *dbCompactionReport, log logrus.FieldLogger,
) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.WithError(err).Warn("Failed to encode the db-compaction report")

		return
	}

	path := filepath.Join(phaseDir, "compaction.json")
	if err := fsutil.WriteFile(path, data, 0644, r.cfg.ResultsOwner); err != nil {
		log.WithError(err).Warn("Failed to write the db-compaction report")
	}
}

// writeDBCompactionMarker records the finished compaction at the root of the
// datadir it compacted, so a persisted baseline says what has already been
// done to it. A failure is logged, never fatal.
func (r *runner) writeDBCompactionMarker(
	hostPath string, req *dbCompactionRequest, report *dbCompactionReport,
	log logrus.FieldLogger,
) {
	marker := readDBCompactionMarker(hostPath)
	if marker == nil {
		marker = &dbCompactionMarker{
			Version: dbCompactionMarkerVersion,
			Phases:  make(map[string]dbCompactionMarkerEntry, 2),
		}
	}

	if marker.Phases == nil {
		marker.Phases = make(map[string]dbCompactionMarkerEntry, 2)
	}

	marker.Version = dbCompactionMarkerVersion
	marker.Phases[req.Phase] = dbCompactionMarkerEntry{
		Client:       req.Instance.Client,
		Image:        report.Image,
		RunID:        req.RunID,
		Prepare:      dbCompactionStepNames(report.Prepare),
		CompletedAt:  report.CompletedAt,
		DurationMS:   report.DurationMS,
		DatadirBytes: report.DatadirBytes,
	}

	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		log.WithError(err).Warn("Failed to encode the db-compaction marker")

		return
	}

	path := filepath.Join(hostPath, config.DBCompactionMarkerFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.WithError(err).Warn("Failed to write the db-compaction marker")
	}
}

// readDBCompactionMarker reads the marker at the root of a datadir. A missing
// or unreadable marker means "never compacted", which only costs a compaction
// the run would have done anyway.
func readDBCompactionMarker(hostPath string) *dbCompactionMarker {
	data, err := os.ReadFile(filepath.Join(hostPath, config.DBCompactionMarkerFile))
	if err != nil {
		return nil
	}

	var marker dbCompactionMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil
	}

	return &marker
}

// dirSize returns the total size in bytes of all regular files under dir.
// Unreadable entries are skipped: the size is a report, not a decision.
func dirSize(dir string) int64 {
	var total int64

	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.Type().IsRegular() {
			if info, infoErr := d.Info(); infoErr == nil {
				total += info.Size()
			}
		}

		return nil
	})

	return total
}

// dbCompactionFor returns the compaction config for an instance when it runs
// at the given phase, or nil.
func (r *runner) dbCompactionFor(
	instance *config.ClientInstance, phase string,
) *config.DBCompactionConfig {
	if r.cfg.FullConfig == nil {
		return nil
	}

	cfg := r.cfg.FullConfig.GetDBCompaction(instance)
	if !cfg.RunsAt(phase) {
		return nil
	}

	return cfg
}

// dbCompactionSkipEntry returns the datadir marker entry that makes a
// compaction at this phase a no-op, or nil when the compaction should run.
//
// It answers the question runDBCompaction asks itself, but without a container
// and without the client having to stop first. A caller that must stop the
// client to compact uses it to find out whether the stop is worth making at
// all: a persisted baseline carries the marker for every later run, and
// stopping and restarting a client to skip the work costs a settle, a graceful
// shutdown, and a boot, every run, for nothing.
func (r *runner) dbCompactionSkipEntry(
	instance *config.ClientInstance, phase string, mount docker.Mount,
) *dbCompactionMarkerEntry {
	cfg := r.dbCompactionFor(instance, phase)
	if cfg == nil || !cfg.SkipIfMarkedEnabled() {
		return nil
	}

	req := &dbCompactionRequest{Mount: mount}

	hostPath := req.hostPath()
	if hostPath == "" {
		return nil
	}

	marker := readDBCompactionMarker(hostPath)
	if marker == nil {
		return nil
	}

	entry, ok := marker.Phases[phase]
	if !ok {
		return nil
	}

	return &entry
}

// logDBCompactionSkip reports a phase the datadir marker already covers, and
// says how that earlier compaction ran.
//
// The marker is the only record of what the persisted baseline had done to it,
// so the skip line carries the earlier run's id, image and prepare steps: a run
// that compacts nothing itself should still be able to say how its datadir got
// that way.
//
// When the current prepare config would do something else, the line is a
// WARNING instead. That is the case the marker used to hide: the whole point of
// prepare is to change what the compaction does, so a changed setting that is
// being ignored has to say so.
func logDBCompactionSkip(
	log logrus.FieldLogger, entry *dbCompactionMarkerEntry, cfg *config.DBCompactionConfig,
) {
	fields := logrus.Fields{
		"compacted_at": entry.CompletedAt,
		"run_id":       entry.RunID,
		"image":        entry.Image,
		"prepare":      entry.prepareSummary(),
	}

	wanted := cfg.PrepareSteps()
	if !slices.Equal(wanted, entry.Prepare) {
		fields["configured_prepare"] = dbCompactionPrepareSummary(wanted)

		log.WithFields(fields).Warn(
			"Datadir already carries a compaction marker for this phase, so it is" +
				" skipped and the configured db_compaction.prepare steps do NOT" +
				" run; the datadir keeps the preparation the marker names" +
				" (set db_compaction.skip_if_marked: false to recompact)",
		)

		return
	}

	log.WithFields(fields).Info(
		"Datadir already carries a compaction marker for this phase; skipping" +
			" (set db_compaction.skip_if_marked: false to force)",
	)
}

// dbCompactionPrepareSummary renders a configured step list for a log line.
func dbCompactionPrepareSummary(steps []string) string {
	if len(steps) == 0 {
		return "none"
	}

	return strings.Join(steps, ",")
}

// dbCompactionPersistsAt reports whether the instance writes the result of the
// given phase back to the datadir baseline.
func (r *runner) dbCompactionPersistsAt(
	instance *config.ClientInstance, phase string,
) bool {
	return r.dbCompactionFor(instance, phase).PersistsAt(phase)
}

// compactDatadirForPhase compacts the datadir behind `mount` when the instance
// is configured to compact at this phase. The client must already be stopped.
//
// It reports whether a compaction actually ran, which is false both when the
// phase is not configured and when the datadir marker says the work is already
// done.
func (r *runner) compactDatadirForPhase(
	ctx context.Context,
	instance *config.ClientInstance,
	spec client.Spec,
	phase, runID, imageName, resultsDir string,
	mount docker.Mount,
	benchmarkoorLog *os.File,
	head *dbCompactionHead,
) (bool, error) {
	cfg := r.dbCompactionFor(instance, phase)
	if cfg == nil {
		return false, nil
	}

	req := &dbCompactionRequest{
		Instance:        instance,
		Spec:            spec,
		Cfg:             cfg,
		Phase:           phase,
		ImageName:       imageName,
		RunID:           runID,
		Mount:           mount,
		ResultsDir:      resultsDir,
		BenchmarkoorLog: benchmarkoorLog,
		Persisting:      cfg.PersistsAt(phase),
		Head:            head,
	}

	return r.runDBCompaction(ctx, req)
}

// compactZFSSourceForPersist compacts a ZFS source dataset in place, before it
// is snapshotted and cloned for the run.
//
// This is the only way a ZFS datadir persists a compaction: the clone is a
// CHILD of its source dataset, so there is no promote or rename that puts the
// compacted clone back at the source path. Compacting the source first also
// keeps the clone small, since a compaction inside a copy-on-write clone
// rewrites the whole database into it.
//
// The dataset is snapshotted first (unless safety_snapshot is off), so a
// `zfs rollback` undoes the whole thing.
func (r *runner) compactZFSSourceForPersist(
	ctx context.Context,
	instance *config.ClientInstance,
	spec client.Spec,
	datadirCfg *config.DataDirConfig,
	runID, imageName, resultsDir string,
	benchmarkoorLog *os.File,
) error {
	cfg := r.dbCompactionFor(instance, config.DBCompactionBeforePreRuns)
	if cfg == nil || !cfg.PersistsAt(config.DBCompactionBeforePreRuns) {
		return nil
	}

	if datadirCfg == nil || datadirCfg.Method != "zfs" {
		return nil
	}

	log := r.log.WithFields(logrus.Fields{
		"instance": instance.ID,
		"phase":    config.DBCompactionBeforePreRuns,
		"source":   datadirCfg.SourceDir,
	})

	if cfg.SafetySnapshotEnabled() {
		snapshot, err := datadir.SnapshotZFSSource(
			ctx, r.log, datadirCfg.SourceDir, "benchmarkoor-precompaction-"+runID,
		)
		if err != nil {
			return fmt.Errorf("snapshotting the source dataset before compaction: %w", err)
		}

		log.WithField("snapshot", snapshot).Info(
			"Took a safety snapshot of the source dataset before compacting it in place",
		)
	}

	containerDir := datadirCfg.ContainerDir
	if containerDir == "" {
		containerDir = spec.DataDir()
	}

	log.Warn(
		"Compacting the ZFS SOURCE dataset in place; every later run and clone" +
			" starts from the compacted database",
	)

	_, err := r.compactDatadirForPhase(
		ctx, instance, spec, config.DBCompactionBeforePreRuns,
		runID, imageName, resultsDir,
		docker.Mount{Type: "bind", Source: datadirCfg.SourceDir, Target: containerDir},
		benchmarkoorLog, nil,
	)

	return err
}

// persistCompactedSchelk makes the datadir the client is about to boot on (or
// has just stopped using) the new schelk baseline.
//
// `schelk promote` unmounts the volume, so the caller must have no client
// holding it; the mount is re-established afterwards because every later
// restore, and the container bind, resolve through that path.
func (r *runner) persistCompactedSchelk(ctx context.Context, log logrus.FieldLogger) error {
	log.Warn(
		"Persisting the compacted datadir as the new schelk baseline (`schelk promote`);" +
			" this overwrites the previous baseline irreversibly",
	)

	if err := datadir.SchelkPromote(ctx, r.log); err != nil {
		return fmt.Errorf("schelk promote: %w", err)
	}

	if err := datadir.EnsureSchelkMounted(ctx, r.log); err != nil {
		return fmt.Errorf("remounting schelk volume after promote: %w", err)
	}

	return nil
}

// stopClientForDatadirWork stops the client gracefully so an offline step can
// touch its datadir, and leaves it stopped.
//
// It is the same sequence the schelk and ZFS persist paths use, for the same
// reasons: settle so the client finishes any in-flight initialisation, stop
// with the default SIGTERM grace so it flushes its database, drain the log
// stream of the stopped container, then sync the host's dirty pages.
func (r *runner) stopClientForDatadirWork(
	ctx context.Context,
	containerID string,
	logDone *chan struct{},
	logCancel *context.CancelFunc,
	log logrus.FieldLogger,
) error {
	log.WithField("settle", schelkSettleBeforeStop).Info(
		"Waiting for client to settle before stop",
	)
	time.Sleep(schelkSettleBeforeStop)

	log.Info("Stopping client for offline datadir work (graceful)")

	if err := r.containerMgr.StopContainer(ctx, containerID, nil); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}

	waitForLogDrain(logDone, logCancel, logDrainTimeout)

	if syncErr := exec.Command("sync").Run(); syncErr != nil {
		log.WithError(syncErr).Warn("Failed to sync before offline datadir work")
	}

	return nil
}

// dbCompactionHeadFromRPC reads the datadir head for the compaction report.
// A failed read costs the report one field, never the run.
func (r *runner) dbCompactionHeadFromRPC(
	ctx context.Context, host string, port int, log logrus.FieldLogger,
) *dbCompactionHead {
	number, hash, _, err := r.getLatestBlock(ctx, host, port)
	if err != nil {
		log.WithError(err).Debug("Could not read the datadir head for the compaction report")

		return nil
	}

	return &dbCompactionHead{Number: number, Hash: hash}
}

// datadirMountFor returns the container's datadir mount from its spec, and
// whether it was found. The lookup is by target, since the datadir config may
// mount the data somewhere other than the client's default path.
func datadirMountFor(
	containerSpec *docker.ContainerSpec, spec client.Spec, dd *config.DataDirConfig,
) (docker.Mount, bool) {
	if containerSpec == nil {
		return docker.Mount{}, false
	}

	target := spec.DataDir()
	if dd != nil && dd.ContainerDir != "" {
		target = dd.ContainerDir
	}

	for _, mnt := range containerSpec.Mounts {
		if mnt.Target == target {
			return mnt, true
		}
	}

	return docker.Mount{}, false
}
