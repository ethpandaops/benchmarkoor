import type { ProcessedTestData } from '../types'
import { RadarComparisonChart } from '../charts/RadarComparisonChart'
import { TimingStackedBars } from '../charts/TimingStackedBars'
import { COMPARISON_COLORS } from '../utils/colors'

interface CompareTabProps {
  data: ProcessedTestData[]
  selectedTests: string[]
  isDark: boolean
  onRemoveTest: (testName: string) => void
  onClearSelection: () => void
}

export function CompareTab({ data, selectedTests, isDark, onRemoveTest, onClearSelection }: CompareTabProps) {
  const selectedData = selectedTests
    .map((name) => data.find((d) => d.testName === name))
    .filter((d): d is ProcessedTestData => d !== undefined)

  if (selectedData.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 py-12">
        <svg className="size-12 text-gray-300 dark:text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 17V7m0 10a2 2 0 01-2 2H5a2 2 0 01-2-2V7a2 2 0 012-2h2a2 2 0 012 2m0 10a2 2 0 002 2h2a2 2 0 002-2M9 7a2 2 0 012-2h2a2 2 0 012 2m0 10V7m0 10a2 2 0 002 2h2a2 2 0 002-2V7a2 2 0 00-2-2h-2a2 2 0 00-2 2" />
        </svg>
        <div className="text-center">
          <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">No tests selected</h4>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            Select up to 5 tests from the table below to compare their performance metrics.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Selected Tests Pills */}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-gray-500 dark:text-gray-400">Comparing:</span>
        {selectedData.map((item, index) => {
          const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
          return (
            <button
              key={item.testName}
              onClick={() => onRemoveTest(item.testName)}
              className="group flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium text-white transition-opacity hover:opacity-80"
              style={{ backgroundColor: COMPARISON_COLORS[index % COMPARISON_COLORS.length] }}
              title={item.testName}
            >
              <span>{testLabel}</span>
              <svg className="size-3.5 opacity-70 group-hover:opacity-100" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          )
        })}
        {selectedData.length > 1 && (
          <button
            onClick={onClearSelection}
            className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300"
          >
            Clear all
          </button>
        )}
      </div>

      {/* Charts Grid */}
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <h4 className="mb-2 text-sm font-medium text-gray-900 dark:text-gray-100">
            Performance Radar
          </h4>
          <p className="mb-4 text-xs text-gray-500 dark:text-gray-400">
            Normalized scores (0-100). Higher is better for all metrics.
          </p>
          <RadarComparisonChart selectedData={selectedData} isDark={isDark} />
        </div>

        <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <h4 className="mb-2 text-sm font-medium text-gray-900 dark:text-gray-100">
            Timing Breakdown
          </h4>
          <p className="mb-4 text-xs text-gray-500 dark:text-gray-400">
            Time spent in each execution phase.
          </p>
          <TimingStackedBars selectedData={selectedData} isDark={isDark} />
        </div>
      </div>

      {/* Metrics Table */}
      <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <h4 className="mb-4 text-sm font-medium text-gray-900 dark:text-gray-100">
          Detailed Metrics
        </h4>
        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead>
              <tr>
                <th className="px-3 py-2 text-left text-xs font-medium text-gray-700 dark:text-gray-300">
                  Test
                </th>
                <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                  MGas/s
                </th>
                <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                  Exec (ms)
                </th>
                <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300" title="state_read + state_hash + commit">
                  Overhead (ms)
                </th>
                <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                  Total (ms)
                </th>
                <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                  Acct Cache
                </th>
                <th className="px-3 py-2 text-right text-xs font-medium text-gray-700 dark:text-gray-300">
                  Code Cache
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
              {selectedData.map((item, index) => {
                const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
                return (
                  <tr key={item.testName}>
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-2">
                        <span
                          className="size-3 shrink-0 rounded-full"
                          style={{ backgroundColor: COMPARISON_COLORS[index % COMPARISON_COLORS.length] }}
                        />
                        <span className="text-sm text-gray-900 dark:text-gray-100" title={item.testName}>
                          {testLabel}
                        </span>
                      </div>
                    </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-900 dark:text-gray-100">
                    {item.throughput.toFixed(2)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {item.executionMs.toFixed(2)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {item.overheadMs.toFixed(2)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {item.totalMs.toFixed(2)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {item.accountCacheHitRate.toFixed(1)}%
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {item.codeCacheHitRate.toFixed(1)}%
                  </td>
                </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
