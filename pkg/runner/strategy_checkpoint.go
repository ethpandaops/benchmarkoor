package runner

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/podman"
	"github.com/ethpandaops/benchmarkoor/pkg/stats"
	"github.com/sirupsen/logrus"
)

// runTestsWithCheckpointRestore executes tests using Podman checkpoint/restore.
// After the initial container reaches RPC readiness, the method checkpoints the
// container's memory state and snapshots the data directory. For each test, it
// rolls back the data directory and restores the container from the checkpoint —
// the client process resumes mid-execution without restart or RPC polling.
//
// Two data-directory rollback strategies are supported:
//   - ZFS snapshots (when datadir.method is "zfs"): instant copy-on-write rollback.
//   - Copy-based (when no datadir is configured): cp -a snapshot, rsync restore.
//
//nolint:gocognit,cyclop // Per-test checkpoint/restore is inherently complex.
func (r *runner) runTestsWithCheckpointRestore(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	containerID string,
	containerIP string,
	dropMemoryCaches string,
	dropCachesPath string,
	resultsDir string,
	logCancel *context.CancelFunc,
	logDone *chan struct{},
	benchmarkoorLog *os.File,
	cleanupFuncs *[]func(),
	cleanupStarted chan struct{},
) (*executor.ExecutionResult, error) {
	log := r.log.WithFields(logrus.Fields{
		"instance": params.Instance.ID,
		"run_id":   params.RunID,
		"strategy": "container-checkpoint-restore",
	})

	// Type-assert the container manager to CheckpointManager.
	cpMgr, ok := r.containerMgr.(podman.CheckpointManager)
	if !ok {
		return nil, fmt.Errorf("container manager does not support checkpoint/restore")
	}

	// Resolve the test list.
	tests := params.Tests
	if tests == nil {
		tests = r.executor.GetTests()
	}

	if len(tests) == 0 {
		return &executor.ExecutionResult{}, nil
	}

	log.WithField("tests", len(tests)).Info(
		"Running tests with checkpoint-restore strategy",
	)

	useZFS := params.DataDirCfg != nil && params.DataDirCfg.Method == "zfs"

	// Find the data mount source — this is the host path the container
	// mounts for its data directory.
	dataMountSource := ""

	containerDir := spec.DataDir()
	if params.DataDirCfg != nil && params.DataDirCfg.ContainerDir != "" {
		containerDir = params.DataDirCfg.ContainerDir
	}

	for _, mnt := range params.ContainerSpec.Mounts {
		if mnt.Target == containerDir {
			dataMountSource = mnt.Source

			break
		}
	}

	if dataMountSource == "" {
		return nil, fmt.Errorf("could not find data mount for %s in container spec", containerDir)
	}

	// 1. Optionally restart the container before checkpointing.
	//    Some clients (e.g., Erigon with MDBX) keep in-memory caches or
	//    dirty state that interferes with CRIU checkpoint/restore. A
	//    stop+start cycle gives us a cold-start process with a cleanly
	//    shut-down database — ideal for a reliable checkpoint.
	if r.cfg.FullConfig.GetCheckpointRestartContainer(params.Instance) {
		log.Info("Restarting container before checkpoint for clean process state")

		stopStart := time.Now()

		if err := r.containerMgr.StopContainer(ctx, containerID, nil); err != nil {
			return nil, fmt.Errorf("stopping container before checkpoint restart: %w", err)
		}

		log.WithField("duration", time.Since(stopStart)).Info(
			"Container stopped for checkpoint restart",
		)

		// Drain logs from the stopped container.
		waitForLogDrain(logDone, logCancel, logDrainTimeout)

		if err := r.containerMgr.StartContainer(ctx, containerID); err != nil {
			return nil, fmt.Errorf("starting container after checkpoint restart: %w", err)
		}

		// IP may change after restart; refresh it.
		newIP, err := r.containerMgr.GetContainerIP(
			ctx, containerID, r.cfg.ContainerNetwork,
		)
		if err != nil {
			return nil, fmt.Errorf("getting container IP after checkpoint restart: %w", err)
		}

		containerIP = newIP

		// Restart log streaming for the restarted container.
		if logErr := r.startLogStreaming(
			ctx, resultsDir,
			params.Instance.ID, containerID,
			benchmarkoorLog, &containerLogInfo{
				Name:             params.ContainerSpec.Name,
				ContainerID:      containerID,
				Image:            params.ContainerSpec.Image,
				GenesisGroupHash: params.GenesisGroupHash,
			},
			params.BlockLogCollector, cleanupStarted,
			logDone, logCancel, cleanupFuncs,
		); logErr != nil {
			return nil, fmt.Errorf(
				"log streaming after checkpoint restart: %w", logErr,
			)
		}

		// Wait for RPC readiness on the restarted container.
		if _, err := r.waitForRPC(ctx, containerIP, spec.RPCPort()); err != nil {
			return nil, fmt.Errorf(
				"waiting for RPC after checkpoint restart: %w", err,
			)
		}

		// Honour the post-RPC-ready wait if configured.
		if waitDuration := r.cfg.FullConfig.GetWaitAfterRPCReady(
			params.Instance,
		); waitDuration > 0 {
			log.WithField("duration", waitDuration).Info(
				"Waiting after RPC ready (post-restart)",
			)

			select {
			case <-time.After(waitDuration):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		log.Info("Container restarted and RPC ready, proceeding to checkpoint")
	}

	// 2. Run pre-run steps on the live container before checkpointing.
	//    These steps (e.g., genesis setup) must be baked into the
	//    checkpoint so every restored container starts post-pre-run.
	engineEndpoint := fmt.Sprintf("http://%s:%d", containerIP, spec.EnginePort())

	// Where the datadir already sits decides how much of the bundle to replay: a
	// promoted baseline is already AT its end block, so replaying from the first
	// block re-imports thousands of known blocks to land where it started.
	headNumber, headHash, _, blkErr := r.getLatestBlock(ctx, containerIP, spec.RPCPort())
	preRunsApplied := false

	if blkErr != nil {
		log.WithError(blkErr).Warn("Could not read the datadir head; replaying the whole bundle")
	} else {
		applied, verifyErr := r.verifyPreRunBundleHead(log, headNumber, headHash)
		if verifyErr != nil {
			return nil, verifyErr
		}

		preRunsApplied = applied
	}

	preRunOpts := &executor.ExecuteOptions{
		EngineEndpoint:                engineEndpoint,
		JWT:                           r.cfg.JWT,
		ResultsDir:                    resultsDir,
		SkipUntilBlockNumber:          headNumber,
		RetryNewPayloadsSyncingConfig: r.cfg.FullConfig.GetRetryNewPayloadsSyncingState(params.Instance),
		RetryNewPayloadsFailedConfig:  r.cfg.FullConfig.GetRetryNewPayloadsFailedState(params.Instance),
		PreRunStepSleep:               r.cfg.PreRunStepSleep,
		SkipPreRunSteps:               preRunsApplied,
	}

	preRunSteps, err := r.executor.RunPreRunSteps(ctx, preRunOpts)
	if err != nil {
		return nil, fmt.Errorf("running pre-run steps before checkpoint: %w", err)
	}

	if preRunSteps > 0 {
		log.WithField("steps", preRunSteps).Info("Pre-run steps completed before checkpoint")
	}

	// 2a. Compact the database before the checkpoint, so every restored
	//     container resumes on the compacted datadir. `geth db compact` takes
	//     the database lock, so the client is stopped for it and started again
	//     before the checkpoint is taken — a checkpoint of a client that is not
	//     running on this datadir would restore onto a database it never opened.
	compactionMount := docker.Mount{
		Type: "bind", Source: dataMountSource, Target: containerDir,
	}

	// As in prepareDatadirBeforeBenchmarks: settle the marker question before
	// the stop, so a datadir that is already compacted does not cost a client
	// recycle on every run.
	compactionSkip := r.dbCompactionSkipEntry(
		params.Instance, config.DBCompactionBeforeBenchmarks, compactionMount,
	)
	if compactionSkip != nil {
		logDBCompactionSkip(log, compactionSkip)
	}

	if compactionSkip == nil &&
		r.dbCompactionFor(params.Instance, config.DBCompactionBeforeBenchmarks) != nil {
		compactionHead := r.dbCompactionHeadFromRPC(ctx, containerIP, spec.RPCPort(), log)

		if err := r.stopClientForDatadirWork(
			ctx, containerID, logDone, logCancel, log,
		); err != nil {
			return nil, fmt.Errorf("stopping container for db compaction: %w", err)
		}

		if _, err := r.compactDatadirForPhase(
			ctx, params.Instance, spec, config.DBCompactionBeforeBenchmarks,
			params.RunID, params.ImageName, resultsDir, compactionMount,
			benchmarkoorLog, compactionHead,
		); err != nil {
			return nil, err
		}

		if err := r.containerMgr.StartContainer(ctx, containerID); err != nil {
			return nil, fmt.Errorf("starting container after db compaction: %w", err)
		}

		newIP, ipErr := r.containerMgr.GetContainerIP(
			ctx, containerID, r.cfg.ContainerNetwork,
		)
		if ipErr != nil {
			return nil, fmt.Errorf("getting container IP after db compaction: %w", ipErr)
		}

		containerIP = newIP

		if logErr := r.startLogStreaming(
			ctx, resultsDir,
			params.Instance.ID, containerID,
			benchmarkoorLog, &containerLogInfo{
				Name:             params.ContainerSpec.Name,
				ContainerID:      containerID,
				Image:            params.ContainerSpec.Image,
				GenesisGroupHash: params.GenesisGroupHash,
			},
			params.BlockLogCollector, cleanupStarted,
			logDone, logCancel, cleanupFuncs,
		); logErr != nil {
			return nil, fmt.Errorf("log streaming after db compaction: %w", logErr)
		}

		if _, err := r.waitForRPC(ctx, containerIP, spec.RPCPort()); err != nil {
			return nil, fmt.Errorf("waiting for RPC after db compaction: %w", err)
		}

		log.WithField("ip", containerIP).Info("Client back up on the compacted datadir")
	}

	// 2c. Persist the advanced datadir as the new schelk baseline, if asked.
	//
	//     This has to happen BEFORE the checkpoint: `schelk promote` unmounts the
	//     volume, so the client cannot be holding it, and the checkpoint must be
	//     taken of a client already running on the promoted baseline — otherwise
	//     every restore would come back on a datadir the baseline no longer
	//     matches. The stop/restart mirrors the pre-checkpoint restart above,
	//     including re-attaching log streaming.
	if params.DataDirCfg.ShouldPromotePostPreRuns() && preRunSteps > 0 && !preRunsApplied {
		log.Info("Promoting the advanced datadir to the schelk baseline before checkpointing")

		log.WithField("settle", schelkSettleBeforeStop).Info(
			"Waiting for client to settle before stop",
		)
		time.Sleep(schelkSettleBeforeStop)

		if err := r.containerMgr.StopContainer(ctx, containerID, nil); err != nil {
			return nil, fmt.Errorf("stopping container for schelk promote: %w", err)
		}

		waitForLogDrain(logDone, logCancel, logDrainTimeout)

		if syncErr := exec.Command("sync").Run(); syncErr != nil {
			log.WithError(syncErr).Warn("Failed to sync before schelk promote")
		}

		log.Info("Persisting the advanced datadir as the new schelk baseline (`schelk promote`)")

		if err := datadir.SchelkPromote(ctx, r.log); err != nil {
			return nil, fmt.Errorf("schelk promote: %w", err)
		}

		// promote leaves the volume unmounted; the restarted client binds this path.
		if err := datadir.EnsureSchelkMounted(ctx, r.log); err != nil {
			return nil, fmt.Errorf("remounting schelk volume after promote: %w", err)
		}

		if err := r.containerMgr.StartContainer(ctx, containerID); err != nil {
			return nil, fmt.Errorf("starting container after schelk promote: %w", err)
		}

		newIP, ipErr := r.containerMgr.GetContainerIP(
			ctx, containerID, r.cfg.ContainerNetwork,
		)
		if ipErr != nil {
			return nil, fmt.Errorf("getting container IP after schelk promote: %w", ipErr)
		}

		containerIP = newIP

		if logErr := r.startLogStreaming(
			ctx, resultsDir,
			params.Instance.ID, containerID,
			benchmarkoorLog, &containerLogInfo{
				Name:             params.ContainerSpec.Name,
				ContainerID:      containerID,
				Image:            params.ContainerSpec.Image,
				GenesisGroupHash: params.GenesisGroupHash,
			},
			params.BlockLogCollector, cleanupStarted,
			logDone, logCancel, cleanupFuncs,
		); logErr != nil {
			return nil, fmt.Errorf("log streaming after schelk promote: %w", logErr)
		}

		if _, err := r.waitForRPC(ctx, containerIP, spec.RPCPort()); err != nil {
			return nil, fmt.Errorf("waiting for RPC after schelk promote: %w", err)
		}

		log.Info("Promoted; checkpointing the client on the promoted baseline")
	}

	// In-place mode keeps the checkpoint data in the container's storage
	// directory and repeatedly restores the same container, skipping the
	// per-test archive extraction and container creation of the
	// export/import path. tmpfs_threshold/tmpfs_max_size only apply to the
	// export archive, so they are ignored in this mode.
	restoreInPlace := r.cfg.FullConfig.GetCheckpointRestoreInPlace(params.Instance)

	// 3. Decide checkpoint export path: tmpfs (RAM) or disk.
	//
	// When checkpoint_tmpfs_threshold is configured and the container's
	// current memory usage is under the threshold, store the checkpoint
	// on a tmpfs mount to eliminate disk I/O during restores.
	exportPath := filepath.Join(resultsDir, "checkpoint.tar")
	tmpfsDir := ""

	thresholdStr := r.cfg.FullConfig.GetCheckpointTmpfsThreshold(params.Instance)
	if restoreInPlace {
		thresholdStr = ""
	}

	if thresholdStr != "" {
		threshold, parseErr := config.ParseByteSize(thresholdStr)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing checkpoint_tmpfs_threshold: %w", parseErr)
		}

		// Read container memory usage before checkpoint.
		reader, readerErr := stats.NewReader(
			r.log, r.getDockerClient(), containerID,
		)
		if readerErr != nil {
			log.WithError(readerErr).Warn(
				"Failed to create stats reader for tmpfs decision, using disk",
			)
		} else {
			containerStats, statsErr := reader.ReadStats()
			_ = reader.Close()

			if statsErr != nil {
				log.WithError(statsErr).Warn(
					"Failed to read container stats for tmpfs decision, using disk",
				)
			} else if containerStats.Memory > 0 && containerStats.Memory <= threshold {
				dir, mkErr := os.MkdirTemp("", "benchmarkoor-cp-tmpfs-")
				if mkErr != nil {
					log.WithError(mkErr).Warn(
						"Failed to create tmpfs dir, using disk",
					)
				} else {
					// Mount tmpfs sized to the configured max, or
					// 2x the threshold as a default.
					tmpfsMaxSize := r.cfg.FullConfig.GetCheckpointTmpfsMaxSize(params.Instance)

					var tmpfsSize uint64
					if tmpfsMaxSize > 0 {
						tmpfsSize = tmpfsMaxSize
					} else {
						tmpfsSize = threshold * 2
					}

					//nolint:gosec // Arguments are computed, not user-supplied.
					mountCmd := exec.CommandContext(
						ctx, "mount", "-t", "tmpfs",
						"-o", fmt.Sprintf("size=%d", tmpfsSize),
						"tmpfs", dir,
					)
					if mountOut, mountErr := mountCmd.CombinedOutput(); mountErr != nil {
						log.WithError(mountErr).WithField(
							"output", string(mountOut),
						).Warn("Failed to mount tmpfs, using disk")

						_ = os.Remove(dir)
					} else {
						tmpfsDir = dir
						exportPath = filepath.Join(dir, "checkpoint.tar")

						log.WithFields(logrus.Fields{
							"memory_bytes":    containerStats.Memory,
							"threshold_bytes": threshold,
							"tmpfs_size":      tmpfsSize,
						}).Info("Using tmpfs for checkpoint storage")
					}
				}
			} else {
				log.WithFields(logrus.Fields{
					"memory_bytes":    containerStats.Memory,
					"threshold_bytes": threshold,
				}).Info("Container memory exceeds tmpfs threshold, using disk")
			}
		}
	}

	// 4. Checkpoint the running container.
	//
	// Close idle HTTP connections first so there are no ESTABLISHED TCP
	// connections inside the container (from RPC readiness checks). CRIU
	// refuses to checkpoint established connections without --tcp-established,
	// and restoring them fails when the container IP changes.
	http.DefaultClient.CloseIdleConnections()
	time.Sleep(200 * time.Millisecond) // Let server-side sockets close.

	waitAfterTCPDrop := r.cfg.FullConfig.GetCheckpointWaitAfterTCPDropConns(params.Instance)

	log.Info("Checkpointing container")

	cpStart := time.Now()

	if restoreInPlace {
		if err := cpMgr.CheckpointContainerLocal(ctx, containerID, waitAfterTCPDrop); err != nil {
			return nil, fmt.Errorf("checkpointing container locally: %w", err)
		}
	} else {
		if err := cpMgr.CheckpointContainer(ctx, containerID, exportPath, waitAfterTCPDrop); err != nil {
			return nil, fmt.Errorf("checkpointing container: %w", err)
		}
	}

	log.WithField("duration", time.Since(cpStart)).Info(
		"Container checkpointed",
	)

	defer func() {
		if restoreInPlace {
			return
		}

		_ = os.Remove(exportPath)

		if tmpfsDir != "" {
			//nolint:gosec // Path is from os.MkdirTemp, not user-supplied.
			if umountErr := exec.Command("umount", tmpfsDir).Run(); umountErr != nil {
				log.WithError(umountErr).Warn("Failed to unmount tmpfs")
			}

			_ = os.Remove(tmpfsDir)
		}
	}()

	// Wait for log drain after checkpoint (container has stopped).
	waitForLogDrain(logDone, logCancel, logDrainTimeout)

	// 5. Flush dirty pages and snapshot the data directory.
	//
	// The checkpointed process used mmap (MAP_SHARED) for its database,
	// so dirty pages may still be in the page cache. Sync ensures they
	// are flushed before the snapshot, so the snapshot captures the exact
	// filesystem state that matches the checkpointed memory.
	if syncErr := exec.Command("sync").Run(); syncErr != nil {
		log.WithError(syncErr).Warn("Failed to sync before data directory snapshot")
	}

	// Create the data directory snapshot using either ZFS or copy-based
	// strategy. Both are abstracted behind rollback/cleanup callbacks.
	type snapshotRollback struct {
		rollback func(ctx context.Context) error
		cleanup  func()
	}

	var sr snapshotRollback

	if useZFS {
		zfsMgr := datadir.NewCheckpointZFSManager(r.log)

		snapshot, snapErr := zfsMgr.SnapshotReady(ctx, &datadir.CheckpointConfig{
			DataDir:    dataMountSource,
			InstanceID: params.Instance.ID,
		})
		if snapErr != nil {
			return nil, fmt.Errorf("creating ready-state ZFS snapshot: %w", snapErr)
		}

		sr = snapshotRollback{
			rollback: func(ctx context.Context) error {
				return zfsMgr.RollbackToReady(ctx, snapshot)
			},
			cleanup: func() {
				if destroyErr := zfsMgr.DestroySnapshot(snapshot); destroyErr != nil {
					log.WithError(destroyErr).Warn(
						"Failed to destroy ready-state ZFS snapshot",
					)
				}
			},
		}
	} else {
		copyMgr := datadir.NewCheckpointCopyManager(r.log)

		snapshot, snapErr := copyMgr.SnapshotReady(ctx, &datadir.CheckpointConfig{
			DataDir:    dataMountSource,
			InstanceID: params.Instance.ID,
		})
		if snapErr != nil {
			return nil, fmt.Errorf("creating ready-state copy snapshot: %w", snapErr)
		}

		sr = snapshotRollback{
			rollback: func(ctx context.Context) error {
				return copyMgr.RollbackToReady(ctx, snapshot)
			},
			cleanup: func() {
				if destroyErr := copyMgr.DestroySnapshot(snapshot); destroyErr != nil {
					log.WithError(destroyErr).Warn(
						"Failed to destroy ready-state copy snapshot",
					)
				}
			},
		}
	}

	defer sr.cleanup()

	log.Info("Container checkpointed, starting per-test restore loop")

	// 6. Per-test restore loop.
	combined := &executor.ExecutionResult{}
	startTime := time.Now()

	for i, test := range tests {
		select {
		case <-ctx.Done():
			combined.TotalDuration = time.Since(startTime)

			return combined, ctx.Err()
		default:
		}

		testLog := log.WithFields(logrus.Fields{
			"test":  test.Name,
			"index": fmt.Sprintf("%d/%d", i+1, len(tests)),
		})

		testLog.Info("Preparing test: rolling back data directory and restoring container")

		// Flush dirty pages and drop caches BEFORE rollback. The killed
		// container's MAP_SHARED mmap writes leave dirty pages in the
		// page cache. Writing to drop_caches triggers a sync first, which
		// would write those stale pages onto the rolled-back dataset —
		// effectively undoing the rollback for recently-written blocks
		// (e.g. MDBX meta-pages). By syncing and dropping caches before
		// the rollback, we ensure no dirty pages survive to corrupt the
		// post-rollback state.
		if dropCachesPath != "" {
			if syncErr := exec.Command("sync").Run(); syncErr != nil {
				testLog.WithError(syncErr).Warn("Failed to sync before rollback")
			}

			if cacheErr := os.WriteFile(dropCachesPath, []byte("3"), 0); cacheErr != nil {
				testLog.WithError(cacheErr).Warn("Failed to drop page caches before rollback")
			}
		}

		// Roll back the data directory to the ready-state snapshot so
		// the container restores onto clean data at the same mount path.
		if err := sr.rollback(ctx); err != nil {
			combined.TotalDuration = time.Since(startTime)

			return combined, fmt.Errorf("rolling back data directory for test %d: %w", i, err)
		}

		// Restore container from checkpoint.
		var (
			restoreName string
			restoredID  string
			err         error
		)

		restoreStart := time.Now()

		if restoreInPlace {
			// Restore the original container from its kept checkpoint
			// data — same ID, name, mounts, and IP every iteration.
			restoreName = params.ContainerSpec.Name
			restoredID = containerID

			testLog.Info("Restoring container in place from checkpoint")

			if err = cpMgr.RestoreContainerInPlace(ctx, containerID); err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"restoring container in place for test %d: %w", i, err,
				)
			}
		} else {
			restoreName = fmt.Sprintf("%s-restore-%d", params.ContainerSpec.Name, i)

			testLog.Info("Restoring container from checkpoint")

			restoredID, err = cpMgr.RestoreContainer(ctx, exportPath, &podman.RestoreOptions{
				Name:        restoreName,
				NetworkName: r.cfg.ContainerNetwork,
			})
			if err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("restoring container for test %d: %w", i, err)
			}
		}

		testLog.WithField("duration", time.Since(restoreStart)).Info(
			"Container restored from checkpoint",
		)

		// Register cleanup for this iteration. In-place mode reuses the
		// original container, whose removal is already handled by the
		// container lifecycle cleanup.
		if !restoreInPlace {
			iterID := restoredID

			*cleanupFuncs = append(*cleanupFuncs, func() {
				if rmErr := r.containerMgr.RemoveContainer(
					context.Background(), iterID,
				); rmErr != nil && !isContainerNotFound(rmErr) {
					testLog.WithError(rmErr).Warn("Failed to remove restored container")
				}
			})
		}

		// Get container IP.
		restoredIP, err := r.containerMgr.GetContainerIP(
			ctx, restoredID, r.cfg.ContainerNetwork,
		)
		if err != nil {
			combined.TotalDuration = time.Since(startTime)

			return combined, fmt.Errorf("getting container IP for test %d: %w", i, err)
		}

		// Start log streaming for restored container.
		if logErr := r.startLogStreaming(
			ctx, resultsDir,
			params.Instance.ID, restoredID,
			benchmarkoorLog, &containerLogInfo{
				Name:             restoreName,
				ContainerID:      restoredID,
				Image:            params.ContainerSpec.Image,
				GenesisGroupHash: params.GenesisGroupHash,
			},
			params.BlockLogCollector, cleanupStarted,
			logDone, logCancel, cleanupFuncs,
		); logErr != nil {
			combined.TotalDuration = time.Since(startTime)

			return combined, fmt.Errorf(
				"log streaming for test %d: %w", i, logErr,
			)
		}

		// No waitForRPC needed — process resumes at checkpoint state.
		testLog.Info("Executing test (restored from checkpoint)")

		// Execute single test with no executor-level rollback.
		execOpts := &executor.ExecuteOptions{
			EngineEndpoint: fmt.Sprintf(
				"http://%s:%d", restoredIP, spec.EnginePort(),
			),
			JWT:              r.cfg.JWT,
			ResultsDir:       resultsDir,
			Filter:           r.cfg.TestFilter,
			ContainerID:      restoredID,
			DockerClient:     r.getDockerClient(),
			DropMemoryCaches: dropMemoryCaches,
			DropCachesPath:   dropCachesPath,
			RollbackStrategy: config.RollbackStrategyNone,
			RPCEndpoint: fmt.Sprintf(
				"http://%s:%d", restoredIP, spec.RPCPort(),
			),
			Tests:                         []*executor.TestWithSteps{test},
			BlockLogCollector:             params.BlockLogCollector,
			RetryNewPayloadsSyncingConfig: r.cfg.FullConfig.GetRetryNewPayloadsSyncingState(params.Instance),
			RetryNewPayloadsFailedConfig:  r.cfg.FullConfig.GetRetryNewPayloadsFailedState(params.Instance),
			PostTestRPCCalls:              r.cfg.FullConfig.GetPostTestRPCCalls(params.Instance),
			OpcodeExtraction:              r.cfg.FullConfig.GetOpcodeExtraction(params.Instance),
			PostTestSleepDuration:         r.cfg.FullConfig.GetPostTestSleepDuration(params.Instance),
		}

		result, execErr := r.executor.ExecuteTests(ctx, execOpts)
		if execErr != nil {
			testLog.WithError(execErr).Error("Test execution failed")
		}

		// Tear the restored container down for the next iteration. Use a
		// fresh context so this succeeds even if the parent was cancelled
		// (CTRL+C). In-place mode only stops the container (SIGKILL via
		// zero timeout — the datadir rollback discards its writes anyway),
		// keeping it and its checkpoint data for the next restore; the
		// export/import mode removes the throwaway container entirely.
		rmStart := time.Now()
		rmCtx, rmCancel := context.WithTimeout(
			context.Background(), 30*time.Second,
		)

		if restoreInPlace {
			testLog.Info("Stopping restored container")

			zeroTimeout := 0
			if stopErr := r.containerMgr.StopContainer(
				rmCtx, restoredID, &zeroTimeout,
			); stopErr != nil && !isContainerNotFound(stopErr) {
				testLog.WithError(stopErr).Warn(
					"Failed to stop restored container",
				)
			}
		} else {
			testLog.Info("Force-removing restored container")

			if rmErr := r.containerMgr.RemoveContainer(
				rmCtx, restoredID,
			); rmErr != nil && !isContainerNotFound(rmErr) {
				testLog.WithError(rmErr).Warn(
					"Failed to remove restored container",
				)
			}
		}

		rmCancel()

		testLog.WithField("duration", time.Since(rmStart)).Info(
			"Restored container torn down",
		)

		waitForLogDrain(logDone, logCancel, logDrainTimeout)

		// Aggregate results.
		if result != nil {
			combined.TotalTests += result.TotalTests
			combined.Passed += result.Passed
			combined.Failed += result.Failed

			if result.StatsReaderType != "" {
				combined.StatsReaderType = result.StatsReaderType
			}

			if result.ContainerDied {
				combined.ContainerDied = true
				combined.TotalDuration = time.Since(startTime)

				return combined, nil
			}
		}
	}

	combined.TotalDuration = time.Since(startTime)

	return combined, nil
}

// isContainerNotFound returns true if the error indicates the container no
// longer exists. This is expected when cleanup runs after a container was
// already removed (e.g., restored containers removed after each test).
func isContainerNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such container")
}
