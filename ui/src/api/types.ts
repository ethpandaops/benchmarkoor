// index.json
export interface Index {
  generated: number
  entries: IndexEntry[]
}

// Run status type
export type RunStatus = 'completed' | 'container_died' | 'cancelled'

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
  retry_new_payloads_syncing_state?: RetryNewPayloadsSyncingConfig
  resource_limits?: ResourceLimitsConfig
  post_test_rpc_calls?: PostTestRPCCallConfig[]
  checkpoint_restore_strategy_options?: CheckpointRestoreStrategyOptions
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
  eest?: {
    github_repo: string
    github_release?: string
    fixtures_url?: string
    genesis_url?: string
    fixtures_subdir?: string
    fixtures_artifact_name?: string
    genesis_artifact_name?: string
    fixtures_artifact_run_id?: string
    genesis_artifact_run_id?: string
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
  timing: BlockLogTiming
  throughput: BlockLogThroughput
  state_reads: BlockLogStateReads
  state_writes: BlockLogStateWrites
  cache: BlockLogCache
}

export type BlockLogs = Record<string, BlockLogEntry>
