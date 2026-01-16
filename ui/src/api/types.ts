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
  }
}

// config.json per run
export interface RunConfig {
  timestamp: number
  suite_hash?: string
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
  aggregated: AggregatedStats
}

export interface AggregatedStats {
  time_total: number
  success: number
  fail: number
  msg_count: number
  methods: Record<string, MethodStats>
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

// .result-details.json per test
export interface ResultDetails {
  duration_ns: number[]
  status: number[] // 0=success, 1=fail
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
