import { useMemo } from 'react'
import type { ProcessedTestData, DashboardStats, TestCategory } from '../types'
import { BoxPlotChart } from '../charts/BoxPlotChart'
import { HistogramChart } from '../charts/HistogramChart'
import { ALL_CATEGORIES, CATEGORY_COLORS } from '../utils/colors'
import { percentile } from '../utils/statistics'

interface DistributionTabProps {
  data: ProcessedTestData[]
  stats: DashboardStats | null
  isDark: boolean
  useLogScale: boolean
}

interface PercentileCardProps {
  label: string
  value: string
}

function PercentileCard({ label, value }: PercentileCardProps) {
  return (
    <div className="rounded-sm bg-gray-50 px-3 py-2 dark:bg-gray-700/50">
      <div className="text-xs text-gray-500 dark:text-gray-400">{label}</div>
      <div className="font-mono text-sm font-semibold text-gray-900 dark:text-gray-100">{value}</div>
    </div>
  )
}

export function DistributionTab({ data, stats, isDark, useLogScale }: DistributionTabProps) {
  const activeCategories = useMemo<TestCategory[]>(() =>
    ALL_CATEGORIES.filter(cat => (stats?.categoryBreakdown[cat] ?? 0) > 0),
    [stats]
  )

  const percentiles = useMemo(() => {
    if (data.length === 0) return null

    const sorted = [...data.map((d) => d.throughput)].sort((a, b) => a - b)
    return {
      p5: percentile(sorted, 5),
      p10: percentile(sorted, 10),
      p25: percentile(sorted, 25),
      p50: percentile(sorted, 50),
      p75: percentile(sorted, 75),
      p90: percentile(sorted, 90),
      p95: percentile(sorted, 95),
      p99: percentile(sorted, 99),
    }
  }, [data])

  const categoryStats = useMemo(() => {
    return ALL_CATEGORIES.map((category) => {
      const categoryData = data.filter((d) => d.category === category)
      if (categoryData.length === 0) return null

      const throughputs = categoryData.map((d) => d.throughput).sort((a, b) => a - b)
      return {
        category,
        count: categoryData.length,
        min: throughputs[0],
        max: throughputs[throughputs.length - 1],
        median: percentile(throughputs, 50),
        avg: throughputs.reduce((a, b) => a + b, 0) / throughputs.length,
      }
    }).filter((s): s is NonNullable<typeof s> => s !== null)
  }, [data])

  if (data.length === 0 || !stats || !percentiles) {
    return (
      <div className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        No data available for the current filters.
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Percentiles Row */}
      <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <h4 className="mb-3 text-sm font-medium text-gray-900 dark:text-gray-100">
          Throughput Percentiles (MGas/s)
        </h4>
        <div className="grid grid-cols-4 gap-3 sm:grid-cols-8">
          <PercentileCard label="P5" value={percentiles.p5.toFixed(1)} />
          <PercentileCard label="P10" value={percentiles.p10.toFixed(1)} />
          <PercentileCard label="P25" value={percentiles.p25.toFixed(1)} />
          <PercentileCard label="P50 (Median)" value={percentiles.p50.toFixed(1)} />
          <PercentileCard label="P75" value={percentiles.p75.toFixed(1)} />
          <PercentileCard label="P90" value={percentiles.p90.toFixed(1)} />
          <PercentileCard label="P95" value={percentiles.p95.toFixed(1)} />
          <PercentileCard label="P99" value={percentiles.p99.toFixed(1)} />
        </div>
      </div>

      {/* Category Summary */}
      {categoryStats.length > 0 && (
        <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <h4 className="mb-3 text-sm font-medium text-gray-900 dark:text-gray-100">
            Category Summary
          </h4>
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
              <thead>
                <tr>
                  <th className="px-3 py-2 text-left text-xs font-medium text-gray-700 dark:text-gray-300">
                    Category
                  </th>
                  <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                    Count
                  </th>
                  <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                    Min
                  </th>
                  <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                    Median
                  </th>
                  <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                    Avg
                  </th>
                  <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                    Max
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                {categoryStats.map((cat) => (
                  <tr key={cat.category}>
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-2">
                        <span
                          className="size-3 rounded-full"
                          style={{ backgroundColor: CATEGORY_COLORS[cat.category] }}
                        />
                        <span className="text-sm capitalize text-gray-900 dark:text-gray-100">
                          {cat.category}
                        </span>
                      </div>
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                      {cat.count}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                      {cat.min.toFixed(1)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-sm text-gray-900 dark:text-gray-100">
                      {cat.median.toFixed(1)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                      {cat.avg.toFixed(1)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                      {cat.max.toFixed(1)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Charts */}
      <div className="flex flex-col gap-6">
        <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <BoxPlotChart data={data} isDark={isDark} useLogScale={useLogScale} />
        </div>
        <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <HistogramChart data={data} isDark={isDark} useLogScale={useLogScale} activeCategories={activeCategories} />
        </div>
      </div>
    </div>
  )
}
