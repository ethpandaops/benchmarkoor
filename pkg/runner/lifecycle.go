package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/blocklog"
	"github.com/ethpandaops/benchmarkoor/pkg/builder"
	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/ethpandaops/benchmarkoor/pkg/genesis"
	"github.com/ethpandaops/benchmarkoor/pkg/podman"
	"github.com/ethpandaops/benchmarkoor/pkg/version"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/sirupsen/logrus"
)

// runContainerLifecycle runs a single container lifecycle: load genesis,
// create container, start, wait for RPC, execute tests, stop.
//
//nolint:gocognit,cyclop // Container lifecycle is inherently complex.
func (r *runner) runContainerLifecycle(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	datadirCfg *config.DataDirConfig,
	useDataDir bool,
) error {
	instance := params.Instance
	runID := params.RunID
	runResultsDir := params.RunResultsDir
	benchmarkoorLogFile := params.BenchmarkoorLog
	genesisSource := params.GenesisSource

	log := r.log.WithFields(logrus.Fields{
		"instance": instance.ID,
		"run_id":   runID,
	})

	if params.GenesisGroupHash != "" {
		log = log.WithField("genesis_group", params.GenesisGroupHash)
	}

	// Each container lifecycle manages its own cleanup and crash detection.
	var localCleanupFuncs []func()

	localCleanupStarted := make(chan struct{})

	var localCleanupOnce sync.Once

	defer func() {
		localCleanupOnce.Do(func() { close(localCleanupStarted) })

		for i := len(localCleanupFuncs) - 1; i >= 0; i-- {
			localCleanupFuncs[i]()
		}
	}()

	// Setup data directory: either container volume or copied datadir.
	// Each container lifecycle gets a fresh volume/datadir.
	var dataMount docker.Mount

	if useDataDir {
		log.WithFields(logrus.Fields{
			"source": datadirCfg.SourceDir,
			"method": datadirCfg.Method,
		}).Info("Using pre-populated data directory")

		provider, err := datadir.NewProvider(log, datadirCfg.Method)
		if err != nil {
			return fmt.Errorf("creating datadir provider: %w", err)
		}

		prepared, err := provider.Prepare(ctx, &datadir.ProviderConfig{
			SourceDir:  datadirCfg.SourceDir,
			InstanceID: instance.ID,
			TmpDir:     r.cfg.TmpDataDir,
		})
		if err != nil {
			return fmt.Errorf("preparing datadir: %w", err)
		}

		localCleanupFuncs = append(localCleanupFuncs, func() {
			if cleanupErr := prepared.Cleanup(); cleanupErr != nil {
				log.WithError(cleanupErr).Warn("Failed to cleanup datadir")
			}
		})

		containerDir := datadirCfg.ContainerDir
		if containerDir == "" {
			containerDir = spec.DataDir()
		}

		dataMount = docker.Mount{
			Type:   "bind",
			Source: prepared.MountPath,
			Target: containerDir,
		}

		// Surface any state-actor provenance dropped at the snapshot root
		// (state-actor-manifest.json + its content-addressed spec sidecar) into
		// the run output under .state-actor/ so the UI can render the State Actor
		// Configuration. Read from the original source dir (the manifest lives
		// there, not necessarily in the copied/overlaid datadir).
		copyStateActorFiles(log, datadirCfg.SourceDir, runResultsDir, r.cfg.ResultsOwner)
	} else if r.cfg.FullConfig != nil &&
		r.cfg.FullConfig.GetRollbackStrategy(instance) == config.RollbackStrategyCheckpointRestore {
		// Checkpoint-restore without a pre-populated datadir uses a bind
		// mount to a host temp directory so the copy-based rollback
		// manager can snapshot and rsync the data between tests.
		tmpDir, mkErr := os.MkdirTemp("", fmt.Sprintf("benchmarkoor-cpdata-%s-", instance.ID))
		if mkErr != nil {
			return fmt.Errorf("creating temp dir for checkpoint-restore data: %w", mkErr)
		}

		localCleanupFuncs = append(localCleanupFuncs, func() {
			if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
				log.WithError(rmErr).Warn("Failed to remove checkpoint data temp dir")
			}
		})

		log.WithField("path", tmpDir).Info(
			"Using bind mount for checkpoint-restore (no datadir)",
		)

		dataMount = docker.Mount{
			Type:   "bind",
			Source: tmpDir,
			Target: spec.DataDir(),
		}
	} else {
		volumeSuffix := instance.ID
		if params.GenesisGroupHash != "" {
			volumeSuffix = instance.ID + "-" + params.GenesisGroupHash
		}

		volumeName := fmt.Sprintf("benchmarkoor-%s-%s", runID, volumeSuffix)
		volumeLabels := map[string]string{
			"benchmarkoor.instance":   instance.ID,
			"benchmarkoor.client":     instance.Client,
			"benchmarkoor.run-id":     runID,
			"benchmarkoor.managed-by": "benchmarkoor",
		}

		if err := r.containerMgr.CreateVolume(
			ctx, volumeName, volumeLabels,
		); err != nil {
			return fmt.Errorf("creating volume: %w", err)
		}

		localCleanupFuncs = append(localCleanupFuncs, func() {
			if rmErr := r.containerMgr.RemoveVolume(
				context.Background(), volumeName,
			); rmErr != nil {
				log.WithError(rmErr).Warn("Failed to remove volume")
			}
		})

		dataMount = docker.Mount{
			Type:   "volume",
			Source: volumeName,
			Target: spec.DataDir(),
		}
	}

	// Load genesis file if configured.
	var genesisContent []byte

	if genesisSource != "" {
		log.WithField("source", genesisSource).Info("Loading genesis file")

		var loadErr error

		genesisContent, loadErr = r.loadFile(ctx, genesisSource)
		if loadErr != nil {
			return fmt.Errorf("loading genesis: %w", loadErr)
		}
	} else {
		log.Info("No genesis configured, skipping genesis setup")
	}

	// Apply per-instance genesis fork-time overrides (the genesis-file
	// equivalent of geth's --override.<fork> for clients that read forks from
	// the genesis, e.g. besu/reth/ethrex).
	if len(instance.GenesisForkOverride) > 0 {
		if len(genesisContent) == 0 {
			return fmt.Errorf(
				"instance %s sets genesis_fork_override but has no genesis file",
				instance.ID,
			)
		}

		patched, overrideErr := genesis.ApplyForkOverrides(
			genesisContent, instance.GenesisForkOverride,
		)
		if overrideErr != nil {
			return fmt.Errorf(
				"applying genesis fork override for %s: %w", instance.ID, overrideErr,
			)
		}

		genesisContent = patched

		log.WithField("forks", instance.GenesisForkOverride).
			Info("Applied genesis fork-time overrides")
	}

	// Apply per-instance genesis EIP overrides (parity/nethermind-format
	// chainspecs schedule forks per-EIP rather than by fork name).
	if instance.GenesisEIPOverride != nil && len(instance.GenesisEIPOverride.EIPs) > 0 {
		if len(genesisContent) == 0 {
			return fmt.Errorf(
				"instance %s sets genesis_eip_override but has no genesis file",
				instance.ID,
			)
		}

		patched, overrideErr := genesis.ApplyEIPOverrides(
			genesisContent, instance.GenesisEIPOverride.Timestamp, instance.GenesisEIPOverride.EIPs,
		)
		if overrideErr != nil {
			return fmt.Errorf(
				"applying genesis eip override for %s: %w", instance.ID, overrideErr,
			)
		}

		genesisContent = patched

		log.WithFields(logrus.Fields{
			"eips":      instance.GenesisEIPOverride.EIPs,
			"timestamp": instance.GenesisEIPOverride.Timestamp,
		}).Info("Applied genesis EIP-time overrides")
	}

	// Fail if neither genesis nor datadir is configured.
	if genesisSource == "" && !useDataDir {
		return fmt.Errorf(
			"no genesis file or datadir configured for client %s",
			instance.Client,
		)
	}

	// Image is already pulled by RunInstance; use the resolved name and digest.
	imageName := params.ImageName
	imageDigest := params.ImageDigest

	// Create temp files for genesis and JWT.
	tempDir, err := os.MkdirTemp(
		r.cfg.CacheDir, "benchmarkoor-"+instance.ID+"-",
	)
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	localCleanupFuncs = append(localCleanupFuncs, func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove temp directory")
		}
	})

	// Write genesis file to temp dir if genesis is configured.
	var genesisFile string

	if genesisSource != "" {
		genesisFile = filepath.Join(tempDir, "genesis.json")
		if err := os.WriteFile(genesisFile, genesisContent, 0644); err != nil {
			return fmt.Errorf("writing genesis file: %w", err)
		}
	}

	jwtFile := filepath.Join(tempDir, "jwtsecret")
	if err := os.WriteFile(jwtFile, []byte(r.cfg.JWT), 0644); err != nil {
		return fmt.Errorf("writing jwt file: %w", err)
	}

	// Build container mounts.
	mounts := []docker.Mount{
		dataMount,
		{
			Type:     "bind",
			Source:   jwtFile,
			Target:   spec.JWTPath(),
			ReadOnly: true,
		},
	}

	// Add genesis mount if genesis is configured.
	if genesisFile != "" {
		mounts = append(mounts, docker.Mount{
			Type:     "bind",
			Source:   genesisFile,
			Target:   spec.GenesisPath(),
			ReadOnly: true,
		})
	}

	// Mount default config files from the client spec.
	for target, content := range spec.DefaultConfigFiles() {
		cfgFile := filepath.Join(tempDir, filepath.Base(target))
		if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing config file %s: %w", target, err)
		}

		mounts = append(mounts, docker.Mount{
			Type:     "bind",
			Source:   cfgFile,
			Target:   target,
			ReadOnly: true,
		})
	}

	// Run init container if required (skip when using datadir or no genesis).
	if spec.RequiresInit() && !useDataDir && genesisSource != "" {
		log.Info("Running init container")

		initSuffix := "init"
		if params.GenesisGroupHash != "" {
			initSuffix = "init-" + params.GenesisGroupHash
		}

		initSpec := &docker.ContainerSpec{
			Name: fmt.Sprintf(
				"benchmarkoor-%s-%s-%s", runID, instance.ID, initSuffix,
			),
			Image:       imageName,
			Command:     spec.InitCommand(),
			Mounts:      mounts,
			NetworkName: r.cfg.ContainerNetwork,
			SecurityOpt: []string{"seccomp=unconfined"},
			Labels: map[string]string{
				"benchmarkoor.instance":   instance.ID,
				"benchmarkoor.client":     instance.Client,
				"benchmarkoor.run-id":     runID,
				"benchmarkoor.type":       "init",
				"benchmarkoor.managed-by": "benchmarkoor",
			},
		}

		// Set up init container log streaming (appends to container.log).
		initLogFile := filepath.Join(runResultsDir, "container.log")

		initFile, err := os.OpenFile(
			initLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
		)
		if err != nil {
			return fmt.Errorf("opening init log file: %w", err)
		}

		if r.cfg.ResultsOwner != nil {
			fsutil.Chown(initLogFile, r.cfg.ResultsOwner)
		}

		_, _ = fmt.Fprint(initFile, formatStartMarker("INIT_CONTAINER", &containerLogInfo{
			Name:             initSpec.Name,
			Image:            initSpec.Image,
			GenesisGroupHash: params.GenesisGroupHash,
		}))

		var initStdout, initStderr io.Writer = initFile, initFile
		if r.cfg.ClientLogsToStdout {
			pfxFn := clientLogPrefix(instance.ID + "-init")
			stdoutPrefixWriter := &prefixedWriter{
				prefixFn: pfxFn, writer: os.Stdout,
			}
			logFilePrefixWriter := &prefixedWriter{
				prefixFn: pfxFn, writer: benchmarkoorLogFile,
			}
			initStdout = io.MultiWriter(
				initFile, stdoutPrefixWriter, logFilePrefixWriter,
			)
			initStderr = io.MultiWriter(
				initFile, stdoutPrefixWriter, logFilePrefixWriter,
			)
		}

		if err := r.containerMgr.RunInitContainer(
			ctx, initSpec, initStdout, initStderr,
		); err != nil {
			_, _ = fmt.Fprintf(initFile, "#INIT_CONTAINER:END\n")
			_ = initFile.Close()

			return fmt.Errorf("running init container: %w", err)
		}

		_, _ = fmt.Fprintf(initFile, "#INIT_CONTAINER:END\n")
		_ = initFile.Close()

		log.Info("Init container completed")
	} else if spec.RequiresInit() && genesisSource == "" {
		log.Info("Skipping init container (no genesis configured)")
	} else if useDataDir {
		log.Info("Skipping init container (using pre-populated datadir)")
	}

	// Determine command.
	cmd := make([]string, len(instance.Command))
	copy(cmd, instance.Command)

	if len(cmd) == 0 {
		cmd = spec.DefaultCommand()
	}

	// Add genesis flag if genesis is configured and client uses a genesis flag.
	if genesisSource != "" && spec.GenesisFlag() != "" {
		cmd = append(cmd, spec.GenesisFlag()+spec.GenesisPath())
	}

	// Append extra args if provided, replacing any base args that share a flag prefix.
	if len(instance.ExtraArgs) > 0 {
		// Build set of flag prefixes from extra_args (e.g. "--config=" from "--config=mainnet.cfg").
		prefixes := make([]string, 0, len(instance.ExtraArgs))
		for _, arg := range instance.ExtraArgs {
			if idx := strings.Index(arg, "="); idx != -1 {
				prefixes = append(prefixes, arg[:idx+1])
			}
		}

		// Remove any existing args that share a prefix with an extra arg.
		if len(prefixes) > 0 {
			filtered := make([]string, 0, len(cmd))
			for _, c := range cmd {
				override := false
				for _, p := range prefixes {
					if strings.HasPrefix(c, p) {
						override = true

						break
					}
				}

				if !override {
					filtered = append(filtered, c)
				}
			}

			cmd = filtered
		}

		cmd = append(cmd, instance.ExtraArgs...)
	}

	// Build environment (default first, instance overrides).
	env := make(
		map[string]string,
		len(spec.DefaultEnvironment())+len(instance.Environment),
	)
	for k, v := range spec.DefaultEnvironment() {
		env[k] = v
	}

	for k, v := range instance.Environment {
		env[k] = v
	}

	// When using checkpoint-restore, apply CRIU-compatibility env overrides.
	if r.cfg.FullConfig != nil &&
		r.cfg.FullConfig.GetRollbackStrategy(instance) == config.RollbackStrategyCheckpointRestore {
		// Disable MPTCP so CRIU can checkpoint TCP sockets. CRIU does not
		// support MPTCP (protocol 262) and recent Go versions enable it
		// by default.
		if _, ok := env["GODEBUG"]; !ok {
			env["GODEBUG"] = "multipathtcp=0"
		}

		if client.ClientType(instance.Client) == client.ClientNethermind {
			// NLog's autoReload uses FileSystemWatcher (inotify) on the
			// overlay rootfs. CRIU cannot dump inotify watches on overlayfs
			// (open_by_handle_at fails). Extract NLog.config from the image,
			// patch autoReload to false, and bind-mount the patched copy so
			// NLog never creates the FileSystemWatcher.
			cpMgr, ok := r.containerMgr.(podman.CheckpointManager)
			if ok {
				nlogContent, nlogErr := cpMgr.ReadFileFromImage(
					ctx, imageName, "/nethermind/NLog.config",
				)
				if nlogErr != nil {
					log.WithError(nlogErr).Warn(
						"Failed to extract NLog.config from image",
					)
				} else {
					patched := strings.Replace(
						string(nlogContent),
						`autoReload="true"`,
						`autoReload="false"`,
						1,
					)

					nlogFile := filepath.Join(tempDir, "NLog.config")
					if writeErr := os.WriteFile(
						nlogFile, []byte(patched), 0644,
					); writeErr != nil {
						log.WithError(writeErr).Warn(
							"Failed to write patched NLog.config",
						)
					} else {
						mounts = append(mounts, docker.Mount{
							Type:     "bind",
							Source:   nlogFile,
							Target:   "/nethermind/NLog.config",
							ReadOnly: true,
						})

						log.Info(
							"Bind-mounted patched NLog.config " +
								"(autoReload=false) for CRIU compatibility",
						)
					}
				}
			}
		}
	}

	// Resolve drop_memory_caches setting.
	var dropMemoryCaches string
	if r.cfg.FullConfig != nil {
		dropMemoryCaches = r.cfg.FullConfig.GetDropMemoryCaches(instance)
	}

	// Resolve resource limits.
	var containerResourceLimits *docker.ResourceLimits
	var resolvedResourceLimits *ResolvedResourceLimits
	var targetCPUs []int // CPUs to apply cpu_freq settings to

	if r.cfg.FullConfig != nil {
		resourceLimitsCfg := r.cfg.FullConfig.GetResourceLimits(instance)
		if resourceLimitsCfg != nil {
			var err error

			containerResourceLimits, resolvedResourceLimits, err =
				buildContainerResourceLimits(resourceLimitsCfg)
			if err != nil {
				return fmt.Errorf("building resource limits: %w", err)
			}

			fields := logrus.Fields{
				"cpuset_cpus":   resolvedResourceLimits.CpusetCpus,
				"memory":        resolvedResourceLimits.Memory,
				"swap_disabled": resolvedResourceLimits.SwapDisabled,
			}

			if resolvedResourceLimits.BlkioConfig != nil {
				fields["blkio_read_bps_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceReadBps)
				fields["blkio_write_bps_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceWriteBps)
				fields["blkio_read_iops_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceReadIOps)
				fields["blkio_write_iops_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceWriteIOps)
			}

			log.WithFields(fields).Info("Resource limits configured")

			// Determine target CPUs for cpu_freq settings.
			// Use the resolved cpuset if available.
			if resolvedResourceLimits.CpusetCpus != "" {
				for _, cpuStr := range strings.Split(resolvedResourceLimits.CpusetCpus, ",") {
					if cpuID, err := strconv.Atoi(strings.TrimSpace(cpuStr)); err == nil {
						targetCPUs = append(targetCPUs, cpuID)
					}
				}
			}

			// Apply CPU frequency settings if configured.
			if r.cpufreqMgr != nil && hasCPUFreqSettings(resourceLimitsCfg) {
				cpufreqCfg := buildCPUFreqConfig(resourceLimitsCfg)

				if err := r.cpufreqMgr.Apply(ctx, cpufreqCfg, targetCPUs); err != nil {
					return fmt.Errorf("applying CPU frequency settings: %w", err)
				}

				// Log CPU frequency info.
				logCPUFreqInfo(log, r.cpufreqMgr, targetCPUs)

				// Add restore to cleanup.
				localCleanupFuncs = append(localCleanupFuncs, func() {
					if restoreErr := r.cpufreqMgr.Restore(context.Background()); restoreErr != nil {
						log.WithError(restoreErr).Warn("Failed to restore CPU frequency settings")
					}
				})

				// Update resolved limits with CPU freq info.
				if cpufreqCfg.Frequency != "" && strings.ToUpper(cpufreqCfg.Frequency) != "MAX" {
					if freqKHz, err := cpufreq.ParseFrequency(cpufreqCfg.Frequency); err == nil {
						resolvedResourceLimits.CPUFreqKHz = &freqKHz
					}
				}
				resolvedResourceLimits.CPUTurboBoost = cpufreqCfg.TurboBoost
				resolvedResourceLimits.CPUGovernor = cpufreqCfg.Governor
			}
		}
	}

	// Resolve wait_after_rpc_ready for config output.
	var waitAfterRPCReadyStr string
	if r.cfg.FullConfig != nil {
		if d := r.cfg.FullConfig.GetWaitAfterRPCReady(instance); d > 0 {
			waitAfterRPCReadyStr = d.String()
		}
	}

	// Resolve run_timeout for config output and timeout enforcement.
	var runTimeoutStr string
	var runTimeout time.Duration
	if r.cfg.FullConfig != nil {
		runTimeout = r.cfg.FullConfig.GetRunTimeout(instance)
		if runTimeout > 0 {
			runTimeoutStr = runTimeout.String()
		}
	}

	// Write run configuration with resolved values.
	runConfig := &RunConfig{
		BenchmarkoorVersion: version.Version,
		Timestamp:           params.RunTimestamp,
		System:              getSystemInfo(),
		Instance: &ResolvedInstance{
			ID:     instance.ID,
			Client: instance.Client,
			ContainerRuntime: func() string {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetContainerRuntime()
				}
				return "docker"
			}(),
			Image:       imageName,
			ImageSHA256: imageDigest,
			Entrypoint:  instance.Entrypoint,
			Command:     cmd,
			ExtraArgs:   instance.ExtraArgs,
			PullPolicy:  instance.PullPolicy,
			Restart:     instance.Restart,
			Environment: env,
			DataDir:     datadirCfg,
			RollbackStrategy: func() string {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetRollbackStrategy(instance)
				}
				return ""
			}(),
			DropMemoryCaches:  dropMemoryCaches,
			WaitAfterRPCReady: waitAfterRPCReadyStr,
			RunTimeout:        runTimeoutStr,
			RetryNewPayloadsSyncingState: func() *config.RetryNewPayloadsSyncingConfig {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetRetryNewPayloadsSyncingState(instance)
				}
				return nil
			}(),
			RetryNewPayloadsFailedState: func() *config.RetryNewPayloadsFailedConfig {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetRetryNewPayloadsFailedState(instance)
				}
				return nil
			}(),
			ResourceLimits: resolvedResourceLimits,
			PostTestRPCCalls: func() []config.PostTestRPCCall {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetPostTestRPCCalls(instance)
				}
				return nil
			}(),
			PostTestSleepDuration: func() string {
				if r.cfg.FullConfig != nil {
					if d := r.cfg.FullConfig.GetPostTestSleepDuration(instance); d > 0 {
						return d.String()
					}
				}
				return ""
			}(),
			BootstrapFCU: func() *config.BootstrapFCUConfig {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetBootstrapFCU(instance)
				}
				return nil
			}(),
			CheckpointRestoreStrategyOptions: func() *config.CheckpointRestoreStrategyOptions {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetCheckpointRestoreStrategyOptions(instance)
				}
				return nil
			}(),
			OpcodeExtraction: func() *config.OpcodeExtractionConfig {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetOpcodeExtraction(instance)
				}
				return nil
			}(),
		},
	}

	// Attach metadata labels if configured (merged: client defaults + instance overrides).
	if r.cfg.FullConfig != nil {
		if labels := r.cfg.FullConfig.GetMetadataLabels(instance); len(labels) > 0 {
			runConfig.Metadata = &config.MetadataConfig{Labels: labels}
		}
	}

	if len(params.GenesisGroups) > 0 {
		runConfig.Instance.GenesisGroups = params.GenesisGroups
	} else {
		runConfig.Instance.Genesis = genesisSource
	}

	if r.executor != nil {
		runConfig.SuiteHash = r.executor.GetSuiteHash()
	}

	if err := writeRunConfig(
		runResultsDir, runConfig, r.cfg.ResultsOwner,
	); err != nil {
		log.WithError(err).Warn("Failed to write run config")
	}

	if params.LiveState != nil {
		params.LiveState.SetConfig(runConfig)
	}

	// Build container spec.
	containerName := fmt.Sprintf("benchmarkoor-%s-%s", runID, instance.ID)
	if params.GenesisGroupHash != "" {
		containerName = fmt.Sprintf(
			"benchmarkoor-%s-%s-%s",
			runID, instance.ID, params.GenesisGroupHash,
		)
	}

	containerSpec := &docker.ContainerSpec{
		Name:           containerName,
		Image:          imageName,
		Entrypoint:     instance.Entrypoint,
		Command:        cmd,
		Env:            env,
		Mounts:         mounts,
		NetworkName:    r.cfg.ContainerNetwork,
		ResourceLimits: containerResourceLimits,
		SecurityOpt:    []string{"seccomp=unconfined"},
		Labels: map[string]string{
			"benchmarkoor.instance":   instance.ID,
			"benchmarkoor.client":     instance.Client,
			"benchmarkoor.run-id":     runID,
			"benchmarkoor.managed-by": "benchmarkoor",
		},
	}

	// Save container spec and datadir info for runner-level rollback strategies.
	params.ContainerSpec = containerSpec
	params.DataDirCfg = datadirCfg
	params.UseDataDir = useDataDir

	// Apply client-specific snapshot-prepare args to the initial (pre-run/snapshot) container only, scoped to ZFS container-recreate; per-test containers clone params.ContainerSpec and keep the base command.
	createSpec := containerSpec

	if prepareArgs := spec.SnapshotPrepareArgs(); len(prepareArgs) > 0 &&
		r.cfg.FullConfig != nil &&
		r.cfg.FullConfig.GetRollbackStrategy(instance) ==
			config.RollbackStrategyContainerRecreate &&
		datadirCfg != nil && datadirCfg.Method == "zfs" {
		prepareSpec := *containerSpec
		prepareSpec.Command = append(append([]string{}, cmd...), prepareArgs...)
		createSpec = &prepareSpec

		log.WithField("args", prepareArgs).Info(
			"Applying snapshot-prepare args to initial (pre-run) container",
		)
	}

	// Create container.
	containerID, err := r.containerMgr.CreateContainer(ctx, createSpec)
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	// Setup log streaming.
	logCtx, logCancel := context.WithCancel(ctx)

	logFilePath := filepath.Join(runResultsDir, "container.log")

	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logCancel()

		return fmt.Errorf("opening container log file: %w", err)
	}

	if r.cfg.ResultsOwner != nil {
		fsutil.Chown(logFilePath, r.cfg.ResultsOwner)
	}

	// Create block log collector to capture JSON payloads from client logs.
	blockLogParser := blocklog.NewParser(client.ClientType(instance.Client))
	blockLogCollector := blocklog.NewCollector(blockLogParser, logFile)
	params.BlockLogCollector = blockLogCollector

	logDone := make(chan struct{})

	r.wg.Add(1)

	go func() {
		defer r.wg.Done()
		defer close(logDone)

		if err := r.streamLogs(
			logCtx, instance.ID, containerID, logFile, benchmarkoorLogFile,
			&containerLogInfo{
				Name:             containerName,
				ContainerID:      containerID,
				Image:            imageName,
				GenesisGroupHash: params.GenesisGroupHash,
			},
			blockLogCollector,
		); err != nil {
			// Context cancellation during cleanup is expected.
			select {
			case <-localCleanupStarted:
				log.WithError(err).Debug("Log streaming stopped")
			default:
				log.WithError(err).Warn("Log streaming error")
			}
		}
	}()

	// Cleanup: stop the container, drain logs, then remove it.
	// Podman's containers.Attach uses a hijacked TCP connection that
	// ignores context cancellation. The only way to unblock it is to
	// stop the container (server closes stdio → Attach gets EOF →
	// StreamLogs returns). Once the attach is closed, Remove succeeds
	// without blocking.
	localCleanupFuncs = append(localCleanupFuncs, func() {
		log.Info("Stopping and removing container")

		// Stop first so the container's stdio closes and the attach
		// connection (used by StreamLogs) receives EOF.
		stopCtx, stopCancel := context.WithTimeout(
			context.Background(), 30*time.Second,
		)

		stopStart := time.Now()

		if stopErr := r.containerMgr.StopContainer(
			stopCtx, containerID, nil,
		); stopErr != nil {
			log.WithError(stopErr).Debug("Failed to stop container")
		}

		stopCancel()

		log.WithField("duration", time.Since(stopStart)).Info(
			"Container stopped",
		)

		// Now that the container is stopped, the log-streaming
		// goroutine should return quickly.
		waitForLogDrain(&logDone, &logCancel, logDrainTimeout)

		// Remove the stopped container.
		rmStart := time.Now()

		if rmErr := r.containerMgr.RemoveContainer(
			context.Background(), containerID,
		); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove container")
		}

		log.WithField("duration", time.Since(rmStart)).Info(
			"Container removed",
		)

		_ = logFile.Close()
	})

	// Start container.
	if err := r.containerMgr.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	log.Info("Container started")

	// Apply run timeout if configured.
	testCtx := ctx
	var timeoutCancel context.CancelFunc

	if runTimeout > 0 {
		testCtx, timeoutCancel = context.WithTimeout(ctx, runTimeout)
		defer timeoutCancel()

		log.WithField("timeout", runTimeout).Info("Run timeout configured")
	}

	// Start container death monitoring.
	execCtx, execCancel := context.WithCancel(testCtx)
	defer execCancel()

	var containerDied bool
	var containerExitCode *int64
	var containerOOMKilled *bool
	var mu sync.Mutex

	// stoppingOnPurpose is set while the run itself stops the client — the schelk
	// promote path does that mid-run. Without it the monitor below cannot tell a
	// deliberate stop from a crash, and would cancel the very context the rest of
	// the run executes under.
	var stoppingOnPurpose bool

	isIntentional := func() bool {
		mu.Lock()
		defer mu.Unlock()

		return stoppingOnPurpose
	}

	// armContainerMonitor watches id and fails the run if it dies unexpectedly.
	// It is re-armed after a deliberate stop/start so the restarted client stays
	// covered for the remainder of the run.
	armContainerMonitor := func(id string) {
		containerExitCh, containerErrCh := r.containerMgr.WaitForContainerExit(ctx, id)

		r.wg.Add(1)

		go func() {
			defer r.wg.Done()

			select {
			case exitInfo := <-containerExitCh:
				if isIntentional() {
					log.WithField("exit_code", exitInfo.ExitCode).
						Debug("Container stopped on purpose; monitor stood down")

					return
				}

				mu.Lock()
				containerDied = true
				containerExitCode = &exitInfo.ExitCode
				containerOOMKilled = &exitInfo.OOMKilled
				mu.Unlock()

				logFields := logrus.Fields{
					"exit_code":  exitInfo.ExitCode,
					"oom_killed": exitInfo.OOMKilled,
				}

				select {
				case <-localCleanupStarted:
					log.WithFields(logFields).Debug(
						"Container stopped during cleanup",
					)
				default:
					log.WithFields(logFields).Warn(
						"Container exited unexpectedly",
					)
				}

				execCancel()
			case err := <-containerErrCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					log.WithError(err).Warn("Container wait error")
				}
			case <-r.done:
				// Runner is stopping.
			}
		}()
	}

	armContainerMonitor(containerID)

	// Get container IP for health checks.
	containerIP, err := r.containerMgr.GetContainerIP(
		ctx, containerID, r.cfg.ContainerNetwork,
	)
	if err != nil {
		return fmt.Errorf("getting container IP: %w", err)
	}

	log.WithField("ip", containerIP).Debug("Container IP address")

	// Wait for RPC to be ready.
	clientVersion, err := r.waitForRPC(execCtx, containerIP, spec.RPCPort())
	if err != nil {
		mu.Lock()
		if containerDied {
			runConfig.Status = RunStatusContainerDied
			runConfig.TerminationReason = fmt.Sprintf(
				"container exited while waiting for RPC: %v", err,
			)
			runConfig.ContainerExitCode = containerExitCode
			runConfig.ContainerOOMKilled = containerOOMKilled
		} else {
			runConfig.Status = RunStatusFailed
			runConfig.TerminationReason = fmt.Sprintf(
				"waiting for RPC: %v", err,
			)
		}
		runConfig.TimestampEnd = time.Now().Unix()
		mu.Unlock()

		if writeErr := writeRunConfig(
			runResultsDir, runConfig, r.cfg.ResultsOwner,
		); writeErr != nil {
			log.WithError(writeErr).Warn(
				"Failed to write run config with failed status",
			)
		}

		if params.LiveState != nil {
			params.LiveState.SetConfig(runConfig)
		}

		return fmt.Errorf("waiting for RPC: %w", err)
	}

	log.WithField("version", clientVersion).Info("RPC endpoint ready")

	// Wait after RPC ready if configured (gives client time to complete internal sync).
	if r.cfg.FullConfig != nil {
		if waitDuration := r.cfg.FullConfig.GetWaitAfterRPCReady(instance); waitDuration > 0 {
			log.WithField("duration", waitDuration).Info("Waiting after RPC ready")

			select {
			case <-time.After(waitDuration):
			case <-execCtx.Done():
				return execCtx.Err()
			}
		}
	}

	// Log the latest block info.
	blockNum, blockHash, stateRoot, blkErr := r.getLatestBlock(execCtx, containerIP, spec.RPCPort())

	// The replay skip below is by block NUMBER, so a datadir sitting at the right
	// height on a different chain would silently skip and benchmark the wrong
	// state. Check the hash too, against the range the bundle records.
	// preRunsAlreadyApplied short-circuits the replay: the head is already the
	// bundle's end block, so every line would be skipped anyway — and skipping
	// them still means streaming the whole file, which for a multi-GB bundle costs
	// minutes per run.
	preRunsAlreadyApplied := false

	if blkErr == nil {
		applied, err := r.verifyPreRunBundleHead(log, blockNum, blockHash)
		if err != nil {
			return err
		}

		preRunsAlreadyApplied = applied
	}

	if preRunsAlreadyApplied {
		log.Info("Pre-run bundle already applied to this datadir; skipping the replay")
	}
	if blkErr != nil {
		log.WithError(blkErr).Warn("Failed to get latest block")
	} else {
		log.WithFields(logrus.Fields{
			"block_number": blockNum,
			"block_hash":   blockHash,
			"state_root":   stateRoot,
		}).Info("Latest block")

		runConfig.StartBlock = &StartBlock{
			Number:    blockNum,
			Hash:      blockHash,
			StateRoot: stateRoot,
		}
	}

	// Move safe/finalized below the head before anything replays. Prestates
	// arrive with their head block already finalized, which would make the
	// replay anchor permanently unreachable. Best-effort: a client that will
	// not lower its finalized marker still runs, it just keeps the old
	// failure mode.
	if blockHash != "" {
		var configuredAnchor string
		if r.cfg.FullConfig != nil {
			if fcuCfg := r.cfg.FullConfig.GetBootstrapFCU(instance); fcuCfg != nil {
				configuredAnchor = fcuCfg.RootAnchorBlockHash
			}
		}

		if anchorErr := r.resetForkchoiceAnchor(
			execCtx, log, containerIP, spec.EnginePort(), spec.RPCPort(),
			blockHash, configuredAnchor, client.EngineAPIDialectFor(spec),
		); anchorErr != nil {
			log.WithError(anchorErr).Warn(
				"Could not reset safe/finalized; a deep rewind to the replay anchor may fail")
		}
	}

	// Send bootstrap FCU if configured.
	if r.cfg.FullConfig != nil {
		if fcuCfg := r.cfg.FullConfig.GetBootstrapFCU(instance); fcuCfg != nil && fcuCfg.Enabled {
			fcuHash := blockHash
			if fcuCfg.HeadBlockHash != "" {
				fcuHash = fcuCfg.HeadBlockHash

				log.WithField("block_hash", fcuHash).Info(
					"Using configured block hash for bootstrap FCU",
				)
			}

			if fcuHash != "" {
				if fcuErr := r.sendBootstrapFCU(
					execCtx, log, containerIP, spec.EnginePort(), fcuHash, "", fcuCfg,
					client.EngineAPIDialectFor(spec),
				); fcuErr != nil {
					log.WithError(fcuErr).Error("Bootstrap FCU failed")

					return fmt.Errorf("sending bootstrap FCU: %w", fcuErr)
				}

				// Re-fetch latest block after FCU with a configured head_block_hash
				// so that runConfig.StartBlock reflects the post-FCU state.
				if fcuCfg.HeadBlockHash != "" {
					bn, bh, sr, err := r.getLatestBlock(execCtx, containerIP, spec.RPCPort())
					if err != nil {
						log.WithError(err).Warn("Failed to get latest block after bootstrap FCU")
					} else {
						log.WithFields(logrus.Fields{
							"block_number": bn,
							"block_hash":   bh,
							"state_root":   sr,
						}).Info("Latest block after bootstrap FCU")

						runConfig.StartBlock = &StartBlock{
							Number:    bn,
							Hash:      bh,
							StateRoot: sr,
						}
					}
				}
			}
		}
	}

	// Update config with client version.
	runConfig.Instance.ClientVersion = clientVersion

	if err := writeRunConfig(
		runResultsDir, runConfig, r.cfg.ResultsOwner,
	); err != nil {
		log.WithError(err).Warn(
			"Failed to update run config with client version",
		)
	}

	if params.LiveState != nil {
		params.LiveState.SetConfig(runConfig)
	}

	// Stop after pre-run if requested: clear localCleanupFuncs first so
	// the deferred cleanup loop is a no-op on every exit path (success
	// or pre-run failure), leaving the container, volume/datadir, temp
	// files, and CPU frequency settings intact for inspection. Then run
	// the suite's pre-run steps (if any) with FailFast set so any
	// failure surfaces immediately.
	if r.cfg.StopAfterPrerun {
		localCleanupFuncs = nil

		if r.executor != nil {
			preRunOpts := &executor.ExecuteOptions{
				EngineEndpoint: fmt.Sprintf(
					"http://%s:%d", containerIP, spec.EnginePort(),
				),
				JWT:                           r.cfg.JWT,
				ResultsDir:                    runResultsDir,
				FailFast:                      true,
				PreRunStepSleep:               r.cfg.PreRunStepSleep,
				SkipUntilBlockNumber:          blockNum,
				RetryNewPayloadsSyncingConfig: r.cfg.FullConfig.GetRetryNewPayloadsSyncingState(instance),
				RetryNewPayloadsFailedConfig:  r.cfg.FullConfig.GetRetryNewPayloadsFailedState(instance),
			}

			if n, err := r.executor.RunPreRunSteps(execCtx, preRunOpts); err != nil {
				return fmt.Errorf("running pre-run steps: %w", err)
			} else if n > 0 {
				log.WithField("steps", n).Info("Pre-run steps completed")
			}
		}

		log.WithFields(logrus.Fields{
			"container_id": containerID,
			"container_ip": containerIP,
			"rpc_endpoint": fmt.Sprintf("http://%s:%d", containerIP, spec.RPCPort()),
			"engine_endpoint": fmt.Sprintf(
				"http://%s:%d", containerIP, spec.EnginePort(),
			),
			"data_mount": dataMount.Source,
			"run_dir":    runResultsDir,
		}).Info("--stop-after-prerun set; exiting without running tests, leaving resources intact")

		return nil
	}

	// Execute tests if executor is configured.
	if r.executor != nil {
		log.Info("Starting test execution")

		var dropCachesPath string
		if r.cfg.FullConfig != nil {
			dropCachesPath = r.cfg.FullConfig.GetDropCachesPath()
		}

		// Resolve rollback strategy.
		var rollbackStrategy string
		if r.cfg.FullConfig != nil {
			rollbackStrategy = r.cfg.FullConfig.GetRollbackStrategy(instance)
		}

		// Fail fast if checkpoint-restore prerequisites are not met.
		if rollbackStrategy == config.RollbackStrategyCheckpointRestore {
			cpMgr, ok := r.containerMgr.(podman.CheckpointManager)
			if !ok {
				return fmt.Errorf("container manager does not support checkpoint/restore")
			}

			if err := cpMgr.ValidateCheckpointSupport(ctx); err != nil {
				return fmt.Errorf("checkpoint/restore prerequisites not met: %w", err)
			}
		}

		// Promote the schelk baseline for the strategies that keep this one client
		// for the whole run (none, rpc-debug-setHead). The runner-level strategies
		// below handle it themselves, next to their own snapshot/checkpoint, since
		// they rebuild the container per test anyway.
		skipPreRunSteps := preRunsAlreadyApplied

		if datadirCfg.ShouldPromotePostPreRuns() && !isRunnerLevelStrategy(rollbackStrategy) &&
			!preRunsAlreadyApplied {
			// The log stream ends with the stopped container, so re-attach it to
			// the restarted one; otherwise the client's logs for the entire
			// benchmark phase — everything after the promote — are lost.
			reattachLogs := func() {
				logCtx, newLogCancel := context.WithCancel(ctx)
				logCancel = newLogCancel
				logDone = make(chan struct{})

				r.wg.Add(1)

				go func() {
					defer r.wg.Done()
					defer close(logDone)

					if err := r.streamLogs(
						logCtx, instance.ID, containerID, logFile, benchmarkoorLogFile,
						&containerLogInfo{
							Name:             containerName,
							ContainerID:      containerID,
							Image:            imageName,
							GenesisGroupHash: params.GenesisGroupHash,
						},
						blockLogCollector,
					); err != nil {
						select {
						case <-localCleanupStarted:
							log.WithError(err).Debug("Log streaming stopped")
						default:
							log.WithError(err).Warn("Log streaming error")
						}
					}
				}()
			}

			newIP, err := r.promoteSchelkInPlace(
				execCtx, ctx, instance, spec, containerID, containerIP, runResultsDir,
				blockNum, &stoppingOnPurpose, &mu, armContainerMonitor, reattachLogs,
				&logDone, &logCancel, log,
			)
			if err != nil {
				return fmt.Errorf("promoting schelk baseline after pre-run steps: %w", err)
			}

			if newIP != "" {
				containerIP = newIP
				skipPreRunSteps = true
			}
		}

		isRunnerLevel := rollbackStrategy == config.RollbackStrategyContainerRecreate ||
			rollbackStrategy == config.RollbackStrategyCheckpointRestore

		var (
			result  *executor.ExecutionResult
			execErr error
		)

		if isRunnerLevel {
			// Runner-level strategies intentionally stop and restart
			// containers. Signal cleanup-started so the death monitor
			// treats container exits as expected (debug-level logging),
			// and cancel execCtx so the monitor's execCancel() is a no-op.
			localCleanupOnce.Do(func() { close(localCleanupStarted) })
			execCancel()

			switch rollbackStrategy {
			case config.RollbackStrategyCheckpointRestore:
				result, execErr = r.runTestsWithCheckpointRestore(
					testCtx, params, spec, containerID, containerIP,
					dropMemoryCaches, dropCachesPath,
					runResultsDir, &logCancel, &logDone, benchmarkoorLogFile,
					&localCleanupFuncs, localCleanupStarted,
				)
			default:
				result, execErr = r.runTestsWithContainerStrategy(
					testCtx, params, spec, containerID, containerIP,
					rollbackStrategy, dropMemoryCaches, dropCachesPath,
					runResultsDir, &logCancel, &logDone, benchmarkoorLogFile,
					&localCleanupFuncs, localCleanupStarted,
				)
			}
		} else {
			execOpts := &executor.ExecuteOptions{
				EngineEndpoint: fmt.Sprintf(
					"http://%s:%d", containerIP, spec.EnginePort(),
				),
				JWT:                   r.cfg.JWT,
				ResultsDir:            runResultsDir,
				Filter:                r.cfg.TestFilter,
				ContainerID:           containerID,
				DockerClient:          r.getDockerClient(),
				DropMemoryCaches:      dropMemoryCaches,
				DropCachesPath:        dropCachesPath,
				RollbackStrategy:      rollbackStrategy,
				ClientRPCRollbackSpec: spec.RPCRollbackSpec(),
				RPCEndpoint: fmt.Sprintf(
					"http://%s:%d", containerIP, spec.RPCPort(),
				),
				Tests:                         params.Tests,
				BlockLogCollector:             params.BlockLogCollector,
				RetryNewPayloadsSyncingConfig: r.cfg.FullConfig.GetRetryNewPayloadsSyncingState(instance),
				RetryNewPayloadsFailedConfig:  r.cfg.FullConfig.GetRetryNewPayloadsFailedState(instance),
				PostTestRPCCalls:              r.cfg.FullConfig.GetPostTestRPCCalls(instance),
				OpcodeExtraction:              r.cfg.FullConfig.GetOpcodeExtraction(instance),
				PostTestSleepDuration:         r.cfg.FullConfig.GetPostTestSleepDuration(instance),
				PreRunStepSleep:               r.cfg.PreRunStepSleep,
				SkipUntilBlockNumber:          blockNum,
				SkipPreRunSteps:               skipPreRunSteps,
			}

			result, execErr = r.executor.ExecuteTests(execCtx, execOpts)
		}

		if execErr != nil {
			log.WithError(execErr).Error("Test execution failed")

			mu.Lock()
			runConfig.Status = RunStatusFailed
			runConfig.TerminationReason = fmt.Sprintf(
				"test execution failed: %v", execErr,
			)
			mu.Unlock()
		}

		if result != nil {
			log.WithFields(logrus.Fields{
				"total":    result.TotalTests,
				"passed":   result.Passed,
				"failed":   result.Failed,
				"duration": result.TotalDuration,
			}).Info("Test execution completed")

			suiteTotal := result.TotalTests
			if r.executor != nil {
				if allTests := r.executor.GetTests(); allTests != nil {
					suiteTotal = len(allTests)
				}
			}

			mu.Lock()

			if params.AccumulatedTestCount != nil {
				// Multi-genesis mode: accumulate counts across groups.
				params.AccumulatedTestCount.Total = suiteTotal
				params.AccumulatedTestCount.Passed += result.Passed
				params.AccumulatedTestCount.Failed += result.Failed
				runConfig.TestCounts = &TestCounts{
					Total:  params.AccumulatedTestCount.Total,
					Passed: params.AccumulatedTestCount.Passed,
					Failed: params.AccumulatedTestCount.Failed,
				}
			} else {
				runConfig.TestCounts = &TestCounts{
					Total:  suiteTotal,
					Passed: result.Passed,
					Failed: result.Failed,
				}
			}

			mu.Unlock()

			if result.StatsReaderType != "" {
				runConfig.SystemResourceCollectionMethod = result.StatsReaderType
			}

			if isRunnerLevel {
				// Runner-level strategies intentionally stop containers,
				// which causes the death monitor to set containerDied.
				// Reset it and trust only the strategy's result.
				mu.Lock()
				containerDied = result.ContainerDied
				containerExitCode = nil
				containerOOMKilled = nil
				mu.Unlock()
			} else if result.ContainerDied {
				mu.Lock()
				containerDied = true
				mu.Unlock()
			}
		}
	}

	// Determine final run status (don't overwrite if already set by executor).
	// Timeout and cancellation are checked before containerDied because when
	// either fires, the context cancellation stops the container, which causes
	// the death monitor to set containerDied.
	mu.Lock()
	if timeoutCancel != nil && testCtx.Err() == context.DeadlineExceeded {
		runConfig.Status = RunStatusTimedOut
		runConfig.TerminationReason = fmt.Sprintf("the run_timeout of %s was reached", runTimeout)
	} else if ctx.Err() != nil {
		runConfig.Status = RunStatusCancelled
		runConfig.TerminationReason = "run was cancelled"
	} else if containerDied {
		runConfig.Status = RunStatusContainerDied
		runConfig.TerminationReason = "container exited during test execution"
		runConfig.ContainerExitCode = containerExitCode
		runConfig.ContainerOOMKilled = containerOOMKilled
	} else if runConfig.Status == "" {
		runConfig.Status = RunStatusCompleted
	}
	mu.Unlock()

	// Record when the run ended.
	runConfig.TimestampEnd = time.Now().Unix()

	// Push the terminal status to the live-reporter state so the next
	// snapshot (including the synchronous one fired by Reporter.Stop()) sees
	// the final values.
	if params.LiveState != nil {
		params.LiveState.SetTerminal(
			runConfig.Status, runConfig.TerminationReason, runConfig.TimestampEnd,
		)
	}

	// Write final config with status.
	if err := writeRunConfig(
		runResultsDir, runConfig, r.cfg.ResultsOwner,
	); err != nil {
		log.WithError(err).Warn("Failed to write final run config with status")
	} else {
		log.WithField("status", runConfig.Status).Info("Run completed")
	}

	if params.LiveState != nil {
		params.LiveState.SetConfig(runConfig)
	}

	// Write block logs if any were captured.
	if params.BlockLogCollector != nil {
		blockLogs := params.BlockLogCollector.GetBlockLogs()
		if len(blockLogs) > 0 {
			if err := executor.WriteBlockLogsResult(
				runResultsDir, blockLogs, r.cfg.ResultsOwner,
			); err != nil {
				log.WithError(err).Warn("Failed to write block logs result")
			} else {
				log.WithField("count", len(blockLogs)).Info("Block logs written")
			}
		}
	}

	// Return an error if the container died so callers (e.g. multi-genesis
	// loop) stop instead of continuing with the next group.
	if containerDied {
		return fmt.Errorf("container died during execution")
	}

	// A run that benchmarked nothing — a pre-run replay that could not be
	// applied, an RPC that never came up — has already written its results and
	// logged its status, but returning nil here let the process exit 0 and the
	// CI job go green on a run that produced no numbers.
	if runConfig.Status == RunStatusFailed {
		return fmt.Errorf("run failed: %s", runConfig.TerminationReason)
	}

	return nil
}

// loadFile loads content from a URL or local file path.
func (r *runner) loadFile(ctx context.Context, source string) ([]byte, error) {
	// Check if source is a URL.
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return r.downloadFromURL(ctx, source)
	}

	// Treat as local file path.
	return r.readFromFile(source)
}

// downloadFromURL downloads content from a URL.
func (r *runner) downloadFromURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return data, nil
}

// readFromFile reads content from a local file.
func (r *runner) readFromFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	return data, nil
}

func getSystemInfo() *SystemInfo {
	info := &SystemInfo{}

	if hostInfo, err := host.Info(); err == nil {
		info.Hostname = hostInfo.Hostname
		info.OS = hostInfo.OS
		info.Platform = hostInfo.Platform
		info.PlatformVersion = hostInfo.PlatformVersion
		info.KernelVersion = hostInfo.KernelVersion
		info.Arch = hostInfo.KernelArch
		info.Virtualization = hostInfo.VirtualizationSystem
		info.VirtualizationRole = hostInfo.VirtualizationRole
	}

	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CPUVendor = cpuInfo[0].VendorID
		info.CPUModel = cpuInfo[0].ModelName
		info.CPUMhz = cpuInfo[0].Mhz
		info.CPUCacheKB = int(cpuInfo[0].CacheSize)
	}

	if cores, err := cpu.Counts(false); err == nil {
		info.CPUCores = cores
	}

	if memInfo, err := mem.VirtualMemory(); err == nil {
		info.MemoryTotalGB = float64(memInfo.Total) / (1024 * 1024 * 1024)
	}

	return info
}

func writeRunConfig(resultsDir string, cfg *RunConfig, owner *fsutil.OwnerConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling run config: %w", err)
	}

	configPath := filepath.Join(resultsDir, "config.json")
	if err := fsutil.WriteFile(configPath, data, 0644, owner); err != nil {
		return fmt.Errorf("writing config.json: %w", err)
	}

	return nil
}

// stateActorFilePrefix matches the provenance files a recent state-actor drops
// at the datadir root: state-actor-manifest.json and its content-addressed spec
// sidecar (state-actor-spec-<sha256>.yaml).
const stateActorFilePrefix = "state-actor-"

// copyStateActorFiles copies any state-actor provenance files from the snapshot
// source dir's root into <runResultsDir>/.state-actor/. Best-effort: a missing
// source, no matching files, or copy errors are logged but never fail the run —
// the metadata is auxiliary and only present for snapshots built by a recent
// state-actor.
func copyStateActorFiles(
	log logrus.FieldLogger,
	sourceDir, runResultsDir string,
	owner *fsutil.OwnerConfig,
) {
	if sourceDir == "" {
		return
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		log.WithError(err).Debug("state-actor: reading snapshot source dir")

		return
	}

	names := make([]string, 0, 2)

	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), stateActorFilePrefix) {
			names = append(names, e.Name())
		}
	}

	if len(names) == 0 {
		return
	}

	dst := filepath.Join(runResultsDir, ".state-actor")
	if err := fsutil.MkdirAll(dst, 0o755, owner); err != nil {
		log.WithError(err).Warn("state-actor: creating .state-actor output dir")

		return
	}

	copied := 0

	for _, name := range names {
		data, readErr := os.ReadFile(filepath.Join(sourceDir, name))
		if readErr != nil {
			log.WithError(readErr).WithField("file", name).Warn("state-actor: reading file")

			continue
		}

		if writeErr := fsutil.WriteFile(filepath.Join(dst, name), data, 0o644, owner); writeErr != nil {
			log.WithError(writeErr).WithField("file", name).Warn("state-actor: writing file")

			continue
		}

		copied++
	}

	log.WithField("count", copied).Debug("Copied state-actor provenance into run output")
}

// verifyPreRunBundleHead checks the datadir the client booted on against the
// block range its pre-run bundle records, and fails the run when they disagree.
//
// The executor resumes a replay by block NUMBER alone, so without this a datadir
// sitting at the right height on a different chain — a promoted baseline built
// from a different bundle, or a re-fill that produced different blocks — skips
// the replay silently and benchmarks the wrong state. Promoting makes
// "already applied" the normal case, so this stops being hypothetical.
//
// A head at the bundle's end block means the pre-runs are already applied (the
// replay then skips every line); at its start block means they are about to be.
// Anything in between is a partially applied bundle, which the replay resumes
// from. Only a hash mismatch at a block the bundle actually names is an error —
// that is the case that cannot be anything but the wrong chain.
// preRunBundleDir locates the pre-run bundle for head verification.
//
// The source is asked first: the bundle may sit at a configured
// local_fixtures_dir OR inside the fixtures artifact the source extracted, and
// only the source knows which. Deriving the path from config alone resolves the
// local case and silently misses the artifact one, which leaves the by-number
// replay skip unguarded — a datadir at the right height on a different chain
// then replays as a no-op and benchmarks the wrong state.
//
// The config-derived path remains as a fallback for callers without a live
// source.
func (r *runner) preRunBundleDir(preRuns *config.EESTPreRunsSource) string {
	if r.executor != nil {
		if loc, ok := r.executor.GetSource().(executor.PreRunBundleLocator); ok {
			if dir := loc.PreRunBundleDir(); dir != "" {
				return dir
			}
		}
	}

	if preRuns.LocalFixturesDir == "" {
		return ""
	}

	subdir := preRuns.FixturesSubdir
	if subdir == "" {
		subdir = config.PreRunBundleSubdir
	}

	return filepath.Join(preRuns.LocalFixturesDir, subdir)
}

func (r *runner) verifyPreRunBundleHead(
	log logrus.FieldLogger, headNumber uint64, headHash string,
) (bool, error) {
	if r.cfg.FullConfig == nil {
		return false, nil
	}

	src := r.cfg.FullConfig.Runner.Benchmark.Tests.Source.EESTFixtures
	if src == nil || src.PreRuns == nil {
		return false, nil
	}

	bundleDir := r.preRunBundleDir(src.PreRuns)
	if bundleDir == "" {
		log.Warn(
			"A pre_runs source is configured but its bundle could not be located; " +
				"skipping head verification — the replay skips by block number alone, " +
				"so a datadir at the right height on a different chain will not be caught",
		)

		return false, nil
	}

	info, err := builder.ReadPreRunBundleInfoAt(bundleDir)
	if err != nil {
		// Metadata is a convenience; an unreadable sidecar must not block a run
		// that would otherwise replay correctly.
		log.WithError(err).Warn("Could not read pre-run bundle metadata; skipping head verification")

		return false, nil
	}

	if info == nil {
		return false, nil
	}

	expect := func(want string, applied bool) (bool, error) {
		if strings.EqualFold(headHash, want) {
			log.WithFields(logrus.Fields{
				"block":            headNumber,
				"pre_runs_applied": applied,
			}).Info("Datadir head matches the pre-run bundle")

			return applied, nil
		}

		return false, fmt.Errorf(
			"datadir head %d has hash %s but the pre-run bundle expects %s at that block — "+
				"the datadir is on a different chain than the bundle was recorded against",
			headNumber, headHash, want,
		)
	}

	if headNumber == info.EndBlockNumber {
		return expect(info.EndBlockHash, true)
	}

	// Info recovered from the bundle file knows only its end block, so the start
	// and range checks below have nothing to compare against. The end-block check
	// above is the one that matters — it is what skips the replay — and the replay
	// itself still fails loudly if the head cannot be built on.
	if info.Derived() {
		log.WithFields(logrus.Fields{
			"block": headNumber, "bundle_end": info.EndBlockNumber,
		}).Info("Pre-run bundle has no metadata sidecar; head verified against its end block only")

		return false, nil
	}

	if headNumber == info.StartBlockNumber {
		return expect(info.StartBlockHash, false)
	}

	if headNumber > info.StartBlockNumber && headNumber < info.EndBlockNumber {
		log.WithFields(logrus.Fields{
			"block": headNumber, "bundle_start": info.StartBlockNumber, "bundle_end": info.EndBlockNumber,
		}).Info("Datadir head is inside the pre-run bundle range; the replay will resume from it")

		return false, nil
	}

	return false, fmt.Errorf(
		"datadir head %d is outside the pre-run bundle range %d..%d — the bundle cannot be "+
			"replayed onto this datadir",
		headNumber, info.StartBlockNumber, info.EndBlockNumber,
	)
}

// isRunnerLevelStrategy reports whether a rollback strategy rebuilds the client
// between tests. Those strategies persist the schelk baseline themselves, next
// to their own snapshot/checkpoint, because they have to bake the pre-run state
// into whatever each test restores from.
func isRunnerLevelStrategy(strategy string) bool {
	return strategy == config.RollbackStrategyContainerRecreate ||
		strategy == config.RollbackStrategyCheckpointRestore
}

// promoteSchelkInPlace applies the suite's pre-run steps, then persists the
// resulting datadir as the new schelk baseline, for the strategies that keep one
// client for the whole run (none, rpc-debug-setHead).
//
// `schelk promote` unmounts the volume, so the client cannot hold it: the client
// is stopped, promoted, the volume remounted, and the SAME container started
// again — restarting re-binds the mount from the host path, which now resolves to
// the promoted volume. The container monitor is told the stop is deliberate and
// re-armed afterwards, so a mid-run stop is not mistaken for a crash.
//
// Returns the restarted container's IP, or "" when nothing was promoted (so the
// caller leaves the executor to run the pre-run steps as usual).
func (r *runner) promoteSchelkInPlace(
	execCtx, ctx context.Context,
	instance *config.ClientInstance,
	spec client.Spec,
	containerID, containerIP, resultsDir string,
	headNumber uint64,
	stoppingOnPurpose *bool,
	mu *sync.Mutex,
	armContainerMonitor func(string),
	reattachLogs func(),
	logDone *chan struct{},
	logCancel *context.CancelFunc,
	log logrus.FieldLogger,
) (string, error) {
	preRunOpts := &executor.ExecuteOptions{
		EngineEndpoint: fmt.Sprintf(
			"http://%s:%d", containerIP, spec.EnginePort(),
		),
		JWT:                           r.cfg.JWT,
		ResultsDir:                    resultsDir,
		FailFast:                      true,
		PreRunStepSleep:               r.cfg.PreRunStepSleep,
		SkipUntilBlockNumber:          headNumber,
		RetryNewPayloadsSyncingConfig: r.cfg.FullConfig.GetRetryNewPayloadsSyncingState(instance),
		RetryNewPayloadsFailedConfig:  r.cfg.FullConfig.GetRetryNewPayloadsFailedState(instance),
	}

	n, err := r.executor.RunPreRunSteps(execCtx, preRunOpts)
	if err != nil {
		return "", fmt.Errorf("running pre-run steps: %w", err)
	}

	if n == 0 {
		log.Warn(
			"promote_post_pre_runs is set but the suite has no pre-run steps; " +
				"refusing to promote (the baseline would be the raw snapshot)",
		)

		return "", nil
	}

	log.WithField("steps", n).Info("Pre-run steps completed; promoting datadir to the schelk baseline")

	log.WithField("settle", schelkSettleBeforeStop).Info("Waiting for client to settle before stop")
	time.Sleep(schelkSettleBeforeStop)

	mu.Lock()
	*stoppingOnPurpose = true
	mu.Unlock()

	defer func() {
		mu.Lock()
		*stoppingOnPurpose = false
		mu.Unlock()
	}()

	// Default (60s) SIGTERM grace so the client persists its state cleanly; a
	// killed client may not have flushed, and that datadir must not become the
	// irreversible golden image.
	log.Info("Stopping client for schelk promote (graceful)")

	if err := r.containerMgr.StopContainer(ctx, containerID, nil); err != nil {
		return "", fmt.Errorf("stopping container: %w", err)
	}

	// Let the stopped container's logs finish landing before promoting.
	waitForLogDrain(logDone, logCancel, logDrainTimeout)

	if syncErr := exec.Command("sync").Run(); syncErr != nil {
		log.WithError(syncErr).Warn("Failed to sync before schelk promote")
	}

	log.Info("Persisting the advanced datadir as the new schelk baseline (`schelk promote`)")

	if err := datadir.SchelkPromote(ctx, r.log); err != nil {
		return "", fmt.Errorf("schelk promote: %w", err)
	}

	// promote leaves the volume unmounted; the restarted container binds this path.
	if err := datadir.EnsureSchelkMounted(ctx, r.log); err != nil {
		return "", fmt.Errorf("remounting schelk volume after promote: %w", err)
	}

	log.Info("Restarting client on the promoted baseline")

	if err := r.containerMgr.StartContainer(ctx, containerID); err != nil {
		return "", fmt.Errorf("restarting container after promote: %w", err)
	}

	mu.Lock()
	*stoppingOnPurpose = false
	mu.Unlock()

	armContainerMonitor(containerID)
	reattachLogs()

	newIP, err := r.containerMgr.GetContainerIP(ctx, containerID, r.cfg.ContainerNetwork)
	if err != nil {
		return "", fmt.Errorf("getting container IP after promote: %w", err)
	}

	if _, err := r.waitForRPC(execCtx, newIP, spec.RPCPort()); err != nil {
		return "", fmt.Errorf("waiting for RPC after promote: %w", err)
	}

	log.WithField("ip", newIP).Info("Client back up on the promoted baseline")

	return newIP, nil
}
