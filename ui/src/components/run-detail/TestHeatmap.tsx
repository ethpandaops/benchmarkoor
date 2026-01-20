import { useMemo, useState } from 'react'
import clsx from 'clsx'
import type { TestEntry, SuiteFile } from '@/api/types'
import { Modal } from '@/components/shared/Modal'
import { TimeBreakdown } from './TimeBreakdown'
import { MGasBreakdown } from './MGasBreakdown'
import { ExecutionsList } from './ExecutionsList'

export type SortMode = 'order' | 'mgas'

interface TestHeatmapProps {
  tests: Record<string, TestEntry>
  suiteTests?: SuiteFile[]
  runId: string
  suiteHash?: string
  selectedTest?: string
  onSelectedTestChange?: (testName: string | undefined) => void
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

function makeTestKey(filename: string, dir?: string): string {
  return dir ? `${dir}/${filename}` : filename
}

function calculateMGasPerSec(gasUsedTotal: number, gasUsedTimeTotal: number): number | undefined {
  if (gasUsedTimeTotal <= 0 || gasUsedTotal <= 0) return undefined
  return (gasUsedTotal * 1000) / gasUsedTimeTotal
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
  onSelectedTestChange,
}: TestHeatmapProps) {
  const [sortMode, setSortMode] = useState<SortMode>('order')
  const [threshold, setThreshold] = useState(DEFAULT_THRESHOLD)
  const [tooltip, setTooltip] = useState<{ test: TestData; x: number; y: number } | null>(null)

  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((file, index) => [makeTestKey(file.f, file.d), index + 1]))
  }, [suiteTests])

  const { testData, minMgas, maxMgas } = useMemo(() => {
    const data: TestData[] = []
    let minMgas = Infinity
    let maxMgas = -Infinity

    for (const [testKey, entry] of Object.entries(tests)) {
      const mgasPerSec = calculateMGasPerSec(entry.aggregated.gas_used_total, entry.aggregated.gas_used_time_total)
      const order = executionOrder.get(testKey) ?? Infinity
      const filename = entry.dir ? testKey.slice(entry.dir.length + 1) : testKey
      const noData = mgasPerSec === undefined

      if (!noData) {
        minMgas = Math.min(minMgas, mgasPerSec)
        maxMgas = Math.max(maxMgas, mgasPerSec)
      }

      data.push({
        testKey,
        filename,
        order,
        mgasPerSec: mgasPerSec ?? 0,
        hasFail: entry.aggregated.fail > 0,
        noData,
      })
    }

    if (minMgas === Infinity) minMgas = 0
    if (maxMgas === -Infinity) maxMgas = 0

    return { testData: data, minMgas, maxMgas }
  }, [tests, executionOrder])

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
                onClick={() => setSortMode('order')}
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
                onClick={() => setSortMode('mgas')}
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
              onChange={(e) => setThreshold(Number(e.target.value))}
              className="h-1.5 w-24 cursor-pointer appearance-none rounded-full bg-gray-200 accent-blue-500 dark:bg-gray-700"
            />
            <input
              type="number"
              min={MIN_THRESHOLD}
              max={MAX_THRESHOLD}
              value={threshold}
              onChange={(e) => {
                const val = Number(e.target.value)
                if (val >= MIN_THRESHOLD && val <= MAX_THRESHOLD) {
                  setThreshold(val)
                }
              }}
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
          {sortedData.map((test) => (
            <button
              key={test.testKey}
              onClick={() => onSelectedTestChange?.(test.testKey)}
              onMouseEnter={(e) => handleMouseEnter(test, e)}
              onMouseLeave={handleMouseLeave}
              className={clsx(
                'size-3 cursor-pointer rounded-xs transition-all hover:scale-150 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
                test.hasFail && 'ring-1 ring-red-500',
              )}
              style={test.noData ? NO_DATA_STYLE : { backgroundColor: getColorByThreshold(test.mgasPerSec, threshold) }}
            />
          ))}
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
            <div>MGas/s: {tooltip.test.noData ? 'No data' : tooltip.test.mgasPerSec.toFixed(2)}</div>
            <div className="max-w-48 truncate text-gray-500 dark:text-gray-400">{tooltip.test.filename}</div>
            {tooltip.test.noData && <div className="text-gray-500 dark:text-gray-400">No gas usage data available</div>}
            {tooltip.test.hasFail && <div className="text-red-600 dark:text-red-400">Has failures</div>}
            <div className="mt-1 text-gray-400 dark:text-gray-500">Click for details</div>
          </div>
        </div>
      )}

      {/* Test Detail Modal */}
      {selectedTest && tests[selectedTest] && (
        <Modal
          isOpen={!!selectedTest}
          onClose={() => onSelectedTestChange?.(undefined)}
          title={`Test #${executionOrder.get(selectedTest) ?? '?'}: ${tests[selectedTest].dir ? selectedTest.slice(tests[selectedTest].dir!.length + 1) : selectedTest}`}
        >
          <div className="flex flex-col gap-6">
            <TimeBreakdown methods={tests[selectedTest].aggregated.method_stats.times} />
            <MGasBreakdown methods={tests[selectedTest].aggregated.method_stats.mgas_s} />
            {suiteHash && (
              <ExecutionsList
                runId={runId}
                suiteHash={suiteHash}
                testName={tests[selectedTest].dir ? selectedTest.slice(tests[selectedTest].dir!.length + 1) : selectedTest}
                dir={tests[selectedTest].dir}
              />
            )}
          </div>
        </Modal>
      )}
    </div>
  )
}
