// index.json
export interface Index {
  generated: number
  entries: IndexEntry[]
}

export interface IndexEntry {
  run_id: string
  timestamp: number
  suite_hash?: string
  instance: {
    id: string
    client: string
    image: string
  }
  tests: {
    success: number
    fail: number
    duration: number
    gas_used: number
    gas_used_duration: number
  }
}

// config.json per run
export interface RunConfig {
  timestamp: number
  suite_hash?: string
  system_resource_collection_method?: string // "cgroupv2" or "dockerstats"
  system: SystemInfo
  instance: InstanceConfig
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

export interface InstanceConfig {
  id: string
  client: string
  image: string
  entrypoint?: string[]
  command?: string[]
  extra_args?: string[]
  pull_policy: string
  restart?: string
  environment?: Record<string, string>
  genesis: string
  datadir?: DataDirConfig
}

// result.json per run
export interface RunResult {
  tests: Record<string, TestEntry>
}

export interface TestEntry {
  dir: string
  filename_hash?: string
  aggregated: AggregatedStats
}

export interface AggregatedStats {
  time_total: number
  gas_used_total: number
  gas_used_time_total: number
  success: number
  fail: number
  msg_count: number
  cpu_usec_total?: number
  memory_delta_total?: number
  disk_read_total?: number
  disk_write_total?: number
  disk_read_iops_total?: number
  disk_write_iops_total?: number
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
}

// summary.json per suite
export interface SuiteInfo {
  hash: string
  source: {
    tests: SourceInfo
    warmup?: SourceInfo
  }
  filter?: string
  warmup?: SuiteFile[]
  tests: SuiteFile[]
}

export interface SourceInfo {
  git?: {
    repo: string
    version: string
    directory?: string
    sha: string
  }
  local_dir?: string
}

export interface SuiteFile {
  f: string
  d?: string
}
