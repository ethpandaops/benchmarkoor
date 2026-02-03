import { useMemo, useState } from 'react'
import clsx from 'clsx'
import type { TestEntry, SuiteTest, AggregatedStats, MethodsAggregated, StepResult } from '@/api/types'
import { Modal } from '@/components/shared/Modal'
import { TimeBreakdown } from './TimeBreakdown'
import { MGasBreakdown } from './MGasBreakdown'
import { ExecutionsList } from './ExecutionsList'
import type { TestStatusFilter } from './TestsTable'
import { type StepTypeOption, ALL_STEP_TYPES } from '@/pages/RunDetailPage'
import { formatDuration } from '@/utils/format'
import { EESTInfoContent, type OpcodeSortMode } from '@/components/suite-detail/TestFilesList'

// Aggregate stats from selected steps of a test entry
function getAggregatedStats(entry: TestEntry, stepFilter: StepTypeOption[] = ALL_STEP_TYPES): AggregatedStats | undefined {
  if (!entry.steps) return undefined

  // Build array of steps based on filter
  const stepMap: Record<StepTypeOption, StepResult | undefined> = {
    setup: entry.steps.setup,
    test: entry.steps.test,
    cleanup: entry.steps.cleanup,
  }

  const steps = stepFilter
    .map((type) => stepMap[type])
    .filter((s): s is StepResult => s?.aggregated !== undefined)

  if (steps.length === 0) return undefined

  // Sum up stats from all steps
  let timeTotal = 0
  let gasUsedTotal = 0
  let gasUsedTimeTotal = 0
  let success = 0
  let fail = 0

  // Merge method stats from all steps
  const mergedTimes: Record<string, { count: number; last: number; min?: number; max?: number; mean?: number; p50?: number; p95?: number; p99?: number }> = {}
  const mergedMgasS: Record<string, { count: number; last: number; min?: number; max?: number; mean?: number; p50?: number; p95?: number; p99?: number }> = {}

  for (const step of steps) {
    if (step?.aggregated) {
      timeTotal += step.aggregated.time_total
      gasUsedTotal += step.aggregated.gas_used_total
      gasUsedTimeTotal += step.aggregated.gas_used_time_total
      success += step.aggregated.success
      fail += step.aggregated.fail

      // Merge times
      for (const [method, stats] of Object.entries(step.aggregated.method_stats.times)) {
        if (!mergedTimes[method]) {
          mergedTimes[method] = { ...stats }
        } else {
          mergedTimes[method].count += stats.count
          mergedTimes[method].last = stats.last
        }
      }

      // Merge mgas_s
      for (const [method, stats] of Object.entries(step.aggregated.method_stats.mgas_s)) {
        if (!mergedMgasS[method]) {
          mergedMgasS[method] = { ...stats }
        } else {
          mergedMgasS[method].count += stats.count
          mergedMgasS[method].last = stats.last
        }
      }
    }
  }

  return {
    time_total: timeTotal,
    gas_used_total: gasUsedTotal,
    gas_used_time_total: gasUsedTimeTotal,
    success,
    fail,
    msg_count: 0,
    method_stats: { times: mergedTimes, mgas_s: mergedMgasS } as MethodsAggregated,
  }
}

export type SortMode = 'order' | 'mgas'

interface TestHeatmapProps {
  tests: Record<string, TestEntry>
  suiteTests?: SuiteTest[]
  runId: string
  suiteHash?: string
  selectedTest?: string
  statusFilter?: TestStatusFilter
  searchQuery?: string
  sortMode?: SortMode
  threshold?: number
  stepFilter?: StepTypeOption[]
  onSelectedTestChange?: (testName: string | undefined) => void
  onSortModeChange?: (mode: SortMode) => void
  onThresholdChange?: (threshold: number) => void
  onSearchChange?: (query: string) => void
}

const COLORS = [
  '#22c55e', // green - best (highest MGas/s)
  '#84cc16', // lime
  '#eab308', // yellow
  '#f97316', // orange
  '#ef4444', // red - worst (lowest MGas/s)
]

const MIN_THRESHOLD = 10
const MAX_THRESHOLD = 1000
const DEFAULT_THRESHOLD = 60

function calculateMGasPerSec(gasUsedTotal: number, gasUsedTimeTotal: number): number | undefined {
  if (gasUsedTimeTotal <= 0 || gasUsedTotal <= 0) return undefined
  return (gasUsedTotal * 1000) / gasUsedTimeTotal
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? (
        <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      ) : (
        <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
          />
        </svg>
      )}
    </button>
  )
}

function getColorByThreshold(value: number, threshold: number): string {
  // Scale: 0 = threshold (yellow), >threshold = green, <threshold = red
  // Range: 0 to 2*threshold maps to full color scale
  const ratio = value / threshold
  if (ratio >= 2) return COLORS[0] // Very fast - green
  if (ratio >= 1.5) return COLORS[1] // Fast - lime
  if (ratio >= 1) return COLORS[2] // At threshold - yellow
  if (ratio >= 0.5) return COLORS[3] // Slow - orange
  return COLORS[4] // Very slow - red
}

interface TestData {
  testKey: string
  filename: string
  order: number
  mgasPerSec: number
  gasUsedTotal: number
  gasUsedTimeTotal: number
  hasFail: boolean
  noData: boolean
}

// Diagonal stripe pattern for tests without data
const NO_DATA_STYLE = {
  backgroundColor: '#374151',
  backgroundImage: 'repeating-linear-gradient(45deg, transparent, transparent 2px, #1f2937 2px, #1f2937 4px)',
}

export function TestHeatmap({
  tests,
  suiteTests,
  runId,
  suiteHash,
  selectedTest,
  statusFilter = 'all',
  searchQuery = '',
  sortMode: sortModeProp,
  threshold: thresholdProp,
  stepFilter = ALL_STEP_TYPES,
  onSelectedTestChange,
  onSortModeChange,
  onThresholdChange,
}: TestHeatmapProps) {
  const sortMode = sortModeProp ?? 'order'
  const threshold = thresholdProp ?? DEFAULT_THRESHOLD
  const [tooltip, setTooltip] = useState<{ test: TestData; x: number; y: number } | null>(null)
  const [opcodeSort, setOpcodeSort] = useState<OpcodeSortMode>('name')

  const handleSortModeChange = (mode: SortMode) => {
    onSortModeChange?.(mode)
  }

  const handleThresholdChange = (value: number) => {
    if (value >= MIN_THRESHOLD && value <= MAX_THRESHOLD) {
      onThresholdChange?.(value)
    }
  }

  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((test, index) => [test.name, index + 1]))
  }, [suiteTests])

  const genesisMap = useMemo(() => {
    if (!suiteTests) return new Map<string, string>()
    const m = new Map<string, string>()
    for (const test of suiteTests) {
      if (test.genesis) m.set(test.name, test.genesis)
    }
    return m
  }, [suiteTests])

  const { testData, minMgas, maxMgas } = useMemo(() => {
    const data: TestData[] = []
    let minMgas = Infinity
    let maxMgas = -Infinity

    for (const [testName, entry] of Object.entries(tests)) {
      // Use stepFilter for MGas/s calculation
      const statsFiltered = getAggregatedStats(entry, stepFilter)
      // Use all steps for hasFail indicator
      const statsAll = getAggregatedStats(entry, ALL_STEP_TYPES)
      const mgasPerSec = statsFiltered ? calculateMGasPerSec(statsFiltered.gas_used_total, statsFiltered.gas_used_time_total) : undefined
      const order = executionOrder.get(testName) ?? Infinity
      const noData = mgasPerSec === undefined

      if (!noData) {
        minMgas = Math.min(minMgas, mgasPerSec)
        maxMgas = Math.max(maxMgas, mgasPerSec)
      }

      data.push({
        testKey: testName,
        filename: testName,
        order,
        mgasPerSec: mgasPerSec ?? 0,
        gasUsedTotal: statsFiltered?.gas_used_total ?? 0,
        gasUsedTimeTotal: statsFiltered?.gas_used_time_total ?? 0,
        hasFail: statsAll ? statsAll.fail > 0 : false,
        noData,
      })
    }

    if (minMgas === Infinity) minMgas = 0
    if (maxMgas === -Infinity) maxMgas = 0

    return { testData: data, minMgas, maxMgas }
  }, [tests, executionOrder, stepFilter])

  const sortedData = useMemo(() => {
    const sorted = [...testData]
    if (sortMode === 'order') {
      sorted.sort((a, b) => a.order - b.order)
    } else {
      sorted.sort((a, b) => a.mgasPerSec - b.mgasPerSec) // slowest first
    }
    return sorted
  }, [testData, sortMode])

  const histogramData = useMemo(() => {
    const testsWithData = testData.filter((t) => !t.noData)
    if (testsWithData.length === 0) return []

    // Create bins based on threshold: 0, 0.25x, 0.5x, 0.75x, 1x, 1.25x, 1.5x, 1.75x, 2x, 2.5x, 3x+
    const binMultipliers = [0, 0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.5, 3]
    const bins = Array(binMultipliers.length).fill(0)

    for (const test of testsWithData) {
      const ratio = test.mgasPerSec / threshold
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
      const midpoint = binMultipliers[i + 1] !== undefined
        ? (binMultipliers[i] + binMultipliers[i + 1]) / 2 * threshold
        : binMultipliers[i] * 1.25 * threshold
      return {
        count,
        height: maxCount > 0 ? (Math.log10(count + 1) / logMax) * 100 : 0,
        rangeStart,
        rangeEnd,
        label: binMultipliers[i + 1] !== undefined
          ? `${rangeStart.toFixed(0)}-${rangeEnd.toFixed(0)}`
          : `${rangeStart.toFixed(0)}+`,
        color: getColorByThreshold(midpoint, threshold),
      }
    })
  }, [testData, threshold])

  const handleMouseEnter = (test: TestData, event: React.MouseEvent) => {
    const rect = event.currentTarget.getBoundingClientRect()
    setTooltip({
      test,
      x: rect.left + rect.width / 2,
      y: rect.top,
    })
  }

  const handleMouseLeave = () => {
    setTooltip(null)
  }

  if (testData.length === 0) {
    return (
      <div className="flex h-32 items-center justify-center text-sm/6 text-gray-500 dark:text-gray-400">
        No MGas/s data available
      </div>
    )
  }

  return (
    <div className="relative flex flex-col gap-4">
      {/* Controls */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Sort by:</span>
            <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
              <button
                onClick={() => handleSortModeChange('order')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  sortMode === 'order'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                Test #
              </button>
              <button
                onClick={() => handleSortModeChange('mgas')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  sortMode === 'mgas'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                MGas/s
              </button>
            </div>
          </div>
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
                onClick={() => handleThresholdChange(DEFAULT_THRESHOLD)}
                className="text-xs/5 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
              >
                Reset
              </button>
            )}
          </div>
        </div>
        <div className="text-xs/5 text-gray-500 dark:text-gray-400">
          {testData.length} tests | {minMgas.toFixed(1)} - {maxMgas.toFixed(1)} MGas/s
        </div>
      </div>

      {/* Heatmap Grid */}
      <div className="flex flex-col gap-1">
        <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">
          Tests {sortMode === 'order' ? '(by execution order)' : '(by MGas/s, slowest first)'}
        </div>
        <div className="flex flex-wrap gap-0.5">
          {sortedData.map((test) => {
            const matchesStatusFilter =
              statusFilter === 'all' ||
              (statusFilter === 'passed' && !test.hasFail) ||
              (statusFilter === 'failed' && test.hasFail)
            const matchesSearchQuery = !searchQuery || test.testKey.toLowerCase().includes(searchQuery.toLowerCase())
            const matchesFilter = matchesStatusFilter && matchesSearchQuery
            const baseStyle = test.noData ? NO_DATA_STYLE : { backgroundColor: getColorByThreshold(test.mgasPerSec, threshold) }
            const style = matchesFilter ? baseStyle : { ...baseStyle, opacity: 0.2 }

            return (
              <button
                key={test.testKey}
                onClick={() => onSelectedTestChange?.(test.testKey)}
                onMouseEnter={(e) => handleMouseEnter(test, e)}
                onMouseLeave={handleMouseLeave}
                className={clsx(
                  'size-3 cursor-pointer rounded-xs transition-all hover:scale-150 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
                  test.hasFail && 'ring-1 ring-red-500',
                )}
                style={style}
              />
            )
          })}
        </div>
      </div>

      {/* Histogram */}
      <div className="flex flex-col gap-1">
        <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Distribution (by threshold multiples)</div>
        <div className="flex items-end gap-1">
          <div className="flex h-16 w-8 shrink-0 flex-col items-center justify-end">
            <span className="text-xs/5 font-medium text-red-600 dark:text-red-400">
              {testData.filter((t) => !t.noData && t.mgasPerSec < threshold).length}
            </span>
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
            <span className="text-xs/5 font-medium text-green-600 dark:text-green-400">
              {testData.filter((t) => !t.noData && t.mgasPerSec >= threshold).length}
            </span>
            <span className="text-xs/5 text-gray-400 dark:text-gray-500">fast</span>
          </div>
        </div>
        <div className="flex justify-between px-9 text-xs/5 text-gray-400 dark:text-gray-500">
          <span>0</span>
          <span className="font-medium text-yellow-600 dark:text-yellow-400">{threshold} MGas/s (threshold)</span>
          <span>{threshold * 3}+</span>
        </div>
      </div>

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
        </span>
        <span className="text-gray-400 dark:text-gray-500">({threshold} MGas/s = yellow)</span>
        <span>
          <span className="mr-1 inline-block size-3 rounded-xs" style={NO_DATA_STYLE} />
          No data
        </span>
        <span>
          <span className="mr-1 inline-block size-3 rounded-xs ring-1 ring-red-500" style={{ backgroundColor: COLORS[2] }} />
          Has failures
        </span>
      </div>

      {/* Tooltip */}
      {tooltip && (
        <div
          className="pointer-events-none fixed z-50 rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-700"
          style={{
            left: tooltip.x,
            top: tooltip.y - 8,
            transform: 'translate(-50%, -100%)',
          }}
        >
          <div className="flex flex-col gap-1">
            <div className="font-medium">Test #{tooltip.test.order}</div>
            {genesisMap.get(tooltip.test.testKey) && (
              <div className="text-gray-500 dark:text-gray-400">Genesis: {genesisMap.get(tooltip.test.testKey)}</div>
            )}
            <div>MGas/s: {tooltip.test.noData ? 'No data' : tooltip.test.mgasPerSec.toFixed(2)}</div>
            {!tooltip.test.noData && (
              <>
                <div>Gas used: {(tooltip.test.gasUsedTotal / 1_000_000).toFixed(2)} MGas</div>
                <div>Gas time: {formatDuration(tooltip.test.gasUsedTimeTotal)}</div>
              </>
            )}
            <div className="text-gray-500 dark:text-gray-400">Based on steps: {stepFilter.join(', ')}</div>
            <div className="max-w-48 truncate text-gray-500 dark:text-gray-400">{tooltip.test.filename}</div>
            {tooltip.test.noData && <div className="text-gray-500 dark:text-gray-400">No gas usage data available</div>}
            {tooltip.test.hasFail && <div className="text-red-600 dark:text-red-400">Has failures</div>}
            <div className="mt-1 text-gray-400 dark:text-gray-500">Click for details</div>
          </div>
        </div>
      )}

      {/* Test Detail Modal */}
      {selectedTest && tests[selectedTest] && (() => {
        const entry = tests[selectedTest]
        const stats = getAggregatedStats(entry, stepFilter)
        return (
          <Modal
            isOpen={!!selectedTest}
            onClose={() => onSelectedTestChange?.(undefined)}
            title={`Test #${executionOrder.get(selectedTest) ?? '?'}`}
          >
            <div className="flex flex-col gap-6">
              <div className="flex flex-col gap-2">
                <div>
                  <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Test Name</div>
                  <div className="flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
                    <span className="min-w-0 break-all">{selectedTest}</span>
                    <CopyButton text={selectedTest} />
                  </div>
                </div>
                {entry.dir && (
                  <div>
                    <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Directory</div>
                    <div className="flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
                      <span>{entry.dir}</span>
                      <CopyButton text={entry.dir} />
                    </div>
                  </div>
                )}
                {genesisMap.get(selectedTest) && (
                  <div>
                    <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis</div>
                    <div className="flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
                      <span className="font-mono">{genesisMap.get(selectedTest)}</span>
                      <CopyButton text={genesisMap.get(selectedTest)!} />
                    </div>
                  </div>
                )}
              </div>
              {(() => {
                const matchingSuiteTest = suiteTests?.find((t) => t.name === selectedTest)
                if (!matchingSuiteTest) return null
                return <EESTInfoContent test={matchingSuiteTest} opcodeSort={opcodeSort} onOpcodeSortChange={setOpcodeSort} />
              })()}
              {stats && (
                <>
                  <TimeBreakdown methods={stats.method_stats.times} />
                  <MGasBreakdown methods={stats.method_stats.mgas_s} />
                </>
              )}
              {suiteHash && entry.steps && (
                <div className="flex flex-col gap-6">
                  {entry.steps.setup && (
                    <div>
                      <h4 className="mb-2 text-sm/6 font-semibold text-gray-900 dark:text-gray-100">Setup Step</h4>
                      <ExecutionsList
                        runId={runId}
                        suiteHash={suiteHash}
                        testName={selectedTest}
                        stepType="setup"
                      />
                    </div>
                  )}
                  {entry.steps.test && (
                    <div>
                      <h4 className="mb-2 text-sm/6 font-semibold text-gray-900 dark:text-gray-100">Test Step</h4>
                      <ExecutionsList
                        runId={runId}
                        suiteHash={suiteHash}
                        testName={selectedTest}
                        stepType="test"
                      />
                    </div>
                  )}
                  {entry.steps.cleanup && (
                    <div>
                      <h4 className="mb-2 text-sm/6 font-semibold text-gray-900 dark:text-gray-100">Cleanup Step</h4>
                      <ExecutionsList
                        runId={runId}
                        suiteHash={suiteHash}
                        testName={selectedTest}
                        stepType="cleanup"
                      />
                    </div>
                  )}
                </div>
              )}
            </div>
          </Modal>
        )
      })()}
    </div>
  )
}
