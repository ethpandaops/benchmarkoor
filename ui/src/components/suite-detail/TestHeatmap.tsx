import { Fragment, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { ChevronUp } from 'lucide-react'
import { type SuiteStats, type SuiteTest, type IndexStepType, ALL_INDEX_STEP_TYPES, getRunDurationAggregatedStats } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Pagination } from '@/components/shared/Pagination'
import { formatTimestamp } from '@/utils/date'

const DEFAULT_PAGE_SIZE = 20
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const
const DEFAULT_RUNS_PER_CLIENT = 5
const RUNS_PER_CLIENT_OPTIONS = [5, 10, 15, 20, 25] as const
const BOXES_PER_ROW = 5
const STAT_DISPLAY_OPTIONS = ['Avg', 'Min', 'Max', 'Last'] as const
type StatDisplayType = (typeof STAT_DISPLAY_OPTIONS)[number]
const DISTRIBUTION_STAT_OPTIONS = ['Avg', 'Min'] as const
type DistributionStatType = (typeof DISTRIBUTION_STAT_OPTIONS)[number]
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

function splitByMatch(name: string, search: string, isRegex: boolean): { text: string; highlight: boolean }[] {
  if (!search) return [{ text: name, highlight: false }]
  try {
    const pattern = isRegex ? search : search.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const re = new RegExp(`(${pattern})`, 'gi')
    const parts = name.split(re)
    if (parts.length === 1) return [{ text: name, highlight: false }]
    return parts.filter(Boolean).map((part) => ({ text: part, highlight: re.test(part) }))
  } catch {
    return [{ text: name, highlight: false }]
  }
}

function HighlightedName({ name, search, useRegex }: { name: string; search: string; useRegex: boolean }) {
  const parts = splitByMatch(name, search, useRegex)
  return (
    <>
      {parts.map((part, i) =>
        part.highlight ? (
          <mark key={i} className="rounded-xs bg-yellow-200 text-yellow-900 dark:bg-yellow-700/50 dark:text-yellow-200">
            {part.text}
          </mark>
        ) : (
          <span key={i}>{part.text}</span>
        ),
      )}
    </>
  )
}

interface RunData {
  runId: string
  mgas: number
  runStart: number
}

interface ClientStats {
  avg: number
  min: number
  max: number
  last: number
}

interface ProcessedTest {
  name: string
  testNumber: number | undefined // 1-based index in suite's test list
  avgMgas: number
  minMgas: number
  maxMgas: number // Per-test max for border color normalization
  lastMgas: number // Most recent run across all clients
  clientRuns: Record<string, RunData[]> // Most recent runs per client (up to runsPerClient)
  clientStats: Record<string, ClientStats> // Stats per client for this test
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
  testFiles?: SuiteTest[]
  isDark: boolean
  stepFilter?: IndexStepType[]
  searchQuery?: string
  onSearchChange?: (query: string | undefined) => void
  showTestName?: boolean
  onShowTestNameChange?: (show: boolean) => void
}

type SortDirection = 'asc' | 'desc'
type SortField = 'testNumber' | 'avgMgas'

export function TestHeatmap({ stats, testFiles, isDark, stepFilter = ALL_INDEX_STEP_TYPES, searchQuery, onSearchChange, showTestName: showTestNameProp, onShowTestNameChange }: TestHeatmapProps) {
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [sortField, setSortField] = useState<SortField>('avgMgas')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')
  const [threshold, setThreshold] = useState(DEFAULT_THRESHOLD)
  const [runsPerClient, setRunsPerClient] = useState(DEFAULT_RUNS_PER_CLIENT)
  const [statDisplay, setStatDisplay] = useState<StatDisplayType>('Avg')
  const [showClientStat, setShowClientStat] = useState(true)
  const showTestName = showTestNameProp ?? false
  const [useRegex, setUseRegex] = useState(false)
  const [statColumnType, setStatColumnType] = useState<DistributionStatType>('Avg')

  const { allTests, clients } = useMemo(() => {
    // Build lookup map from test name to 1-based index
    const testIndexMap = new Map<string, number>()
    if (testFiles) {
      testFiles.forEach((test, index) => {
        testIndexMap.set(test.name, index + 1)
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
        const { gasUsed, timeNs } = getRunDurationAggregatedStats(duration, stepFilter)
        const mgas = calculateMGasPerSec(gasUsed, timeNs)
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
      const clientStats: Record<string, ClientStats> = {}
      let totalMgas = 0
      let count = 0
      let minClientMgas = Infinity
      let maxClientMgas = -Infinity
      let lastRunStart = -Infinity
      let lastMgas = 0

      for (const [client, runs] of Object.entries(clientRunsMap)) {
        if (runs.length === 0) continue
        // Sort by run_start descending (most recent first)
        runs.sort((a, b) => b.runStart - a.runStart)
        const recentRuns = runs.slice(0, runsPerClient)
        clientRuns[client] = recentRuns

        // Track the most recent run across all clients
        if (recentRuns[0].runStart > lastRunStart) {
          lastRunStart = recentRuns[0].runStart
          lastMgas = recentRuns[0].mgas
        }

        // Calculate stats from recent runs
        let clientTotal = 0
        let clientMin = Infinity
        let clientMax = -Infinity
        for (const run of recentRuns) {
          totalMgas += run.mgas
          clientTotal += run.mgas
          count++
          minClientMgas = Math.min(minClientMgas, run.mgas)
          maxClientMgas = Math.max(maxClientMgas, run.mgas)
          clientMin = Math.min(clientMin, run.mgas)
          clientMax = Math.max(clientMax, run.mgas)
        }
        clientStats[client] = {
          avg: clientTotal / recentRuns.length,
          min: clientMin,
          max: clientMax,
          last: recentRuns[0].mgas, // Most recent run for this client
        }
      }

      if (count === 0) continue

      const avgMgas = totalMgas / count

      processedTests.push({
        name: testName,
        testNumber: testIndexMap.get(testName),
        avgMgas,
        minMgas: minClientMgas,
        maxMgas: maxClientMgas,
        lastMgas,
        clientRuns,
        clientStats,
      })
    }

    return { allTests: processedTests, clients }
  }, [stats, testFiles, runsPerClient, stepFilter])

  const search = searchQuery ?? ''

  // Filter by search query
  const filteredTests = useMemo(() => {
    if (!search) return allTests
    if (useRegex) {
      try {
        const re = new RegExp(search, 'i')
        return allTests.filter((t) => re.test(t.name))
      } catch {
        return allTests
      }
    }
    const lower = search.toLowerCase()
    return allTests.filter((t) => t.name.toLowerCase().includes(lower))
  }, [allTests, search, useRegex])

  // Sort and paginate
  const sortedTests = useMemo(() => {
    const sorted = [...filteredTests]
    sorted.sort((a, b) => {
      if (sortField === 'testNumber') {
        // Tests without a number go to the end
        const aNum = a.testNumber ?? Infinity
        const bNum = b.testNumber ?? Infinity
        return sortDirection === 'asc' ? aNum - bNum : bNum - aNum
      }
      // Sort by selected stat column type
      const aVal = statColumnType === 'Avg' ? a.avgMgas : a.minMgas
      const bVal = statColumnType === 'Avg' ? b.avgMgas : b.minMgas
      if (sortDirection === 'asc') {
        return aVal - bVal // Lowest first (slowest)
      }
      return bVal - aVal // Highest first (fastest)
    })
    return sorted
  }, [filteredTests, sortField, sortDirection, statColumnType])

  const totalPages = Math.ceil(sortedTests.length / pageSize)
  const paginatedTests = sortedTests.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  // Calculate histogram data for distribution graph
  const histogramData = useMemo(() => {
    if (allTests.length === 0) return []

    // Create bins based on threshold: 0, 0.25x, 0.5x, 0.75x, 1x, 1.25x, 1.5x, 1.75x, 2x, 2.5x, 3x+
    const binMultipliers = [0, 0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.5, 3]
    const bins = Array(binMultipliers.length).fill(0) as number[]

    for (const test of allTests) {
      const statValue = statColumnType === 'Avg' ? test.avgMgas : test.minMgas
      const ratio = statValue / threshold
      let binIndex = binMultipliers.findIndex((_, i) => {
        const next = binMultipliers[i + 1]
        return next === undefined ? true : ratio < next
      })
      if (binIndex === -1) binIndex = binMultipliers.length - 1
      bins[binIndex]++
    }

    const maxCount = Math.max(...bins)
    const logMax = Math.log10(maxCount + 1)
    return bins.map((count, i) => {
      const rangeStart = binMultipliers[i] * threshold
      const rangeEnd = binMultipliers[i + 1] !== undefined ? binMultipliers[i + 1] * threshold : Infinity
      const midpoint =
        binMultipliers[i + 1] !== undefined
          ? ((binMultipliers[i] + binMultipliers[i + 1]) / 2) * threshold
          : binMultipliers[i] * 1.25 * threshold
      return {
        count,
        height: maxCount > 0 ? (Math.log10(count + 1) / logMax) * 100 : 0,
        rangeStart,
        rangeEnd,
        label: binMultipliers[i + 1] !== undefined ? `${rangeStart.toFixed(0)}-${rangeEnd.toFixed(0)}` : `${rangeStart.toFixed(0)}+`,
        color: getColorByThreshold(midpoint, threshold),
      }
    })
  }, [allTests, threshold, statColumnType])

  const slowCount = allTests.filter((t) => (statColumnType === 'Avg' ? t.avgMgas : t.minMgas) < threshold).length
  const fastCount = allTests.filter((t) => (statColumnType === 'Avg' ? t.avgMgas : t.minMgas) >= threshold).length

  const handlePageChange = (page: number) => {
    setCurrentPage(page)
  }

  const handlePageSizeChange = (size: number) => {
    setPageSize(size)
    setCurrentPage(1) // Reset to first page when page size changes
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

  const handleSearchChange = (value: string) => {
    setCurrentPage(1)
    onSearchChange?.(value || undefined)
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
      <div className="flex items-start gap-x-6 gap-y-2">
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

        {/* Stat display selector */}
        <div className="flex items-center gap-2">
          <label className="flex cursor-pointer items-center gap-1.5">
            <input
              type="checkbox"
              checked={showClientStat}
              onChange={(e) => setShowClientStat(e.target.checked)}
              className="size-3.5 cursor-pointer rounded-xs border-gray-300 text-blue-500 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
            />
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Client stat:</span>
          </label>
          <div className="inline-flex rounded-sm border border-gray-300 dark:border-gray-600">
            {STAT_DISPLAY_OPTIONS.map((option) => (
              <button
                key={option}
                onClick={() => setStatDisplay(option)}
                disabled={!showClientStat}
                className={clsx(
                  'px-2 py-0.5 text-xs/5 transition-colors first:rounded-l-sm last:rounded-r-sm',
                  !showClientStat && 'opacity-50',
                  option === statDisplay
                    ? 'bg-blue-500 text-white'
                    : 'bg-white text-gray-700 hover:bg-gray-100 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
                )}
              >
                {option}
              </button>
            ))}
          </div>
        </div>

        {/* Show test name toggle */}
        <label className="flex cursor-pointer items-center gap-1.5">
          <input
            type="checkbox"
            checked={showTestName}
            onChange={(e) => onShowTestNameChange?.(e.target.checked)}
            className="size-3.5 cursor-pointer rounded-xs border-gray-300 text-blue-500 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
          />
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Test name</span>
        </label>

        {/* Page size selector */}
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Per page:</span>
          <div className="inline-flex rounded-sm border border-gray-300 dark:border-gray-600">
            {PAGE_SIZE_OPTIONS.map((option) => (
              <button
                key={option}
                onClick={() => handlePageSizeChange(option)}
                className={clsx(
                  'px-2 py-0.5 text-xs/5 transition-colors first:rounded-l-sm last:rounded-r-sm',
                  option === pageSize
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

        {/* Search filter */}
        <div className="flex shrink-0 items-center gap-1.5">
          <input
            type="text"
            value={search}
            onChange={(e) => handleSearchChange(e.target.value)}
            placeholder={useRegex ? 'Regex pattern...' : 'Filter tests...'}
            className={clsx(
              'w-48 rounded-sm border bg-white px-2 py-0.5 text-xs/5 text-gray-900 placeholder:text-gray-400 focus:outline-hidden focus:ring-1 dark:bg-gray-800 dark:text-gray-100 dark:placeholder:text-gray-500',
              useRegex && search && (() => { try { new RegExp(search); return false } catch { return true } })()
                ? 'border-red-400 focus:border-red-500 focus:ring-red-500 dark:border-red-500'
                : 'border-gray-300 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-600',
            )}
          />
          <button
            onClick={() => setUseRegex(!useRegex)}
            title={useRegex ? 'Regex mode (click to switch to text)' : 'Text mode (click to switch to regex)'}
            className={clsx(
              'rounded-sm px-1.5 py-0.5 font-mono text-xs/5 transition-colors',
              useRegex
                ? 'bg-blue-500 text-white'
                : 'bg-white text-gray-500 ring-1 ring-gray-300 hover:bg-gray-100 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700',
            )}
          >
            .*
          </button>
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
                    <ChevronUp className={clsx('size-4 transition-transform', sortDirection === 'desc' && 'rotate-180')} />
                  )}
                </button>
              </th>
              {clients.map((client) => (
                <th key={client} className="px-1 py-2 text-center">
                  <ClientBadge client={client} />
                </th>
              ))}
              <th className="px-2 py-2 text-right">
                <div className="flex flex-col items-end gap-1">
                  <div className="inline-flex rounded-sm border border-gray-300 dark:border-gray-600">
                    {DISTRIBUTION_STAT_OPTIONS.map((option) => (
                      <button
                        key={option}
                        onClick={() => setStatColumnType(option)}
                        className={clsx(
                          'px-1.5 py-0.5 text-xs/4 transition-colors first:rounded-l-sm last:rounded-r-sm',
                          option === statColumnType
                            ? 'bg-blue-500 text-white'
                            : 'bg-white text-gray-700 hover:bg-gray-100 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
                        )}
                      >
                        {option}
                      </button>
                    ))}
                  </div>
                  <button
                    onClick={() => handleSort('avgMgas')}
                    className="inline-flex items-center gap-1 text-xs font-normal text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                  >
                    MGas/s
                    {sortField === 'avgMgas' && (
                      <ChevronUp className={clsx('size-3 transition-transform', sortDirection === 'desc' && 'rotate-180')} />
                    )}
                  </button>
                </div>
              </th>
            </tr>
          </thead>
          <tbody>
            {paginatedTests.map((test) => (
              <Fragment key={test.name}>
              {showTestName && (
                <tr className="border-t border-gray-200 dark:border-gray-700">
                  <td
                    colSpan={clients.length + 2}
                    className="truncate px-2 py-0.5 font-mono text-xs/5 text-gray-500 dark:text-gray-400"
                    title={test.name}
                  >
                    <HighlightedName name={test.name} search={search} useRegex={useRegex} />
                  </td>
                </tr>
              )}
              <tr className={clsx('border-t border-gray-200 dark:border-gray-700', showTestName && 'border-t-0')}>
                <td
                  className="sticky left-0 z-10 bg-white px-2 py-1.5 text-right font-mono text-xs/5 text-gray-500 dark:bg-gray-800 dark:text-gray-400"
                  title={test.name}
                >
                  {test.testNumber ?? '-'}
                </td>
                {clients.map((client) => {
                  const runs = test.clientRuns[client]
                  const stats = test.clientStats[client]
                  const numRows = Math.ceil(runsPerClient / BOXES_PER_ROW)
                  const displayValue = stats
                    ? statDisplay === 'Avg'
                      ? stats.avg
                      : statDisplay === 'Min'
                        ? stats.min
                        : statDisplay === 'Max'
                          ? stats.max
                          : stats.last
                    : undefined
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
                          {showClientStat && (
                            <span className="font-mono text-xs/4 text-gray-400 dark:text-gray-500">-</span>
                          )}
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
                                <Link
                                  key={run.runId}
                                  to="/runs/$runId"
                                  params={{ runId: run.runId }}
                                  search={{ testModal: test.name }}
                                  onMouseEnter={(e) => handleMouseEnter(test, client, run, e)}
                                  onMouseLeave={handleMouseLeave}
                                  className="block size-5 rounded-xs border-2 transition-all hover:scale-110 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500"
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
                        {showClientStat && (
                          <span className="font-mono text-xs/4 text-gray-500 dark:text-gray-400">
                            {displayValue !== undefined ? formatMGasCompact(displayValue) : '-'}
                          </span>
                        )}
                      </div>
                    </td>
                  )
                })}
                <td className="px-2 py-1.5 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400">
                  {formatMGasCompact(statColumnType === 'Avg' ? test.avgMgas : test.minMgas)}
                </td>
              </tr>
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>

      {/* Distribution Histogram */}
      {histogramData.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Distribution by threshold</span>
          <div className="flex items-end gap-1">
            <div className="flex h-16 w-8 shrink-0 flex-col items-center justify-end">
              <span className="text-xs/5 font-medium text-red-600 dark:text-red-400">{slowCount}</span>
              <span className="text-xs/5 text-gray-400 dark:text-gray-500">slow</span>
            </div>
            <div className="relative flex h-16 flex-1 items-end gap-1">
              {histogramData.map((bin, i) => (
                <div
                  key={i}
                  className="flex-1 rounded-t-xs transition-all hover:opacity-80"
                  style={{
                    height: `${bin.height}%`,
                    backgroundColor: bin.color,
                    minHeight: bin.count > 0 ? '2px' : '0',
                  }}
                  title={`${bin.label} MGas/s: ${bin.count} tests`}
                />
              ))}
              {/* Threshold line - positioned at bin index 4 (1x threshold) */}
              <div
                className="absolute bottom-0 top-0 w-0.5 bg-black dark:bg-white"
                style={{ left: `${(4 / 11) * 100}%` }}
                title={`Threshold: ${threshold} MGas/s`}
              />
            </div>
            <div className="flex h-16 w-8 shrink-0 flex-col items-center justify-end">
              <span className="text-xs/5 font-medium text-green-600 dark:text-green-400">{fastCount}</span>
              <span className="text-xs/5 text-gray-400 dark:text-gray-500">fast</span>
            </div>
          </div>
          <div className="flex justify-between px-9 text-xs/5 text-gray-400 dark:text-gray-500">
            <span>0</span>
            <span className="font-medium text-yellow-600 dark:text-yellow-400">{threshold} MGas/s (threshold)</span>
            <span>{threshold * 3}+</span>
          </div>
        </div>
      )}

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
            {search ? `${filteredTests.length} / ${allTests.length}` : allTests.length} tests · {runsPerClient} most recent runs per client
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
          <div className="flex max-w-md flex-col gap-1">
            <div className="font-medium">{tooltip.client}</div>
            <div className="break-all font-mono text-gray-500 dark:text-gray-400">{tooltip.testName}</div>
            <div>{formatMGas(tooltip.run.mgas)}</div>
            <div className="text-gray-400 dark:text-gray-500">{formatTimestamp(tooltip.run.runStart)}</div>
            <div className="mt-1 text-gray-400 dark:text-gray-500">Click for details</div>
          </div>
        </div>
      )}
    </div>
  )
}
