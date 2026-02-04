export type TestCategory =
  | 'arithmetic'
  | 'memory'
  | 'storage'
  | 'stack'
  | 'control'
  | 'keccak'
  | 'log'
  | 'account'
  | 'call'
  | 'context'
  | 'system'
  | 'bn128'
  | 'bls'
  | 'precompile'
  | 'scenario'
  | 'other'

export interface ProcessedTestData {
  testName: string
  testOrder: number // Position in suite (1-indexed), Infinity if not in suite
  category: TestCategory
  throughput: number
  executionMs: number
  totalMs: number
  overheadMs: number // state_read + state_hash + commit
  stateReadMs: number
  stateHashMs: number
  commitMs: number
  accountCacheHitRate: number
  storageCacheHitRate: number
  codeCacheHitRate: number
  accountCacheHits: number
  accountCacheMisses: number
  storageCacheHits: number
  storageCacheMisses: number
  codeCacheHits: number
  codeCacheMisses: number
  gasUsed: number
  txCount: number
  // Normalized 0-100 for radar chart
  normalizedThroughput: number
  normalizedSpeed: number
  normalizedLowOverhead: number
  normalizedAccountCache: number
  normalizedCodeCache: number
}

export interface DashboardStats {
  count: number
  avgThroughput: number
  minThroughput: number
  maxThroughput: number
  medianThroughput: number
  avgExecution: number
  avgOverhead: number
  categoryBreakdown: Record<TestCategory, number>
  outlierCount: number
}

export interface BoxPlotStats {
  category: TestCategory
  min: number
  q1: number
  median: number
  q3: number
  max: number
  outliers: number[]
}

export type DashboardTab = 'overview' | 'cache' | 'distribution'
export type SortField = 'throughput' | 'execution' | 'overhead' | 'name' | 'order' | 'category' | 'accountCache' | 'storageCache' | 'codeCache' | 'gas'
export type SortOrder = 'asc' | 'desc'

export interface DashboardState {
  activeTab: DashboardTab
  categories: TestCategory[] // Empty array means all categories
  sortBy: SortField
  sortOrder: SortOrder
  minThroughput?: number
  maxThroughput?: number
  excludeOutliers: boolean
  useLogScale: boolean
}
