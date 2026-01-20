import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import type { SuiteStats, SuiteFile } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Pagination } from '@/components/shared/Pagination'
import { formatTimestamp } from '@/utils/date'

const DEFAULT_PAGE_SIZE = 20
const DEFAULT_RUNS_PER_CLIENT = 5
const RUNS_PER_CLIENT_OPTIONS = [5, 10, 15, 20] as const
const BOXES_PER_ROW = 5
const MIN_THRESHOLD = 10
const MAX_THRESHOLD = 1000
const DEFAULT_THRESHOLD = 60

// 5-level discrete color scale (green to red)
const COLORS = [
  '#22c55e', // green - fast (high MGas/s)
  '#84cc16', // lime
  '#eab308', // yellow
  '#f97316', // orange
  '#ef4444', // red - slow (low MGas/s)
]

// For per-test normalization (border color)
function getColorByNormalizedValue(value: number, min: number, max: number): string {
  if (max === min) return COLORS[2] // middle color if all same
  // Reverse: high values (fast) get green, low values (slow) get red
  const normalized = 1 - (value - min) / (max - min)
  const level = Math.min(4, Math.floor(normalized * 5))
  return COLORS[level]
}

// For global threshold-based coloring (fill color)
function getColorByThreshold(value: number, threshold: number): string {
  // Scale: threshold = yellow, >threshold = green, <threshold = red
  const ratio = value / threshold
  if (ratio >= 2) return COLORS[0] // Very fast - green
  if (ratio >= 1.5) return COLORS[1] // Fast - lime
  if (ratio >= 1) return COLORS[2] // At threshold - yellow
  if (ratio >= 0.5) return COLORS[3] // Slow - orange
  return COLORS[4] // Very slow - red
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
  maxMgas: number // Per-test max for border color normalization
  clientRuns: Record<string, RunData[]> // Most recent runs per client (up to runsPerClient)
  clientAvgMgas: Record<string, number> // Average MGas/s per client for this test
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
  const [threshold, setThreshold] = useState(DEFAULT_THRESHOLD)
  const [runsPerClient, setRunsPerClient] = useState(DEFAULT_RUNS_PER_CLIENT)

  const { allTests, clients } = useMemo(() => {
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
      const clientAvgMgas: Record<string, number> = {}
      let totalMgas = 0
      let count = 0
      let minClientMgas = Infinity
      let maxClientMgas = -Infinity

      for (const [client, runs] of Object.entries(clientRunsMap)) {
        if (runs.length === 0) continue
        // Sort by run_start descending (most recent first)
        runs.sort((a, b) => b.runStart - a.runStart)
        const recentRuns = runs.slice(0, runsPerClient)
        clientRuns[client] = recentRuns

        // Calculate stats from recent runs
        let clientTotal = 0
        for (const run of recentRuns) {
          totalMgas += run.mgas
          clientTotal += run.mgas
          count++
          minClientMgas = Math.min(minClientMgas, run.mgas)
          maxClientMgas = Math.max(maxClientMgas, run.mgas)
        }
        clientAvgMgas[client] = clientTotal / recentRuns.length
      }

      if (count === 0) continue

      const avgMgas = totalMgas / count

      processedTests.push({
        name: testName,
        testNumber: testIndexMap.get(testName),
        avgMgas,
        minMgas: minClientMgas,
        maxMgas: maxClientMgas,
        clientRuns,
        clientAvgMgas,
      })
    }

    return { allTests: processedTests, clients }
  }, [stats, testFiles, runsPerClient])

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

  const handleThresholdChange = (value: number) => {
    if (value >= MIN_THRESHOLD && value <= MAX_THRESHOLD) {
      setThreshold(value)
    }
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
    <div className="relative flex flex-col gap-4">
      {/* Controls */}
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
        {/* Threshold control */}
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Slow threshold:</span>
          <input
            type="range"
            min={MIN_THRESHOLD}
            max={MAX_THRESHOLD}
            value={threshold}
            onChange={(e) => handleThresholdChange(Number(e.target.value))}
            className="h-1.5 w-24 cursor-pointer appearance-none rounded-full bg-gray-200 accent-blue-500 dark:bg-gray-700"
          />
          <input
            type="number"
            min={MIN_THRESHOLD}
            max={MAX_THRESHOLD}
            value={threshold}
            onChange={(e) => handleThresholdChange(Number(e.target.value))}
            className="w-16 rounded-sm border border-gray-300 bg-white px-1.5 py-0.5 text-center text-xs/5 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          />
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">MGas/s</span>
          {threshold !== DEFAULT_THRESHOLD && (
            <button
              onClick={() => setThreshold(DEFAULT_THRESHOLD)}
              className="text-xs/5 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
            >
              Reset
            </button>
          )}
        </div>

        {/* Runs per client selector */}
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Runs per client:</span>
          <div className="inline-flex rounded-sm border border-gray-300 dark:border-gray-600">
            {RUNS_PER_CLIENT_OPTIONS.map((option) => (
              <button
                key={option}
                onClick={() => setRunsPerClient(option)}
                className={clsx(
                  'px-2 py-0.5 text-xs/5 transition-colors first:rounded-l-sm last:rounded-r-sm',
                  option === runsPerClient
                    ? 'bg-blue-500 text-white'
                    : 'bg-white text-gray-700 hover:bg-gray-100 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
                )}
              >
                {option}
              </button>
            ))}
          </div>
        </div>
      </div>

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
              {clients.map((client) => (
                <th key={client} className="px-1 py-2 text-center">
                  <ClientBadge client={client} />
                </th>
              ))}
              <th className="px-2 py-2 text-right">
                  <button
                    onClick={() => handleSort('avgMgas')}
                    className="inline-flex flex-col items-end font-medium text-gray-700 hover:text-gray-900 dark:text-gray-300 dark:hover:text-gray-100"
                  >
                    <span className="inline-flex items-center gap-1">
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
                    </span>
                    <span className="text-xs font-normal text-gray-500 dark:text-gray-400">MGas/s</span>
                  </button>
                </th>
            </tr>
          </thead>
          <tbody>
            {paginatedTests.map((test) => (
              <tr key={test.name} className="border-t border-gray-200 dark:border-gray-700">
                <td
                  className="sticky left-0 z-10 bg-white px-2 py-1.5 text-right font-mono text-xs/5 text-gray-500 dark:bg-gray-800 dark:text-gray-400"
                  title={test.name}
                >
                  {test.testNumber ?? '-'}
                </td>
                {clients.map((client) => {
                  const runs = test.clientRuns[client]
                  const clientAvg = test.clientAvgMgas[client]
                  const numRows = Math.ceil(runsPerClient / BOXES_PER_ROW)
                  if (!runs || runs.length === 0) {
                    return (
                      <td key={client} className="px-1 py-1.5 text-center">
                        <div className="flex flex-col items-center gap-0.5">
                          {Array.from({ length: numRows }).map((_, rowIdx) => (
                            <div key={rowIdx} className="flex justify-center gap-0.5">
                              {Array.from({ length: BOXES_PER_ROW }).map((_, i) => (
                                <div
                                  key={i}
                                  className="size-5 rounded-xs bg-gray-100 dark:bg-gray-700"
                                  title="No data"
                                />
                              ))}
                            </div>
                          ))}
                          <span className="font-mono text-xs/4 text-gray-400 dark:text-gray-500">-</span>
                        </div>
                      </td>
                    )
                  }
                  return (
                    <td key={client} className="px-1 py-1.5 text-center">
                      <div className="flex flex-col items-center gap-0.5">
                        {Array.from({ length: numRows }).map((_, rowIdx) => (
                          <div key={rowIdx} className="flex justify-center gap-0.5">
                            {Array.from({ length: BOXES_PER_ROW }).map((_, colIdx) => {
                              const i = rowIdx * BOXES_PER_ROW + colIdx
                              const run = runs[i]
                              if (!run) {
                                return (
                                  <div
                                    key={colIdx}
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
                                  className="size-5 cursor-pointer rounded-xs border-2 transition-all hover:scale-110 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500"
                                  style={{
                                    backgroundColor: getColorByThreshold(run.mgas, threshold),
                                    borderColor: getColorByNormalizedValue(run.mgas, test.minMgas, test.maxMgas),
                                  }}
                                  title={formatMGas(run.mgas)}
                                />
                              )
                            })}
                          </div>
                        ))}
                        <span className="font-mono text-xs/4 text-gray-500 dark:text-gray-400">
                          {formatMGasCompact(clientAvg)}
                        </span>
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
            <span>&gt;{threshold * 2}</span>
            <span className="flex gap-0.5">
              {COLORS.map((color, i) => (
                <span key={i} className="size-3 rounded-xs" style={{ backgroundColor: color }} />
              ))}
            </span>
            <span>&lt;{threshold / 2}</span>
            <span className="text-gray-400 dark:text-gray-500">(fill: threshold, border: per-test)</span>
          </span>
          <span>
            <span className="mr-1 inline-block size-3 rounded-xs bg-gray-100 dark:bg-gray-700" />
            No data
          </span>
          <span className="text-gray-400 dark:text-gray-500">
            {allTests.length} tests · {runsPerClient} most recent runs per client
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
