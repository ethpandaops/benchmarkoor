// index.json
export interface Index {
  generated: number
  entries: IndexEntry[]
}

// Run status type
export type RunStatus = 'running' | 'completed' | 'container_died' | 'cancelled' | 'timeout'

// Live run reported by an active runner via the ingest endpoint. Mirrors
// indexstore.LiveRunResponse on the API side.
export interface LiveRun {
  id: number
  discovery_path: string
  run_id: string
  timestamp: number
  timestamp_end?: number
  suite_hash?: string
  status: RunStatus
  termination_reason?: string
  instance_id?: string
  client?: string
  image?: string
  rollback_strategy?: string
  tests_total: number
  tests_passed: number
  tests_failed: number
  metadata?: Record<string, string>
  // Running totals from the test step of completed tests. Both stay zero
  // until the first test completes, so the UI can guard on
  // total_gas_used_duration_ns > 0 before computing MGas/s.
  total_gas_used?: number
  total_gas_used_duration_ns?: number
  // Per-test gas map, one entry per completed test (success or failure).
  // Drives the live Performance Heatmap. Failed tests carry zero gas data.
  tests?: Record<string, LiveTestStats>
  // The full config.json content as reported by the runner. Mirrors the
  // on-disk config.json schema so the UI can reuse RunConfiguration.
  config?: RunConfig
  last_reported_at: string
}

// LiveTestStats is the per-test record carried in each LiveRun snapshot
// for the live heatmap. Failed tests carry zero gas data.
export interface LiveTestStats {
  passed: boolean
  gas_used?: number
  gas_used_duration_ns?: number
}

export interface IndexEntry {
  run_id: string
  timestamp: number
  timestamp_end?: number
  suite_hash?: string
  instance: {
    id: string
    client: string
    image: string
    rollback_strategy?: string
  }
  tests: {
    tests_total: number
    tests_passed: number
    tests_failed: number
    steps: {
      setup?: IndexStepStats
      test?: IndexStepStats
      cleanup?: IndexStepStats
    }
  }
  status?: RunStatus
  termination_reason?: string
  metadata?: Record<string, string>
}

export interface IndexStepStats {
  success: number
  fail: number
  duration: number
  gas_used: number
  gas_used_duration: number
  resource_totals?: ResourceTotals
}

// Step types that can be included in metric calculations
export type IndexStepType = 'setup' | 'test' | 'cleanup'
export const ALL_INDEX_STEP_TYPES: IndexStepType[] = ['setup', 'test', 'cleanup']
export const DEFAULT_INDEX_STEP_FILTER: IndexStepType[] = ['test']

// Aggregates stats from selected steps (setup, test, cleanup) of an index entry
export function getIndexAggregatedStats(
  entry: IndexEntry,
  stepFilter: IndexStepType[] = ALL_INDEX_STEP_TYPES
): { success: number; fail: number; duration: number; gasUsed: number; gasUsedDuration: number } {
  const steps = entry.tests.steps
  let success = 0
  let fail = 0
  let duration = 0
  let gasUsed = 0
  let gasUsedDuration = 0

  if (stepFilter.includes('setup') && steps.setup) {
    success += steps.setup.success
    fail += steps.setup.fail
    duration += steps.setup.duration
    gasUsed += steps.setup.gas_used
    gasUsedDuration += steps.setup.gas_used_duration
  }

  if (stepFilter.includes('test') && steps.test) {
    success += steps.test.success
    fail += steps.test.fail
    duration += steps.test.duration
    gasUsed += steps.test.gas_used
    gasUsedDuration += steps.test.gas_used_duration
  }

  if (stepFilter.includes('cleanup') && steps.cleanup) {
    success += steps.cleanup.success
    fail += steps.cleanup.fail
    duration += steps.cleanup.duration
    gasUsed += steps.cleanup.gas_used
    gasUsedDuration += steps.cleanup.gas_used_duration
  }

  return { success, fail, duration, gasUsed, gasUsedDuration }
}

// Aggregates gas and time from selected steps of a RunDuration entry
export function getRunDurationAggregatedStats(
  duration: RunDuration,
  stepFilter: IndexStepType[] = ALL_INDEX_STEP_TYPES
): { gasUsed: number; timeNs: number } {
  // If no steps data, fall back to the total values
  if (!duration.steps) {
    return { gasUsed: duration.gas_used, timeNs: duration.time_ns }
  }

  let gasUsed = 0
  let timeNs = 0

  if (stepFilter.includes('setup') && duration.steps.setup) {
    gasUsed += duration.steps.setup.gas_used
    timeNs += duration.steps.setup.time_ns
  }

  if (stepFilter.includes('test') && duration.steps.test) {
    gasUsed += duration.steps.test.gas_used
    timeNs += duration.steps.test.time_ns
  }

  if (stepFilter.includes('cleanup') && duration.steps.cleanup) {
    gasUsed += duration.steps.cleanup.gas_used
    timeNs += duration.steps.cleanup.time_ns
  }

  return { gasUsed, timeNs }
}

// Start block info captured at the beginning of a run.
export interface StartBlock {
  number: number
  hash: string
  state_root: string
}

// config.json per run
export interface RunConfig {
  benchmarkoor_version?: string
  timestamp: number
  timestamp_end?: number
  suite_hash?: string
  system_resource_collection_method?: string // "cgroupv2" or "dockerstats"
  system: SystemInfo
  instance: InstanceConfig
  start_block?: StartBlock
  test_counts?: {
    total: number
    passed: number
    failed: number
  }
  status?: RunStatus
  termination_reason?: string
  container_exit_code?: number
  container_oom_killed?: boolean
  metadata?: {
    labels?: Record<string, string>
  }
}

// .state-actor/state-actor-manifest.json per run (present only when the run's
// snapshot was built by a state-actor that emits the manifest). Mirrors the
// state-actor manifest schema (github.com/ethereum/state-actor).
export interface StateActorManifest {
  schema_version: number
  state_actor: {
    version: string
    go_version: string
    os: string
    arch: string
    vcs_revision?: string
    vcs_time?: string
    vcs_modified: boolean
  }
  generated_at: string
  command: string[]
  flags: {
    client: string
    db: string
    seed: number
    seed_input: number
    fork: string
    fork_input: string
    chain_id: number
    gas_limit: number
    timestamp: number
    extra_data?: string
    target_size?: string
    binary_trie: boolean
    group_depth: number
    archive: boolean
    spec_path?: string
  }
  spec?: {
    input_path: string
    sha256: string
    output_file: string
  }
  result?: {
    state_root: string
    accounts_created: number
    contracts_created: number
    storage_slots: number
    total_db_size_bytes: number
    elapsed_ms: number
  }
  reproduced_from?: string
}

export interface SystemInfo {
  hostname: string
  os: string
  platform: string
  platform_version: string
  kernel_version: string
  arch: string
  virtualization?: string
  virtualization_role?: string
  cpu_vendor: string
  cpu_model: string
  cpu_cores: number
  cpu_mhz: number
  cpu_cache_kb: number
  memory_total_gb: number
}

export interface DataDirConfig {
  source_dir: string
  container_dir?: string
  method?: string
}

export interface ThrottleDeviceConfig {
  path: string
  rate: number
}

export interface BlkioConfig {
  device_read_bps?: ThrottleDeviceConfig[]
  device_read_iops?: ThrottleDeviceConfig[]
  device_write_bps?: ThrottleDeviceConfig[]
  device_write_iops?: ThrottleDeviceConfig[]
}

export interface ResourceLimitsConfig {
  cpuset_cpus?: string
  memory?: string
  memory_bytes?: number
  swap_disabled?: boolean
  blkio_config?: BlkioConfig
  cpu_freq_khz?: number
  cpu_turboboost?: boolean
  cpu_freq_governor?: string
}

export interface RetryNewPayloadsSyncingConfig {
  enabled: boolean
  max_retries: number
  backoff: string
}

export interface RetryNewPayloadsFailedConfig {
  enabled: boolean
  max_retries: number
  backoff: string
}

export interface DumpConfig {
  enabled: boolean
  filename?: string
}

export interface PostTestRPCCallConfig {
  method: string
  params?: unknown[]
  dump?: DumpConfig
}

export interface CheckpointRestoreStrategyOptions {
  tmpfs_threshold?: string
  tmpfs_max_size?: string
  wait_after_tcp_drop_connections?: string
  restart_container?: boolean
}

export interface OpcodeExtractionConfig {
  enabled: boolean
  timeout?: string
}

/**
 * Shape of the per-run `test-opcodes.json` written when
 * `opcode_extraction.enabled` is true. One entry per test name; each
 * entry is an array with one map per `engine_newPayload*` in that test
 * (per-tx counts are summed and the opcode names are uppercased).
 */
export type RunTestOpcodes = Record<string, Array<Record<string, number>>>

export interface InstanceConfig {
  id: string
  client: string
  container_runtime?: string
  image: string
  image_sha256?: string
  entrypoint?: string[]
  command?: string[]
  extra_args?: string[]
  pull_policy: string
  restart?: string
  environment?: Record<string, string>
  genesis?: string
  genesis_groups?: Record<string, string>
  datadir?: DataDirConfig
  client_version?: string
  rollback_strategy?: string
  drop_memory_caches?: string
  wait_after_rpc_ready?: string
  run_timeout?: string
  retry_new_payloads_syncing_state?: RetryNewPayloadsSyncingConfig
  retry_new_payloads_failed_state?: RetryNewPayloadsFailedConfig
  resource_limits?: ResourceLimitsConfig
  post_test_rpc_calls?: PostTestRPCCallConfig[]
  post_test_sleep_duration?: string
  checkpoint_restore_strategy_options?: CheckpointRestoreStrategyOptions
  opcode_extraction?: OpcodeExtractionConfig
}

// result.json per run
export interface RunResult {
  pre_run_steps?: Record<string, StepResult>
  tests: Record<string, TestEntry>
}

export interface StepResult {
  aggregated: AggregatedStats
}

export interface StepsResult {
  setup?: StepResult
  test?: StepResult
  cleanup?: StepResult
}

export interface TestEntry {
  dir: string
  filename_hash?: string
  steps?: StepsResult
}

export interface ResourceTotals {
  cpu_usec: number
  memory_delta_bytes: number
  memory_bytes?: number
  disk_read_bytes: number
  disk_write_bytes: number
  disk_read_iops: number
  disk_write_iops: number
}

export interface AggregatedStats {
  time_total: number
  gas_used_total: number
  gas_used_time_total: number
  success: number
  fail: number
  msg_count: number
  resource_totals?: ResourceTotals
  method_stats: MethodsAggregated
}

export interface MethodsAggregated {
  times: Record<string, MethodStats>
  mgas_s: Record<string, MethodStatsFloat>
}

export interface MethodStats {
  count: number
  last: number
  min?: number
  max?: number
  mean?: number
  p50?: number
  p95?: number
  p99?: number
}

export interface MethodStatsFloat {
  count: number
  last: number
  min?: number
  max?: number
  mean?: number
  p50?: number
  p95?: number
  p99?: number
}

// Resource delta for a single RPC call
export interface ResourceDelta {
  memory_delta_bytes: number
  memory_abs_bytes?: number
  cpu_delta_usec: number
  disk_read_bytes: number
  disk_write_bytes: number
  disk_read_iops: number
  disk_write_iops: number
}

// .result-details.json per test
export interface ResultDetails {
  duration_ns: number[]
  status: number[] // 0=success, 1=fail
  mgas_s: Record<string, number> // map of index -> MGas/s value
  gas_used: Record<string, number> // map of index -> gas used value
  resources?: Record<string, ResourceDelta> // map of index -> resource delta
  original_test_name?: string // original test name when using hashed filenames
  filename_hash?: string // truncated+hash filename when original was too long
}

// stats.json per suite
export interface SuiteStats {
  [testName: string]: TestDurations
}

export interface TestDurations {
  durations: RunDuration[]
}

export interface RunDuration {
  id: string
  client: string
  gas_used: number
  time_ns: number
  run_start: number
  run_end?: number
  steps?: RunDurationStepsStats
}

export interface RunDurationStepsStats {
  setup?: RunDurationStepStats
  test?: RunDurationStepStats
  cleanup?: RunDurationStepStats
}

export interface RunDurationStepStats {
  gas_used: number
  time_ns: number
}

// summary.json per suite
export interface SuiteInfo {
  hash: string
  source: SourceInfo
  filter?: string
  metadata?: {
    labels?: Record<string, string>
  }
  pre_run_steps?: SuiteFile[]
  tests: SuiteTest[]
  // eest_metadata is true when the suite output carries an .eest-meta directory
  // (EEST fill provenance) copied from the fixtures' .meta.
  eest_metadata?: boolean
}

export interface SuiteTestEEST {
  info?: {
    'fixture-format': string
    hash?: string
    opcode_count?: Record<string, number>
    comment?: string
    'filling-transition-tool'?: string
    description?: string
    url?: string
  }
}

export interface SuiteTest {
  name: string
  genesis?: string
  setup?: SuiteFile
  test?: SuiteFile
  cleanup?: SuiteFile
  eest?: SuiteTestEEST
  opcode_count?: Record<string, number>
  /**
   * Per-newPayload byte counts for each step that contains
   * engine_newPayload* calls. Steps with no payload activity are omitted.
   * Within each step, the three arrays are aligned by index — `raw[i]`,
   * `bal[i]`, `snappy[i]` describe the i-th newPayload in step order.
   */
  payload_sizes?: PayloadSizes
  /**
   * Per-newPayload transaction counts (len of payload.transactions) for
   * each step that contains engine_newPayload* calls. Steps with no
   * newPayload activity are omitted.
   */
  tx_counts?: TxCounts
}

/**
 * Per-engine_newPayload byte counts for a single step. All arrays are
 * aligned by index — entry `i` describes the i-th newPayload in step
 * order.
 *
 * - `ssz_full`: full SSZ-encoded ExecutionPayload (BAL inline for Gloas+).
 * - `ssz_bal`: just the SSZ-encoded BlockAccessList sub-field (a subset
 *   of `ssz_full`). Zero for pre-Gloas payloads.
 * - `ssz_full_snappy`: snappy(ssz_full).
 * - `json_full`: canonical JSON encoding of the same ExecutionPayload
 *   (no envelope, no whitespace).
 * - `json_bal`: byte length of the BAL hex string as it appears in JSON
 *   (chars only, not the surrounding quotes). Zero for pre-Gloas payloads.
 */
export interface PayloadSizeBuckets {
  ssz_full: number[]
  ssz_bal: number[]
  ssz_full_snappy: number[]
  json_full: number[]
  json_bal: number[]
}

export interface PayloadSizes {
  setup?: PayloadSizeBuckets
  test?: PayloadSizeBuckets
  cleanup?: PayloadSizeBuckets
}

/**
 * Per-newPayload transaction counts for a single test, broken down by
 * step. Each array has one element per engine_newPayload* line in step
 * order, holding the transaction count for that block.
 */
export interface TxCounts {
  setup?: number[]
  test?: number[]
  cleanup?: number[]
}

export interface SourceInfo {
  git?: {
    repo: string
    version: string
    sha: string
    pre_run_steps?: string[]
    steps?: {
      setup?: string[]
      test?: string[]
      cleanup?: string[]
    }
  }
  local?: {
    base_dir: string
    pre_run_steps?: string[]
    steps?: {
      setup?: string[]
      test?: string[]
      cleanup?: string[]
    }
  }
  archive?: {
    file?: string
    parts?: string[]
    pre_run_steps?: string[]
    steps?: {
      setup?: string[]
      test?: string[]
      cleanup?: string[]
    }
  }
  eest?: {
    github_repo?: string
    github_release?: string
    fixtures_url?: string
    genesis_url?: string
    fixtures_subdir?: string
    fixtures_artifact_name?: string
    genesis_artifact_name?: string
    fixtures_artifact_run_id?: string
    genesis_artifact_run_id?: string
    local_fixtures_dir?: string
    local_genesis_dir?: string
    local_fixtures_tarball?: string
    local_genesis_tarball?: string
  }
}

export interface SuiteFile {
  og_path: string
}

// Block log types from result.block-logs.json
export interface BlockLogBlock {
  number: number
  hash: string
  gas_used: number
  tx_count: number
}

export interface BlockLogTiming {
  execution_ms: number
  state_read_ms: number
  state_hash_ms: number
  commit_ms: number
  total_ms: number
}

export interface BlockLogThroughput {
  mgas_per_sec: number
}

export interface BlockLogStateReads {
  accounts: number
  storage_slots: number
  code: number
  code_bytes: number
}

export interface BlockLogStateWrites {
  accounts: number
  accounts_deleted: number
  storage_slots: number
  storage_slots_deleted: number
  code: number
  code_bytes: number
}

export interface BlockLogCacheEntry {
  hits: number
  misses: number
  hit_rate: number
}

export interface BlockLogCodeCache extends BlockLogCacheEntry {
  hit_bytes: number
  miss_bytes: number
}

export interface BlockLogCache {
  account: BlockLogCacheEntry
  storage: BlockLogCacheEntry
  code: BlockLogCodeCache
}

export interface BlockLogEntry {
  block: BlockLogBlock
  timing?: BlockLogTiming
  throughput?: BlockLogThroughput
  state_reads?: BlockLogStateReads
  state_writes?: BlockLogStateWrites
  cache?: BlockLogCache
}

export type BlockLogs = Record<string, BlockLogEntry>
