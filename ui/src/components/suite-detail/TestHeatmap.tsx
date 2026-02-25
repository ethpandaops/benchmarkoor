import { Fragment, useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { ChevronUp, Flame, Maximize2, X } from 'lucide-react'
import { type SuiteStats, type SuiteTest, type IndexStepType, ALL_INDEX_STEP_TYPES, getRunDurationAggregatedStats } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { JDenticon } from '@/components/shared/JDenticon'
import { Pagination } from '@/components/shared/Pagination'
import { Spinner } from '@/components/shared/Spinner'
import { formatTimestamp } from '@/utils/date'

const DEFAULT_PAGE_SIZE = 20
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const
const DEFAULT_RUNS_PER_CLIENT = 5
const RUNS_PER_CLIENT_OPTIONS = [5, 10, 15, 20, 25] as const
const BOXES_PER_ROW = 5
const STAT_COLUMNS = ['avgMgas', 'minMgas', 'p99Mgas'] as const
const STAT_COLUMN_LABELS: Record<(typeof STAT_COLUMNS)[number], string> = { avgMgas: 'Avg', minMgas: 'Min', p99Mgas: 'P99' }
const STAT_TO_CLIENT_FIELD: Record<(typeof STAT_COLUMNS)[number], keyof ClientStats> = { avgMgas: 'avg', minMgas: 'min', p99Mgas: 'p99' }
const BIN_MULTIPLIERS = [0, 0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.5, 3]
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
  return Math.round(mgas).toString()
}

function percentile(sorted: number[], p: number): number {
  if (sorted.length === 0) return 0
  if (sorted.length === 1) return sorted[0]
  const idx = (p / 100) * (sorted.length - 1)
  const lo = Math.floor(idx)
  const hi = Math.ceil(idx)
  if (lo === hi) return sorted[lo]
  return sorted[lo] + (sorted[hi] - sorted[lo]) * (idx - lo)
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

interface HistogramBin {
  count: number
  height: number
  rangeStart: number
  rangeEnd: number
  label: string
  color: string
}

function computeHistogramBins(values: number[], threshold: number): { bins: HistogramBin[]; slowCount: number; fastCount: number } {
  const counts = Array(BIN_MULTIPLIERS.length).fill(0) as number[]
  let slowCount = 0
  let fastCount = 0

  for (const value of values) {
    if (value < threshold) slowCount++
    else fastCount++
    const ratio = value / threshold
    let binIndex = BIN_MULTIPLIERS.findIndex((_, i) => {
      const next = BIN_MULTIPLIERS[i + 1]
      return next === undefined ? true : ratio < next
    })
    if (binIndex === -1) binIndex = BIN_MULTIPLIERS.length - 1
    counts[binIndex]++
  }

  const maxCount = Math.max(...counts)
  const logMax = Math.log10(maxCount + 1)
  const bins = counts.map((count, i) => {
    const rangeStart = BIN_MULTIPLIERS[i] * threshold
    const rangeEnd = BIN_MULTIPLIERS[i + 1] !== undefined ? BIN_MULTIPLIERS[i + 1] * threshold : Infinity
    const midpoint =
      BIN_MULTIPLIERS[i + 1] !== undefined
        ? ((BIN_MULTIPLIERS[i] + BIN_MULTIPLIERS[i + 1]) / 2) * threshold
        : BIN_MULTIPLIERS[i] * 1.25 * threshold
    return {
      count,
      height: maxCount > 0 ? (Math.log10(count + 1) / logMax) * 100 : 0,
      rangeStart,
      rangeEnd,
      label: BIN_MULTIPLIERS[i + 1] !== undefined ? `${rangeStart.toFixed(0)}-${rangeEnd.toFixed(0)}` : `${rangeStart.toFixed(0)}+`,
      color: getColorByThreshold(midpoint, threshold),
    }
  })

  return { bins, slowCount, fastCount }
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
  p99: number
}

interface ProcessedTest {
  name: string
  testNumber: number | undefined // 1-based index in suite's test list
  avgMgas: number
  minMgas: number
  maxMgas: number // Per-test max for border color normalization
  p99Mgas: number
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
  isLoading?: boolean
  suiteHash?: string
  suiteName?: string
  stepFilter?: IndexStepType[]
  searchQuery?: string
  onSearchChange?: (query: string | undefined) => void
  showTestName?: boolean
  onShowTestNameChange?: (show: boolean) => void
  useRegex?: boolean
  onUseRegexChange?: (useRegex: boolean) => void
  fullscreen?: boolean
  onFullscreenChange?: (fullscreen: boolean) => void
  showClientStat?: boolean
  onShowClientStatChange?: (show: boolean) => void
  histogramStat?: (typeof STAT_COLUMNS)[number]
  onHistogramStatChange?: (stat: (typeof STAT_COLUMNS)[number]) => void
}

type SortDirection = 'asc' | 'desc'
type SortField = 'testNumber' | (typeof STAT_COLUMNS)[number]

export function TestHeatmap({ stats, testFiles, isDark, isLoading, suiteHash, suiteName, stepFilter = ALL_INDEX_STEP_TYPES, searchQuery, onSearchChange, showTestName: showTestNameProp, onShowTestNameChange, useRegex: useRegexProp, onUseRegexChange, fullscreen: fullscreenProp, onFullscreenChange, showClientStat: showClientStatProp, onShowClientStatChange, histogramStat: histogramStatProp, onHistogramStatChange }: TestHeatmapProps) {
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [sortField, setSortField] = useState<SortField>('avgMgas')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')
  const [threshold, setThreshold] = useState(DEFAULT_THRESHOLD)
  const [internalHistogramStat, setInternalHistogramStat] = useState<(typeof STAT_COLUMNS)[number]>('avgMgas')
  const histogramStat = histogramStatProp ?? internalHistogramStat
  const setHistogramStat = onHistogramStatChange ?? setInternalHistogramStat
  const [runsPerClient, setRunsPerClient] = useState(DEFAULT_RUNS_PER_CLIENT)
  const showClientStat = showClientStatProp ?? false
  const setShowClientStat = onShowClientStatChange ?? (() => {})
  const showTestName = showTestNameProp ?? false
  const [internalUseRegex, setInternalUseRegex] = useState(false)
  const useRegex = useRegexProp ?? internalUseRegex
  const setUseRegex = onUseRegexChange ?? setInternalUseRegex
  const [internalFullscreen, setInternalFullscreen] = useState(false)
  const fullscreen = fullscreenProp ?? internalFullscreen
  const setFullscreen = onFullscreenChange ?? setInternalFullscreen

  useEffect(() => {
    if (!fullscreen) return
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false)
    }
    document.addEventListener('keydown', handleKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', handleKey)
      document.body.style.overflow = ''
    }
  }, [fullscreen, setFullscreen])

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
      const allMgasValues: number[] = []
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
        const clientMgasValues: number[] = []
        for (const run of recentRuns) {
          totalMgas += run.mgas
          clientTotal += run.mgas
          count++
          minClientMgas = Math.min(minClientMgas, run.mgas)
          maxClientMgas = Math.max(maxClientMgas, run.mgas)
          clientMin = Math.min(clientMin, run.mgas)
          clientMgasValues.push(run.mgas)
          allMgasValues.push(run.mgas)
        }
        const sortedClientValues = [...clientMgasValues].sort((a, b) => a - b)
        clientStats[client] = {
          avg: clientTotal / recentRuns.length,
          min: clientMin,
          p99: percentile(sortedClientValues, 99),
        }
      }

      if (count === 0) continue

      const avgMgas = totalMgas / count
      const sortedAllValues = [...allMgasValues].sort((a, b) => a - b)

      processedTests.push({
        name: testName,
        testNumber: testIndexMap.get(testName),
        avgMgas,
        minMgas: minClientMgas,
        maxMgas: maxClientMgas,
        p99Mgas: percentile(sortedAllValues, 99),
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
        const aNum = a.testNumber ?? Infinity
        const bNum = b.testNumber ?? Infinity
        return sortDirection === 'asc' ? aNum - bNum : bNum - aNum
      }
      const aVal = a[sortField]
      const bVal = b[sortField]
      return sortDirection === 'asc' ? aVal - bVal : bVal - aVal
    })
    return sorted
  }, [filteredTests, sortField, sortDirection])

  const totalPages = Math.ceil(sortedTests.length / pageSize)
  const paginatedTests = sortedTests.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  // Calculate histogram data for distribution graph (respects search filter)
  const { bins: histogramData, slowCount, fastCount } = useMemo(() => {
    if (filteredTests.length === 0) return { bins: [] as HistogramBin[], slowCount: 0, fastCount: 0 }
    const values = filteredTests.map((test) => test[histogramStat])
    return computeHistogramBins(values, threshold)
  }, [filteredTests, threshold, histogramStat])

  // Per-client histogram data (respects search filter)
  const perClientHistogramData = useMemo(() => {
    const result: Record<string, { bins: HistogramBin[]; slowCount: number; fastCount: number }> = {}
    const field = STAT_TO_CLIENT_FIELD[histogramStat]
    for (const client of clients) {
      const values: number[] = []
      for (const test of filteredTests) {
        const cs = test.clientStats[client]
        if (cs) values.push(cs[field])
      }
      if (values.length > 0) {
        result[client] = computeHistogramBins(values, threshold)
      }
    }
    return result
  }, [filteredTests, clients, threshold, histogramStat])

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

  const header = (
    <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
      <div className="flex items-center gap-3">
        {fullscreen && suiteHash && (
          <div className="flex items-center gap-2">
            <JDenticon value={suiteHash} size={24} className="shrink-0 rounded-xs" />
            <span className="text-sm font-medium text-gray-500 dark:text-gray-400">
              {suiteName ?? suiteHash}
            </span>
            <span className="text-gray-300 dark:text-gray-600">/</span>
          </div>
        )}
        <h3 className="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-gray-100">
          <Flame className="size-4 text-gray-400 dark:text-gray-500" />
          Test Heatmap
        </h3>
        {isLoading && <Spinner size="sm" />}
        <span className="rounded-full bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/50 dark:text-purple-300">
          {search ? `${filteredTests.length} / ${allTests.length}` : allTests.length} tests
        </span>
      </div>
      <div className="flex items-center gap-2">
        <input
          type="text"
          value={search}
          onChange={(e) => handleSearchChange(e.target.value)}
          placeholder={useRegex ? 'Regex pattern...' : 'Filter tests...'}
          className={clsx(
            'rounded-xs border bg-white px-3 py-1 text-sm placeholder-gray-400 focus:outline-hidden focus:ring-1 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500',
            useRegex && search && (() => { try { new RegExp(search); return false } catch { return true } })()
              ? 'border-red-400 focus:border-red-500 focus:ring-red-500 dark:border-red-500'
              : 'border-gray-300 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-600',
          )}
        />
        <button
          onClick={() => setUseRegex(!useRegex)}
          title={useRegex ? 'Regex mode (click to switch to text)' : 'Text mode (click to switch to regex)'}
          className={clsx(
            'rounded-xs px-1.5 py-1 font-mono text-sm transition-colors',
            useRegex
              ? 'bg-blue-500 text-white'
              : 'border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
          )}
        >
          .*
        </button>
        <button
          onClick={() => setFullscreen(!fullscreen)}
          className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
          title={fullscreen ? 'Exit fullscreen' : 'Fullscreen'}
        >
          {fullscreen ? <X className="size-4" /> : <Maximize2 className="size-4" />}
        </button>
      </div>
    </div>
  )

  if (allTests.length === 0) {
    return (
      <div className={
        fullscreen
          ? 'fixed inset-0 z-40 flex flex-col overflow-hidden bg-white dark:bg-gray-900'
          : 'overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800'
      }>
        {header}
        <p className="py-4 text-center text-sm/6 text-gray-500 dark:text-gray-400">
          No test performance data available.
        </p>
      </div>
    )
  }

  const controls = (
    <div className={clsx('flex items-start justify-between gap-x-6 gap-y-2', fullscreen ? 'shrink-0 px-4 pt-4 pb-2' : 'px-4 pt-4')}>
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

          {/* Client stat toggle */}
          <label className="flex cursor-pointer items-center gap-1.5">
            <input
              type="checkbox"
              checked={showClientStat}
              onChange={(e) => setShowClientStat(e.target.checked)}
              className="size-3.5 cursor-pointer rounded-xs border-gray-300 text-blue-500 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
            />
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Client stats</span>
          </label>

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

          {/* Separator */}
          <div className="hidden h-4 w-px bg-gray-200 sm:block dark:bg-gray-700" />

          {/* Runs per client */}
          <div className="flex items-center gap-1.5">
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Runs per client:</span>
            <select
              value={runsPerClient}
              onChange={(e) => setRunsPerClient(Number(e.target.value))}
              className="rounded-sm border border-gray-300 bg-white px-1.5 py-0.5 text-xs/5 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
            >
              {RUNS_PER_CLIENT_OPTIONS.map((count) => (
                <option key={count} value={count}>
                  {count}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Pagination */}
        <div className="flex shrink-0 flex-col items-end gap-1.5">
          <div className="flex items-center gap-1.5">
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Show</span>
            <select
              value={pageSize}
              onChange={(e) => handlePageSizeChange(Number(e.target.value))}
              className="rounded-sm border border-gray-300 bg-white px-1.5 py-0.5 text-xs/5 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
            >
              {PAGE_SIZE_OPTIONS.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">per page</span>
          </div>
          {totalPages > 1 && <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={handlePageChange} />}
        </div>
    </div>
  )

  const tableContent = (
    <div className={fullscreen ? 'min-h-0 flex-1 overflow-auto px-4' : 'max-h-[75vh] overflow-auto px-4'}>
      <table className="w-full border-collapse text-sm/6">
          <thead className="sticky top-0 z-20">
            <tr>
              <th className={clsx('sticky left-0 z-30', fullscreen ? 'bg-white dark:bg-gray-900' : 'bg-white dark:bg-gray-800')} />
              <th colSpan={clients.length} className={clsx(fullscreen ? 'bg-white dark:bg-gray-900' : 'bg-white dark:bg-gray-800')} />
              <th colSpan={STAT_COLUMNS.length} className={clsx('px-2 pt-2 pb-0 text-center text-xs/5 font-medium text-gray-400 dark:text-gray-500', fullscreen ? 'bg-white dark:bg-gray-900' : 'bg-white dark:bg-gray-800')}>
                MGas/s
              </th>
            </tr>
            <tr>
              <th className={clsx('sticky left-0 z-30 px-2 pt-0 pb-2 text-right', fullscreen ? 'bg-white dark:bg-gray-900' : 'bg-white dark:bg-gray-800')}>
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
                <th key={client} className={clsx('px-1 pt-0 pb-2 text-center', fullscreen ? 'bg-white dark:bg-gray-900' : 'bg-white dark:bg-gray-800')}>
                  <ClientBadge client={client} />
                </th>
              ))}
              {STAT_COLUMNS.map((col) => (
                <th key={col} className={clsx('px-2 pt-0 pb-2 text-right', fullscreen ? 'bg-white dark:bg-gray-900' : 'bg-white dark:bg-gray-800')}>
                  <button
                    onClick={() => handleSort(col)}
                    className="inline-flex items-center gap-1 text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                  >
                    {STAT_COLUMN_LABELS[col]}
                    {sortField === col && (
                      <ChevronUp className={clsx('size-3 transition-transform', sortDirection === 'desc' && 'rotate-180')} />
                    )}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {paginatedTests.map((test) => (
              <Fragment key={test.name}>
              {showTestName && (
                <tr className="border-t border-gray-100 bg-gray-50/80 dark:border-gray-700/50 dark:bg-gray-900/60">
                  <td
                    colSpan={clients.length + 1 + STAT_COLUMNS.length}
                    className="truncate px-3 py-1 font-mono text-xs/5 text-gray-400 dark:text-gray-500"
                    title={test.name}
                  >
                    <span className="mr-1.5 font-sans text-gray-300 dark:text-gray-600">&#9656;</span>
                    <HighlightedName name={test.name} search={search} useRegex={useRegex} />
                  </td>
                </tr>
              )}
              <tr className={clsx(showTestName ? 'border-t-0' : 'border-t border-gray-200 dark:border-gray-700')}>
                <td
                  className={clsx('sticky left-0 z-10 px-2 py-1.5 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400', fullscreen ? 'bg-white dark:bg-gray-900' : 'bg-white dark:bg-gray-800')}
                  title={test.name}
                >
                  {test.testNumber ?? '-'}
                </td>
                {clients.map((client) => {
                  const runs = test.clientRuns[client]
                  const stats = test.clientStats[client]
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
                          {showClientStat && (
                            <div className="font-mono text-xs/4 text-gray-400 dark:text-gray-500">-</div>
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
                        {showClientStat && stats && (
                          <table className="text-xs/4">
                            <tbody>
                              <tr>
                                <td className="pr-1 text-left text-gray-400 dark:text-gray-500">avg</td>
                                <td className="font-mono text-gray-500 dark:text-gray-400">{formatMGasCompact(stats.avg)}</td>
                              </tr>
                              <tr>
                                <td className="pr-1 text-left text-gray-400 dark:text-gray-500">min</td>
                                <td className="font-mono text-gray-500 dark:text-gray-400">{formatMGasCompact(stats.min)}</td>
                              </tr>
                              <tr>
                                <td className="pr-1 text-left text-gray-400 dark:text-gray-500">p99</td>
                                <td className="font-mono text-gray-500 dark:text-gray-400">{formatMGasCompact(stats.p99)}</td>
                              </tr>
                            </tbody>
                          </table>
                        )}
                      </div>
                    </td>
                  )
                })}
                {STAT_COLUMNS.map((col) => (
                  <td key={col} className="px-2 py-1.5 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400">
                    {formatMGasCompact(test[col])}
                  </td>
                ))}
              </tr>
              </Fragment>
            ))}
          </tbody>
        </table>
    </div>
  )

  const bottomSection = (
    <div className={clsx('flex flex-col gap-4', fullscreen ? 'shrink-0 px-4 pb-4' : 'px-4 pb-4')}>
      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex justify-end">
          <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={handlePageChange} />
        </div>
      )}

      {/* Distribution Histogram */}
      {histogramData.length > 0 && (
        <div className="flex flex-col gap-1 border-t border-gray-200 pt-4 dark:border-gray-700">
          <p className="text-xs/5 text-gray-400 dark:text-gray-500">
            Stats are computed from the {runsPerClient} most recent runs per client visible in the heatmap. Changing &quot;Runs per client&quot; alters these values.
          </p>
          <div className="mb-1 flex items-center justify-between">
            <span className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Distribution by threshold</span>
            <div className="flex items-center gap-2">
              <span className="text-xs/5 text-gray-500 dark:text-gray-400">Stat:</span>
              <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
                {STAT_COLUMNS.map((col) => (
                  <button
                    key={col}
                    onClick={() => setHistogramStat(col)}
                    className={clsx(
                      'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                      histogramStat === col
                        ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                        : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                    )}
                  >
                    {STAT_COLUMN_LABELS[col]}
                  </button>
                ))}
              </div>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
            {/* All clients (global) */}
            <div className="flex flex-col gap-1 rounded-sm border border-gray-200 p-2 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <span className="inline-flex w-28 items-center gap-1.5 rounded-sm bg-gray-100 px-2.5 py-0.5 text-xs/5 font-medium text-gray-700 dark:bg-gray-700 dark:text-gray-300">All</span>
                <div className="flex gap-2 text-xs/5 font-medium tabular-nums">
                  {slowCount > 0 && <span className="text-red-600 dark:text-red-400">{slowCount} slow</span>}
                  {fastCount > 0 && <span className="text-green-600 dark:text-green-400">{fastCount} fast</span>}
                </div>
              </div>
              <div className="relative flex h-16 items-end gap-0.5">
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
                <div
                  className="absolute top-0 bottom-0 w-0.5 bg-black/30 dark:bg-white/30"
                  style={{ left: `${(4 / 11) * 100}%` }}
                  title={`Threshold: ${threshold} MGas/s`}
                />
              </div>
            </div>
            {/* Per-client */}
            {clients.map((client) => {
              const data = perClientHistogramData[client]
              if (!data) return null
              return (
                <div key={client} className="flex flex-col gap-1 rounded-sm border border-gray-200 p-2 dark:border-gray-700">
                  <div className="flex items-center justify-between">
                    <ClientBadge client={client} />
                    <div className="flex gap-2 text-xs/5 font-medium tabular-nums">
                      {data.slowCount > 0 && <span className="text-red-600 dark:text-red-400">{data.slowCount} slow</span>}
                      {data.fastCount > 0 && <span className="text-green-600 dark:text-green-400">{data.fastCount} fast</span>}
                    </div>
                  </div>
                  <div className="relative flex h-16 items-end gap-0.5">
                    {data.bins.map((bin, i) => (
                      <div
                        key={i}
                        className="flex-1 rounded-t-xs transition-all hover:opacity-80"
                        style={{
                          height: `${bin.height}%`,
                          backgroundColor: bin.color,
                          minHeight: bin.count > 0 ? '2px' : '0',
                        }}
                        title={`${client} · ${bin.label} MGas/s: ${bin.count} tests`}
                      />
                    ))}
                    <div
                      className="absolute top-0 bottom-0 w-0.5 bg-black/30 dark:bg-white/30"
                      style={{ left: `${(4 / 11) * 100}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Legend */}
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
    </div>
  )

  return (
    <div className={
      fullscreen
        ? 'fixed inset-0 z-40 flex flex-col overflow-hidden bg-white dark:bg-gray-900'
        : 'overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800'
    }>
      {header}
      {controls}
      {tableContent}
      {bottomSection}

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
