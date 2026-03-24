package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/sirupsen/logrus"
)

// runTestsWithContainerStrategy executes tests one at a time, manipulating
// the container between tests according to the given strategy.
//
//nolint:gocognit,cyclop // Per-test container manipulation is inherently complex.
func (r *runner) runTestsWithContainerStrategy(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	containerID string,
	containerIP string,
	strategy string,
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
		"strategy": strategy,
	})

	// Resolve the test list.
	tests := params.Tests
	if tests == nil {
		tests = r.executor.GetTests()
	}

	if len(tests) == 0 {
		return &executor.ExecutionResult{}, nil
	}

	log.WithField("tests", len(tests)).Info(
		"Running tests with container-level rollback strategy",
	)

	// Detect ZFS snapshot optimization: when the strategy is
	// container-recreate with a ZFS datadir, we snapshot the data
	// directory after the first RPC-ready state, then rollback to
	// that snapshot between tests instead of cloning from scratch.
	useZFSSnapshot := strategy == config.RollbackStrategyContainerRecreate &&
		params.DataDirCfg != nil && params.DataDirCfg.Method == "zfs"

	// snapshotRollback holds the rollback/cleanup callbacks for the
	// ZFS snapshot path. Only populated when useZFSSnapshot is true.
	type snapshotRollback struct {
		rollback func(ctx context.Context) error
		cleanup  func()
	}

	var sr *snapshotRollback

	if useZFSSnapshot {
		log.Info(
			"ZFS datadir detected: will snapshot after RPC-ready " +
				"and rollback between tests",
		)

		// Run pre-run steps on the live container before snapshotting.
		// These steps (e.g., genesis setup) must be baked into the
		// snapshot so every recreated container starts post-pre-run.
		engineEndpoint := fmt.Sprintf(
			"http://%s:%d", containerIP, spec.EnginePort(),
		)

		preRunOpts := &executor.ExecuteOptions{
			EngineEndpoint: engineEndpoint,
			JWT:            r.cfg.JWT,
			ResultsDir:     resultsDir,
		}

		if n, err := r.executor.RunPreRunSteps(ctx, preRunOpts); err != nil {
			return nil, fmt.Errorf("running pre-run steps before ZFS snapshot: %w", err)
		} else if n > 0 {
			log.WithField("steps", n).Info(
				"Pre-run steps completed before ZFS snapshot",
			)
		}

		// Stop the initial container so writes are flushed to disk.
		log.Info("Stopping container for ZFS snapshot")

		stopStart := time.Now()

		if err := r.containerMgr.StopContainer(ctx, containerID); err != nil {
			return nil, fmt.Errorf("stopping container for ZFS snapshot: %w", err)
		}

		log.WithField("duration", time.Since(stopStart)).Info(
			"Container stopped for ZFS snapshot",
		)

		waitForLogDrain(logDone, logCancel, logDrainTimeout)

		// Sync to flush any dirty pages before snapshotting.
		if syncErr := exec.Command("sync").Run(); syncErr != nil {
			log.WithError(syncErr).Warn(
				"Failed to sync before ZFS snapshot",
			)
		}

		// Find the data mount source path from the container spec.
		containerDir := spec.DataDir()
		if params.DataDirCfg.ContainerDir != "" {
			containerDir = params.DataDirCfg.ContainerDir
		}

		dataMountSource := ""

		for _, mnt := range params.ContainerSpec.Mounts {
			if mnt.Target == containerDir {
				dataMountSource = mnt.Source

				break
			}
		}

		if dataMountSource == "" {
			return nil, fmt.Errorf(
				"could not find data mount for %s in container spec",
				containerDir,
			)
		}

		// Take the ready-state ZFS snapshot.
		zfsMgr := datadir.NewCheckpointZFSManager(r.log)

		snapshot, snapErr := zfsMgr.SnapshotReady(
			ctx, &datadir.CheckpointConfig{
				DataDir:    dataMountSource,
				InstanceID: params.Instance.ID,
			},
		)
		if snapErr != nil {
			return nil, fmt.Errorf(
				"creating ready-state ZFS snapshot: %w", snapErr,
			)
		}

		sr = &snapshotRollback{
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

		defer sr.cleanup()

		// Remove the initial container (we'll create fresh ones per test).
		log.Info("Removing initial container")

		rmStart := time.Now()

		if err := r.containerMgr.RemoveContainer(
			ctx, containerID,
		); err != nil {
			log.WithError(err).Warn("Failed to remove initial container")
		}

		log.WithField("duration", time.Since(rmStart)).Info(
			"Initial container removed",
		)
	}

	combined := &executor.ExecutionResult{}
	startTime := time.Now()
	currentContainerID := containerID
	currentContainerIP := containerIP

	for i, test := range tests {
		select {
		case <-ctx.Done():
			log.Info("Context cancelled, stopping current container")

			// Stop the container first so its stdio closes and
			// the Podman attach connection (used by StreamLogs)
			// receives EOF. Without this, RemoveContainer blocks
			// because the active attach holds a server-side lock.
			stopCtx, stopCancel := context.WithTimeout(
				context.Background(), 30*time.Second,
			)

			stopStart := time.Now()

			if err := r.containerMgr.StopContainer(
				stopCtx, currentContainerID,
			); err != nil {
				log.WithError(err).Debug(
					"Failed to stop container on cancellation",
				)
			}

			stopCancel()

			log.WithField("duration", time.Since(stopStart)).Info(
				"Container stopped on cancellation",
			)

			waitForLogDrain(logDone, logCancel, logDrainTimeout)

			// Remove the stopped container.
			rmCtx, rmCancel := context.WithTimeout(
				context.Background(), 30*time.Second,
			)

			rmStart := time.Now()

			if err := r.containerMgr.RemoveContainer(
				rmCtx, currentContainerID,
			); err != nil {
				log.WithError(err).Warn(
					"Failed to remove container on cancellation",
				)
			}

			rmCancel()

			log.WithField("duration", time.Since(rmStart)).Info(
				"Container removed on cancellation",
			)

			combined.TotalDuration = time.Since(startTime)

			return combined, ctx.Err()
		default:
		}

		testLog := log.WithFields(logrus.Fields{
			"test":  test.Name,
			"index": fmt.Sprintf("%d/%d", i+1, len(tests)),
		})

		// Restore state before test.
		switch {
		case useZFSSnapshot:
			// ZFS snapshot path: rollback datadir, create a fresh
			// container on the same mount, start, wait for RPC.
			testLog.Info("Rolling back ZFS snapshot for next test")

			if i > 0 {
				// Force-remove container from previous test (no graceful
				// stop needed — ZFS rollback discards the datadir anyway).
				testLog.Info("Force-removing container before ZFS rollback")

				rmStart := time.Now()

				if err := r.containerMgr.RemoveContainer(
					ctx, currentContainerID,
				); err != nil {
					testLog.WithError(err).Warn(
						"Failed to remove container",
					)
				}

				testLog.WithField("duration", time.Since(rmStart)).Info(
					"Container removed before ZFS rollback",
				)

				waitForLogDrain(logDone, logCancel, logDrainTimeout)
			}

			// Rollback the ZFS dataset to the ready-state snapshot.
			if err := sr.rollback(ctx); err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"rolling back ZFS snapshot for test %d: %w", i, err,
				)
			}

			// Create a new container using the same mount path.
			newSpec := *params.ContainerSpec
			newSpec.Name = fmt.Sprintf("%s-%d", params.ContainerSpec.Name, i)
			newSpec.Mounts = make(
				[]docker.Mount, len(params.ContainerSpec.Mounts),
			)
			copy(newSpec.Mounts, params.ContainerSpec.Mounts)

			newID, err := r.containerMgr.CreateContainer(ctx, &newSpec)
			if err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"creating container for test %d: %w", i, err,
				)
			}

			currentContainerID = newID

			*cleanupFuncs = append(*cleanupFuncs, func() {
				if rmErr := r.containerMgr.RemoveContainer(
					context.Background(), newID,
				); rmErr != nil {
					testLog.WithError(rmErr).Warn(
						"Failed to remove recreated container",
					)
				}
			})

			// Start fresh log streaming.
			if err := r.startLogStreaming(
				ctx, resultsDir,
				params.Instance.ID, newID,
				benchmarkoorLog, &containerLogInfo{
					Name:             newSpec.Name,
					ContainerID:      newID,
					Image:            newSpec.Image,
					GenesisGroupHash: params.GenesisGroupHash,
				},
				params.BlockLogCollector, cleanupStarted,
				logDone, logCancel, cleanupFuncs,
			); err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"log streaming for test %d: %w", i, err,
				)
			}

			// Start the new container.
			if err := r.containerMgr.StartContainer(ctx, newID); err != nil {
				waitForLogDrain(logDone, logCancel, logDrainTimeout)
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"starting container for test %d: %w", i, err,
				)
			}

			// Get new container IP.
			newIP, err := r.containerMgr.GetContainerIP(
				ctx, newID, r.cfg.ContainerNetwork,
			)
			if err != nil {
				waitForLogDrain(logDone, logCancel, logDrainTimeout)
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"getting container IP for test %d: %w", i, err,
				)
			}

			currentContainerIP = newIP

			// Wait for RPC to be ready.
			clientVersion, rpcErr := r.waitForRPC(
				ctx, currentContainerIP, spec.RPCPort(),
			)
			if rpcErr != nil {
				waitForLogDrain(logDone, logCancel, logDrainTimeout)
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"waiting for RPC on test %d: %w", i, rpcErr,
				)
			}

			testLog.WithField("version", clientVersion).Info(
				"RPC endpoint ready",
			)

			// Honour the post-RPC-ready wait if configured.
			if r.cfg.FullConfig != nil {
				if waitDuration := r.cfg.FullConfig.GetWaitAfterRPCReady(
					params.Instance,
				); waitDuration > 0 {
					testLog.WithField("duration", waitDuration).Info(
						"Waiting after RPC ready",
					)

					select {
					case <-time.After(waitDuration):
					case <-ctx.Done():
						combined.TotalDuration = time.Since(startTime)

						return combined, ctx.Err()
					}
				}
			}

			// Log the latest block info.
			blockNum, blockHash, stateRoot, blkErr := r.getLatestBlock(
				ctx, currentContainerIP, spec.RPCPort(),
			)
			if blkErr != nil {
				testLog.WithError(blkErr).Warn("Failed to get latest block")
			} else {
				testLog.WithFields(logrus.Fields{
					"block_number": blockNum,
					"block_hash":   blockHash,
					"state_root":   stateRoot,
				}).Info("Latest block")
			}

			// Send bootstrap FCU if configured.
			if r.cfg.FullConfig != nil {
				if fcuCfg := r.cfg.FullConfig.GetBootstrapFCU(
					params.Instance,
				); fcuCfg != nil && fcuCfg.Enabled {
					blkHash := fcuCfg.HeadBlockHash
					if blkHash == "" {
						var blkErr error
						_, blkHash, _, blkErr = r.getLatestBlock(
							ctx, currentContainerIP, spec.RPCPort(),
						)

						if blkErr != nil {
							testLog.WithError(blkErr).Warn(
								"Failed to get latest block " +
									"for bootstrap FCU",
							)
						}
					}

					if blkHash != "" {
						if fcuErr := r.sendBootstrapFCU(
							ctx, testLog, currentContainerIP,
							spec.EnginePort(), blkHash, fcuCfg,
						); fcuErr != nil {
							testLog.WithError(fcuErr).Error(
								"Bootstrap FCU failed",
							)
							waitForLogDrain(
								logDone, logCancel, logDrainTimeout,
							)
							combined.TotalDuration = time.Since(startTime)

							return combined, fmt.Errorf(
								"sending bootstrap FCU for test %d: %w",
								i, fcuErr,
							)
						}
					}
				}
			}

		case strategy == config.RollbackStrategyContainerRecreate && i > 0:
			testLog.Info("Recreating container for next test")

			// Stop container first so Docker flushes remaining logs.
			testLog.Info("Stopping container for recreate")

			stopStart := time.Now()

			if err := r.containerMgr.StopContainer(
				ctx, currentContainerID,
			); err != nil {
				testLog.WithError(err).Warn("Failed to stop container")
			}

			testLog.WithField("duration", time.Since(stopStart)).Info(
				"Container stopped for recreate",
			)

			// Wait for the log-streaming goroutine to finish.
			waitForLogDrain(logDone, logCancel, logDrainTimeout)

			// Remove the stopped container.
			testLog.Info("Removing stopped container")

			rmStart := time.Now()

			if err := r.containerMgr.RemoveContainer(
				ctx, currentContainerID,
			); err != nil {
				testLog.WithError(err).Warn("Failed to remove container")
			}

			testLog.WithField("duration", time.Since(rmStart)).Info(
				"Container removed for recreate",
			)

			// Create a fresh data volume/datadir for the new container.
			newSpec := *params.ContainerSpec
			newSpec.Name = fmt.Sprintf("%s-%d", params.ContainerSpec.Name, i)
			newSpec.Mounts = make([]docker.Mount, len(params.ContainerSpec.Mounts))
			copy(newSpec.Mounts, params.ContainerSpec.Mounts)

			freshMount, mountCleanup, err := r.createFreshDataMount(
				ctx, params, spec, i,
			)
			if err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"creating fresh data mount for test %d: %w", i, err,
				)
			}

			if mountCleanup != nil {
				*cleanupFuncs = append(*cleanupFuncs, mountCleanup)
			}

			// Replace the data mount (index 0) with the fresh one.
			newSpec.Mounts[0] = freshMount

			// Run init container if required to populate the fresh volume.
			if spec.RequiresInit() && !params.UseDataDir &&
				params.GenesisSource != "" {
				testLog.Info("Running init container for fresh volume")

				initMounts := make([]docker.Mount, len(newSpec.Mounts))
				copy(initMounts, newSpec.Mounts)

				if err := r.runInitForRecreate(
					ctx, params, spec, initMounts, resultsDir,
					benchmarkoorLog, i,
				); err != nil {
					combined.TotalDuration = time.Since(startTime)

					return combined, fmt.Errorf(
						"running init container for test %d: %w", i, err,
					)
				}
			}

			newID, err := r.containerMgr.CreateContainer(ctx, &newSpec)
			if err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("creating container for test %d: %w", i, err)
			}

			currentContainerID = newID

			// Update cleanup to remove this container on exit.
			*cleanupFuncs = append(*cleanupFuncs, func() {
				if rmErr := r.containerMgr.RemoveContainer(
					context.Background(), newID,
				); rmErr != nil {
					testLog.WithError(rmErr).Warn(
						"Failed to remove recreated container",
					)
				}
			})

			// Start fresh log streaming.
			if err := r.startLogStreaming(
				ctx, resultsDir,
				params.Instance.ID, newID,
				benchmarkoorLog, &containerLogInfo{
					Name:             newSpec.Name,
					ContainerID:      newID,
					Image:            newSpec.Image,
					GenesisGroupHash: params.GenesisGroupHash,
				},
				params.BlockLogCollector, cleanupStarted,
				logDone, logCancel, cleanupFuncs,
			); err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"log streaming for test %d: %w", i, err,
				)
			}

			// Start the new container.
			if err := r.containerMgr.StartContainer(ctx, newID); err != nil {
				waitForLogDrain(logDone, logCancel, logDrainTimeout)
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("starting container for test %d: %w", i, err)
			}

			// Get new container IP.
			newIP, err := r.containerMgr.GetContainerIP(
				ctx, newID, r.cfg.ContainerNetwork,
			)
			if err != nil {
				waitForLogDrain(logDone, logCancel, logDrainTimeout)
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("getting container IP for test %d: %w", i, err)
			}

			currentContainerIP = newIP

			// Wait for RPC to be ready.
			clientVersion, rpcErr := r.waitForRPC(
				ctx, currentContainerIP, spec.RPCPort(),
			)
			if rpcErr != nil {
				waitForLogDrain(logDone, logCancel, logDrainTimeout)
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("waiting for RPC on test %d: %w", i, rpcErr)
			}

			testLog.WithField("version", clientVersion).Info(
				"RPC endpoint ready",
			)

			// Honour the post-RPC-ready wait if configured.
			if r.cfg.FullConfig != nil {
				if waitDuration := r.cfg.FullConfig.GetWaitAfterRPCReady(
					params.Instance,
				); waitDuration > 0 {
					testLog.WithField("duration", waitDuration).Info(
						"Waiting after RPC ready",
					)

					select {
					case <-time.After(waitDuration):
					case <-ctx.Done():
						combined.TotalDuration = time.Since(startTime)

						return combined, ctx.Err()
					}
				}
			}

			// Log the latest block info.
			blockNum, blockHash, stateRoot, blkErr := r.getLatestBlock(
				ctx, currentContainerIP, spec.RPCPort(),
			)
			if blkErr != nil {
				testLog.WithError(blkErr).Warn("Failed to get latest block")
			} else {
				testLog.WithFields(logrus.Fields{
					"block_number": blockNum,
					"block_hash":   blockHash,
					"state_root":   stateRoot,
				}).Info("Latest block")
			}

			// Send bootstrap FCU if configured.
			if r.cfg.FullConfig != nil {
				if fcuCfg := r.cfg.FullConfig.GetBootstrapFCU(params.Instance); fcuCfg != nil && fcuCfg.Enabled {
					blkHash := fcuCfg.HeadBlockHash
					if blkHash == "" {
						var blkErr error
						_, blkHash, _, blkErr = r.getLatestBlock(
							ctx, currentContainerIP, spec.RPCPort(),
						)

						if blkErr != nil {
							testLog.WithError(blkErr).Warn(
								"Failed to get latest block for bootstrap FCU",
							)
						}
					}

					if blkHash != "" {
						if fcuErr := r.sendBootstrapFCU(
							ctx, testLog, currentContainerIP,
							spec.EnginePort(), blkHash, fcuCfg,
						); fcuErr != nil {
							testLog.WithError(fcuErr).Error(
								"Bootstrap FCU failed",
							)
							waitForLogDrain(logDone, logCancel, logDrainTimeout)
							combined.TotalDuration = time.Since(startTime)

							return combined, fmt.Errorf(
								"sending bootstrap FCU for test %d: %w",
								i, fcuErr,
							)
						}
					}
				}
			}

		}

		// Run pre-run steps on fresh containers (non-ZFS paths).
		// For ZFS, pre-run steps are baked into the snapshot already.
		if !useZFSSnapshot {
			preRunOpts := &executor.ExecuteOptions{
				EngineEndpoint: fmt.Sprintf(
					"http://%s:%d", currentContainerIP, spec.EnginePort(),
				),
				JWT:        r.cfg.JWT,
				ResultsDir: resultsDir,
			}

			if n, err := r.executor.RunPreRunSteps(ctx, preRunOpts); err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"running pre-run steps for test %d: %w", i, err,
				)
			} else if n > 0 {
				testLog.WithField("steps", n).Info(
					"Pre-run steps completed",
				)
			}
		}

		testLog.Info("Executing test")

		// Execute single test via executor with no executor-level rollback.
		execOpts := &executor.ExecuteOptions{
			EngineEndpoint: fmt.Sprintf(
				"http://%s:%d", currentContainerIP, spec.EnginePort(),
			),
			JWT:              r.cfg.JWT,
			ResultsDir:       resultsDir,
			Filter:           r.cfg.TestFilter,
			ContainerID:      currentContainerID,
			DockerClient:     r.getDockerClient(),
			DropMemoryCaches: dropMemoryCaches,
			DropCachesPath:   dropCachesPath,
			RollbackStrategy: config.RollbackStrategyNone,
			RPCEndpoint: fmt.Sprintf(
				"http://%s:%d", currentContainerIP, spec.RPCPort(),
			),
			Tests:                         []*executor.TestWithSteps{test},
			BlockLogCollector:             params.BlockLogCollector,
			RetryNewPayloadsSyncingConfig: r.cfg.FullConfig.GetRetryNewPayloadsSyncingState(params.Instance),
			PostTestRPCCalls:              r.cfg.FullConfig.GetPostTestRPCCalls(params.Instance),
		}

		result, err := r.executor.ExecuteTests(ctx, execOpts)
		if err != nil {
			testLog.WithError(err).Error("Test execution failed")

			continue
		}

		// Aggregate results.
		combined.TotalTests += result.TotalTests
		combined.Passed += result.Passed
		combined.Failed += result.Failed

		if result.StatsReaderType != "" {
			combined.StatsReaderType = result.StatsReaderType
		}

		if result.ContainerDied {
			combined.ContainerDied = true
			combined.TotalDuration = time.Since(startTime)

			// The container may still be running (the executor sets
			// ContainerDied when interrupted, not only on actual
			// death). Stop it first so the attach connection closes
			// and the log goroutine can finish.
			stopCtx, stopCancel := context.WithTimeout(
				context.Background(), 30*time.Second,
			)

			stopStart := time.Now()

			if stopErr := r.containerMgr.StopContainer(
				stopCtx, currentContainerID,
			); stopErr != nil {
				log.WithError(stopErr).Debug(
					"Failed to stop container after death/interruption",
				)
			}

			stopCancel()

			log.WithField("duration", time.Since(stopStart)).Info(
				"Container stopped after death/interruption",
			)

			waitForLogDrain(logDone, logCancel, logDrainTimeout)

			return combined, nil
		}
	}

	combined.TotalDuration = time.Since(startTime)

	waitForLogDrain(logDone, logCancel, logDrainTimeout)

	return combined, nil
}

// createFreshDataMount creates a new volume or datadir for a recreated container.
// Returns the mount, a cleanup function (may be nil), and any error.
func (r *runner) createFreshDataMount(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	iteration int,
) (docker.Mount, func(), error) {
	log := r.log.WithFields(logrus.Fields{
		"instance":  params.Instance.ID,
		"run_id":    params.RunID,
		"iteration": iteration,
	})

	if params.UseDataDir {
		log.Info("Preparing fresh datadir copy")

		provider, err := datadir.NewProvider(log, params.DataDirCfg.Method)
		if err != nil {
			return docker.Mount{}, nil, fmt.Errorf("creating datadir provider: %w", err)
		}

		prepared, err := provider.Prepare(ctx, &datadir.ProviderConfig{
			SourceDir:  params.DataDirCfg.SourceDir,
			InstanceID: fmt.Sprintf("%s-%d", params.Instance.ID, iteration),
			TmpDir:     r.cfg.TmpDataDir,
		})
		if err != nil {
			return docker.Mount{}, nil, fmt.Errorf("preparing datadir: %w", err)
		}

		containerDir := params.DataDirCfg.ContainerDir
		if containerDir == "" {
			containerDir = spec.DataDir()
		}

		cleanup := func() {
			if cleanupErr := prepared.Cleanup(); cleanupErr != nil {
				log.WithError(cleanupErr).Warn("Failed to cleanup recreate datadir")
			}
		}

		return docker.Mount{
			Type:   "bind",
			Source: prepared.MountPath,
			Target: containerDir,
		}, cleanup, nil
	}

	// Container volume path.
	volumeSuffix := params.Instance.ID
	if params.GenesisGroupHash != "" {
		volumeSuffix = params.Instance.ID + "-" + params.GenesisGroupHash
	}

	volumeName := fmt.Sprintf(
		"benchmarkoor-%s-%s-%d", params.RunID, volumeSuffix, iteration,
	)
	volumeLabels := map[string]string{
		"benchmarkoor.instance":   params.Instance.ID,
		"benchmarkoor.client":     params.Instance.Client,
		"benchmarkoor.run-id":     params.RunID,
		"benchmarkoor.managed-by": "benchmarkoor",
	}

	if err := r.containerMgr.CreateVolume(ctx, volumeName, volumeLabels); err != nil {
		return docker.Mount{}, nil, fmt.Errorf("creating volume: %w", err)
	}

	log.WithField("volume", volumeName).Debug("Created fresh volume")

	cleanup := func() {
		if rmErr := r.containerMgr.RemoveVolume(
			context.Background(), volumeName,
		); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove recreate volume")
		}
	}

	return docker.Mount{
		Type:   "volume",
		Source: volumeName,
		Target: spec.DataDir(),
	}, cleanup, nil
}

// runInitForRecreate runs an init container to populate a fresh volume
// during container-recreate strategy.
func (r *runner) runInitForRecreate(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	mounts []docker.Mount,
	resultsDir string,
	benchmarkoorLog *os.File,
	iteration int,
) error {
	instance := params.Instance

	initName := fmt.Sprintf(
		"benchmarkoor-%s-%s-init-%d", params.RunID, instance.ID, iteration,
	)
	if params.GenesisGroupHash != "" {
		initName = fmt.Sprintf(
			"benchmarkoor-%s-%s-%s-init-%d",
			params.RunID, instance.ID, params.GenesisGroupHash, iteration,
		)
	}

	initSpec := &docker.ContainerSpec{
		Name:        initName,
		Image:       params.ImageName,
		Command:     spec.InitCommand(),
		Mounts:      mounts,
		NetworkName: r.cfg.ContainerNetwork,
		Labels: map[string]string{
			"benchmarkoor.instance":   instance.ID,
			"benchmarkoor.client":     instance.Client,
			"benchmarkoor.run-id":     params.RunID,
			"benchmarkoor.type":       "init",
			"benchmarkoor.managed-by": "benchmarkoor",
		},
	}

	initLogFile := filepath.Join(resultsDir, "container.log")

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
		stdoutPW := &prefixedWriter{prefixFn: pfxFn, writer: os.Stdout}
		logPW := &prefixedWriter{prefixFn: pfxFn, writer: benchmarkoorLog}
		initStdout = io.MultiWriter(initFile, stdoutPW, logPW)
		initStderr = io.MultiWriter(initFile, stdoutPW, logPW)
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

	return nil
}
