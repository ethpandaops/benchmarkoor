import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import type { SuiteStats, SuiteFile } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Pagination } from '@/components/shared/Pagination'
import { formatTimestamp } from '@/utils/date'

const DEFAULT_PAGE_SIZE = 20
const RUNS_PER_CLIENT = 4

// 5-level discrete color scale (green to red)
const COLORS = [
  '#22c55e', // green - fast (high MGas/s)
  '#84cc16', // lime
  '#eab308', // yellow
  '#f97316', // orange
  '#ef4444', // red - slow (low MGas/s)
]

// For MGas/s, higher is better, so we reverse the color scale
function getColorByNormalizedValue(value: number, min: number, max: number): string {
  if (max === min) return COLORS[2] // middle color if all same
  // Reverse: high values (fast) get green, low values (slow) get red
  const normalized = 1 - (value - min) / (max - min)
  const level = Math.min(4, Math.floor(normalized * 5))
  return COLORS[level]
}

function calculateMGasPerSec(gasUsed: number, timeNs: number): number | undefined {
  if (timeNs <= 0 || gasUsed <= 0) return undefined
  // MGas/s = (gas / 1_000_000) / (time_ns / 1_000_000_000)
  //        = (gas * 1000) / time_ns
  return (gasUsed * 1000) / timeNs
}

function formatMGas(mgas: number): string {
  return `${mgas.toFixed(2)} MGas/s`
}

function formatMGasCompact(mgas: number): string {
  return mgas.toFixed(1)
}

function truncateTestName(name: string, maxLength: number = 40): string {
  if (name.length <= maxLength) return name
  return '...' + name.slice(-(maxLength - 3))
}

interface RunData {
  runId: string
  mgas: number
  runStart: number
}

interface ProcessedTest {
  name: string
  testNumber: number | undefined // 1-based index in suite's test list
  avgMgas: number
  minMgas: number
  clientRuns: Record<string, RunData[]> // Most recent runs per client (up to RUNS_PER_CLIENT)
}

interface TooltipData {
  testName: string
  client: string
  run: RunData
  x: number
  y: number
}

interface TestHeatmapProps {
  stats: SuiteStats
  testFiles?: SuiteFile[]
  isDark: boolean
  pageSize?: number
}

type SortDirection = 'asc' | 'desc'
type SortField = 'testNumber' | 'avgMgas'

export function TestHeatmap({ stats, testFiles, isDark, pageSize = DEFAULT_PAGE_SIZE }: TestHeatmapProps) {
  const navigate = useNavigate()
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)
  const [currentPage, setCurrentPage] = useState(1)
  const [sortField, setSortField] = useState<SortField>('avgMgas')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  const { allTests, clients, minMgas, maxMgas } = useMemo(() => {
    // Build lookup map from test path to 1-based index
    const testIndexMap = new Map<string, number>()
    if (testFiles) {
      testFiles.forEach((file, index) => {
        const path = file.d ? `${file.d}/${file.f}` : file.f
        testIndexMap.set(path, index + 1)
      })
    }

    // Extract unique clients from all durations
    const clientSet = new Set<string>()
    for (const testDurations of Object.values(stats)) {
      for (const duration of testDurations.durations) {
        clientSet.add(duration.client)
      }
    }
    const clients = Array.from(clientSet).sort()

    // Process each test
    const processedTests: ProcessedTest[] = []
    for (const [testName, testDurations] of Object.entries(stats)) {
      // Group durations by client
      const clientRunsMap: Record<string, RunData[]> = {}
      for (const duration of testDurations.durations) {
        const mgas = calculateMGasPerSec(duration.gas_used, duration.time_ns)
        if (mgas === undefined) continue

        if (!clientRunsMap[duration.client]) {
          clientRunsMap[duration.client] = []
        }
        clientRunsMap[duration.client].push({
          runId: duration.id,
          mgas,
          runStart: duration.run_start,
        })
      }

      // Sort by run_start (most recent first) and take top N for each client
      const clientRuns: Record<string, RunData[]> = {}
      let totalMgas = 0
      let count = 0
      let minClientMgas = Infinity

      for (const [client, runs] of Object.entries(clientRunsMap)) {
        if (runs.length === 0) continue
        // Sort by run_start descending (most recent first)
        runs.sort((a, b) => b.runStart - a.runStart)
        const recentRuns = runs.slice(0, RUNS_PER_CLIENT)
        clientRuns[client] = recentRuns

        // Calculate stats from recent runs
        for (const run of recentRuns) {
          totalMgas += run.mgas
          count++
          minClientMgas = Math.min(minClientMgas, run.mgas)
        }
      }

      if (count === 0) continue

      const avgMgas = totalMgas / count

      processedTests.push({
        name: testName,
        testNumber: testIndexMap.get(testName),
        avgMgas,
        minMgas: minClientMgas,
        clientRuns,
      })
    }

    // Calculate min/max for color scaling across ALL tests for consistent colors
    let minMgas = Infinity
    let maxMgas = -Infinity
    for (const test of processedTests) {
      for (const runs of Object.values(test.clientRuns)) {
        for (const run of runs) {
          minMgas = Math.min(minMgas, run.mgas)
          maxMgas = Math.max(maxMgas, run.mgas)
        }
      }
    }

    if (minMgas === Infinity) minMgas = 0
    if (maxMgas === -Infinity) maxMgas = 0

    return { allTests: processedTests, clients, minMgas, maxMgas }
  }, [stats, testFiles])

  // Sort and paginate
  const sortedTests = useMemo(() => {
    const sorted = [...allTests]
    sorted.sort((a, b) => {
      if (sortField === 'testNumber') {
        // Tests without a number go to the end
        const aNum = a.testNumber ?? Infinity
        const bNum = b.testNumber ?? Infinity
        return sortDirection === 'asc' ? aNum - bNum : bNum - aNum
      }
      // Sort by avgMgas
      if (sortDirection === 'asc') {
        return a.avgMgas - b.avgMgas // Lowest first (slowest)
      }
      return b.avgMgas - a.avgMgas // Highest first (fastest)
    })
    return sorted
  }, [allTests, sortField, sortDirection])

  const totalPages = Math.ceil(sortedTests.length / pageSize)
  const paginatedTests = sortedTests.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  const handlePageChange = (page: number) => {
    setCurrentPage(page)
  }

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection((prev) => (prev === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(field)
      setSortDirection('asc')
    }
    setCurrentPage(1) // Reset to first page when sorting changes
  }

  const handleCellClick = (runId: string) => {
    navigate({
      to: '/runs/$runId',
      params: { runId },
    })
  }

  const handleMouseEnter = (test: ProcessedTest, client: string, run: RunData, event: React.MouseEvent) => {
    const rect = event.currentTarget.getBoundingClientRect()
    setTooltip({
      testName: test.name,
      client,
      run,
      x: rect.left + rect.width / 2,
      y: rect.top,
    })
  }

  const handleMouseLeave = () => {
    setTooltip(null)
  }

  if (allTests.length === 0) {
    return (
      <p className="py-4 text-center text-sm/6 text-gray-500 dark:text-gray-400">
        No test performance data available.
      </p>
    )
  }

  return (
    <div className="relative">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm/6">
          <thead>
            <tr>
              <th className="sticky left-0 z-10 bg-white px-2 py-2 text-right dark:bg-gray-800">
                <button
                  onClick={() => handleSort('testNumber')}
                  className="inline-flex items-center gap-1 font-medium text-gray-700 hover:text-gray-900 dark:text-gray-300 dark:hover:text-gray-100"
                >
                  #
                  {sortField === 'testNumber' && (
                    <svg
                      className={clsx('size-4 transition-transform', sortDirection === 'desc' && 'rotate-180')}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
                    </svg>
                  )}
                </button>
              </th>
              <th className="px-2 py-2 text-left font-medium text-gray-700 dark:bg-gray-800 dark:text-gray-300">
                Test
              </th>
              {clients.map((client) => (
                <th key={client} className="px-1 py-2 text-center">
                  <ClientBadge client={client} />
                </th>
              ))}
              <th className="px-2 py-2 text-right">
                  <button
                    onClick={() => handleSort('avgMgas')}
                    className="inline-flex items-center gap-1 font-medium text-gray-700 hover:text-gray-900 dark:text-gray-300 dark:hover:text-gray-100"
                  >
                    Avg
                    {sortField === 'avgMgas' && (
                      <svg
                        className={clsx('size-4 transition-transform', sortDirection === 'desc' && 'rotate-180')}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
                      </svg>
                    )}
                  </button>
                </th>
            </tr>
          </thead>
          <tbody>
            {paginatedTests.map((test) => (
              <tr key={test.name} className="border-t border-gray-200 dark:border-gray-700">
                <td className="sticky left-0 z-10 bg-white px-2 py-1.5 text-right font-mono text-xs/5 text-gray-500 dark:bg-gray-800 dark:text-gray-400">
                  {test.testNumber ?? '-'}
                </td>
                <td
                  className="max-w-xs truncate px-2 py-1.5 font-mono text-xs/5 text-gray-900 dark:text-gray-100"
                  title={test.name}
                >
                  {truncateTestName(test.name)}
                </td>
                {clients.map((client) => {
                  const runs = test.clientRuns[client]
                  if (!runs || runs.length === 0) {
                    return (
                      <td key={client} className="px-1 py-1.5 text-center">
                        <div className="mx-auto flex justify-center gap-0.5">
                          {Array.from({ length: RUNS_PER_CLIENT }).map((_, i) => (
                            <div
                              key={i}
                              className="size-5 rounded-xs bg-gray-100 dark:bg-gray-700"
                              title="No data"
                            />
                          ))}
                        </div>
                      </td>
                    )
                  }
                  return (
                    <td key={client} className="px-1 py-1.5 text-center">
                      <div className="mx-auto flex justify-center gap-0.5">
                        {/* Pad with empty slots if fewer than RUNS_PER_CLIENT */}
                        {Array.from({ length: RUNS_PER_CLIENT }).map((_, i) => {
                          const run = runs[i]
                          if (!run) {
                            return (
                              <div
                                key={i}
                                className="size-5 rounded-xs bg-gray-100 dark:bg-gray-700"
                                title="No data"
                              />
                            )
                          }
                          return (
                            <button
                              key={run.runId}
                              onClick={() => handleCellClick(run.runId)}
                              onMouseEnter={(e) => handleMouseEnter(test, client, run, e)}
                              onMouseLeave={handleMouseLeave}
                              className="size-5 cursor-pointer rounded-xs transition-all hover:scale-110 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500"
                              style={{ backgroundColor: getColorByNormalizedValue(run.mgas, minMgas, maxMgas) }}
                              title={formatMGas(run.mgas)}
                            />
                          )
                        })}
                      </div>
                    </td>
                  )
                })}
                <td className="px-2 py-1.5 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400">
                  {formatMGasCompact(test.avgMgas)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Legend and pagination */}
      <div className="mt-4 flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs/5 text-gray-500 dark:text-gray-400">
          <span className="flex items-center gap-1">
            <span>Fast</span>
            <span className="flex gap-0.5">
              {COLORS.map((color, i) => (
                <span key={i} className="size-3 rounded-xs" style={{ backgroundColor: color }} />
              ))}
            </span>
            <span>Slow</span>
          </span>
          <span>
            <span className="mr-1 inline-block size-3 rounded-xs bg-gray-100 dark:bg-gray-700" />
            No data
          </span>
          <span className="text-gray-400 dark:text-gray-500">
            {allTests.length} tests · {RUNS_PER_CLIENT} most recent runs per client
          </span>
        </div>
        <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={handlePageChange} />
      </div>

      {/* Tooltip */}
      {tooltip && (
        <div
          className={clsx(
            'pointer-events-none fixed z-50 rounded-sm px-3 py-2 text-xs/5 shadow-lg',
            isDark ? 'bg-gray-800 text-gray-100' : 'bg-white text-gray-900 ring-1 ring-gray-200',
          )}
          style={{
            left: tooltip.x,
            top: tooltip.y - 8,
            transform: 'translate(-50%, -100%)',
          }}
        >
          <div className="flex flex-col gap-1">
            <div className="font-medium">{tooltip.client}</div>
            <div className="max-w-xs truncate font-mono text-gray-500 dark:text-gray-400">{tooltip.testName}</div>
            <div>{formatMGas(tooltip.run.mgas)}</div>
            <div className="text-gray-400 dark:text-gray-500">{formatTimestamp(tooltip.run.runStart)}</div>
            <div className="mt-1 text-gray-400 dark:text-gray-500">Click for details</div>
          </div>
        </div>
      )}
    </div>
  )
}
