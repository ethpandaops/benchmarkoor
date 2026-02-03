import type { ProcessedTestData, DashboardStats } from '../types'
import { ThroughputBarChart } from '../charts/ThroughputBarChart'
import { ThroughputScatterChart } from '../charts/ThroughputScatterChart'

interface OverviewTabProps {
  data: ProcessedTestData[]
  stats: DashboardStats | null
  isDark: boolean
  useLogScale: boolean
  onTestClick?: (testName: string) => void
}

interface StatCardProps {
  label: string
  value: string
  subValue?: string
}

function StatCard({ label, value, subValue }: StatCardProps) {
  return (
    <div className="rounded-sm bg-gray-50 px-4 py-3 dark:bg-gray-700/50">
      <div className="text-xs text-gray-500 dark:text-gray-400">{label}</div>
      <div className="text-lg font-semibold text-gray-900 dark:text-gray-100">{value}</div>
      {subValue && <div className="text-xs text-gray-400 dark:text-gray-500">{subValue}</div>}
    </div>
  )
}

export function OverviewTab({ data, stats, isDark, useLogScale, onTestClick }: OverviewTabProps) {
  if (!stats || data.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        No data available for the current filters.
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Stats Row */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
        <StatCard label="Tests" value={stats.count.toString()} />
        <StatCard
          label="Avg Throughput"
          value={`${stats.avgThroughput.toFixed(1)} MGas/s`}
        />
        <StatCard
          label="Min Throughput"
          value={`${stats.minThroughput.toFixed(1)} MGas/s`}
        />
        <StatCard
          label="Max Throughput"
          value={`${stats.maxThroughput.toFixed(1)} MGas/s`}
        />
        <StatCard
          label="Median Throughput"
          value={`${stats.medianThroughput.toFixed(1)} MGas/s`}
        />
        <StatCard
          label="Avg Execution"
          value={`${stats.avgExecution.toFixed(2)}ms`}
        />
      </div>

      {/* Charts */}
      <div className="flex flex-col gap-6">
        <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <ThroughputBarChart data={data} isDark={isDark} useLogScale={useLogScale} onTestClick={onTestClick} />
        </div>
        <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <ThroughputScatterChart data={data} isDark={isDark} useLogScale={useLogScale} onTestClick={onTestClick} />
        </div>
      </div>
    </div>
  )
}
