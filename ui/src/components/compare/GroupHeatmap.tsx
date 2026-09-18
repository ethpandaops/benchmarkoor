import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import clsx from 'clsx'
import { LayoutGrid } from 'lucide-react'
import type { AggregatedStats, SuiteTest } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { ColorScaleLegend } from '@/components/shared/ColorScaleLegend'
import { TestName } from '@/components/shared/TestName'
import {
  DEFAULT_THRESHOLD,
  MAX_THRESHOLD,
  MIN_THRESHOLD,
  THRESHOLD_COLORS,
  THRESHOLD_LIMIT_STEP,
  THRESHOLD_RATIOS,
  getColorByThreshold,
  thresholdStep,
  thresholdStepRange,
} from '@/utils/perfThreshold'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

// One row per group, one column per test. When the tests do not fit the
// width, the matrix wraps into stanzas: each stanza repeats the group
// rows for its slice of tests, so a column is the same test in every row.

// ColorMode selects what a tile says: throughput against an absolute
// threshold, or the ratio to the baseline group on the same test.
type ColorMode = 'mgas' | 'baseline'
type SortMode = 'order' | 'spread'

// Same tile as the run-detail heatmap. The logo is the tile size too,
// so it does not set a taller row pitch than the tiles.
const TILE_PX = 12
const GAP_PX = 2
// Width of the logo column before the tiles of a row.
const LABEL_PX = 20

// Hatched tile for a test the group has no throughput for. Same as the
// run-detail heatmap.
const NO_DATA_STYLE = {
  backgroundColor: '#374151',
  backgroundImage: 'repeating-linear-gradient(45deg, transparent, transparent 2px, #1f2937 2px, #1f2937 4px)',
}

interface HeatmapTest {
  name: string
  order: number
  /** MGas/s per group, in `runs` order. */
  values: (number | undefined)[]
  /** Whether the group reported failed executions for this test. */
  fails: boolean[]
  /** Best over worst group value minus one. Zero when fewer than two groups have a value. */
  spread: number
}

interface GroupHeatmapProps {
  /** One synthetic run per group. */
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
  labelMode: LabelMode
  baselineIdx: number
  onBaselineChange: (idx: number) => void
  testNameFilter?: (name: string) => boolean
  onTestClick?: (testName: string) => void
}

function calculateMGasPerSec(stats: AggregatedStats | undefined): number | undefined {
  if (!stats || stats.gas_used_time_total <= 0 || stats.gas_used_total <= 0) return undefined
  return (stats.gas_used_total * 1000) / stats.gas_used_time_total
}

/** Legend range of a step in baseline mode, e.g. "1.25× – 1.5× the baseline". */
function baselineStepRange(step: number): string {
  const fmt = (ratio: number) => `${ratio >= 10 ? ratio.toFixed(0) : ratio.toFixed(2).replace(/\.?0+$/, '')}×`
  const low = THRESHOLD_RATIOS[step]
  if (step === 0) return `≥ ${fmt(low)} the baseline`

  const high = THRESHOLD_RATIOS[step - 1]
  if (low === 0) return `< ${fmt(high)} the baseline`

  return `${fmt(low)} – ${fmt(high)} the baseline`
}

/** Render a ratio to the baseline as a signed percentage, e.g. 1.2 -> "+20%". */
function formatRatio(ratio: number): string {
  const percent = (ratio - 1) * 100
  const sign = percent > 0 ? '+' : ''
  return `${sign}${Math.abs(percent) < 10 ? percent.toFixed(1) : percent.toFixed(0)}%`
}

function ModeGroup<T extends string>({ label, value, options, onChange }: {
  label: string
  value: T
  options: { value: T; label: string; title?: string }[]
  onChange: (value: T) => void
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs/5 text-gray-500 dark:text-gray-400">{label}</span>
      <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
        {options.map((option) => (
          <button
            key={option.value}
            onClick={() => onChange(option.value)}
            title={option.title}
            className={clsx(
              'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
              value === option.value
                ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
            )}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  )
}

export function GroupHeatmap({ runs, suiteTests, stepFilter, labelMode, baselineIdx, onBaselineChange, testNameFilter, onTestClick }: GroupHeatmapProps) {
  const [colorMode, setColorMode] = useState<ColorMode>('baseline')
  const [sortMode, setSortMode] = useState<SortMode>('order')
  const [threshold, setThreshold] = useState(DEFAULT_THRESHOLD)
  // The tooltip anchors to the hovered tile. Its own size is only known
  // once rendered, so it renders hidden and a layout effect measures it,
  // clamps it inside the viewport and shows it before the paint.
  const [tooltip, setTooltip] = useState<{ test: HeatmapTest; anchor: DOMRect } | null>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = tooltipRef.current
    if (!tooltip || !el) return
    const margin = 8
    const { width, height } = el.getBoundingClientRect()
    const { anchor } = tooltip
    const left = Math.max(margin, Math.min(window.innerWidth - width - margin, anchor.left + anchor.width / 2 - width / 2))
    // Above the tile, or below it when the top of the viewport is too close.
    let top = anchor.top - margin - height
    if (top < margin) top = anchor.bottom + margin
    el.style.left = `${left}px`
    el.style.top = `${top}px`
    el.style.visibility = 'visible'
  }, [tooltip])

  const tests = useMemo(() => {
    const suiteOrder = new Map<string, number>()
    suiteTests?.forEach((t, i) => suiteOrder.set(t.name, i + 1))

    const byName = new Map<string, HeatmapTest>()
    runs.forEach((run, gi) => {
      if (!run.result) return
      for (const [name, entry] of Object.entries(run.result.tests)) {
        if (testNameFilter && !testNameFilter(name)) continue
        let test = byName.get(name)
        if (!test) {
          test = {
            name,
            order: suiteOrder.get(name) ?? (parseInt(entry.dir, 10) || 0),
            values: new Array<number | undefined>(runs.length).fill(undefined),
            fails: new Array<boolean>(runs.length).fill(false),
            spread: 0,
          }
          byName.set(name, test)
        }
        const stats = getAggregatedStats(entry, stepFilter)
        test.values[gi] = calculateMGasPerSec(stats)
        test.fails[gi] = (stats?.fail ?? 0) > 0
      }
    })

    const list = [...byName.values()]
    for (const test of list) {
      const known = test.values.filter((v): v is number => v !== undefined)
      if (known.length >= 2) test.spread = Math.max(...known) / Math.min(...known) - 1
    }

    if (sortMode === 'spread') list.sort((a, b) => b.spread - a.spread || a.order - b.order)
    else list.sort((a, b) => a.order - b.order)

    return list
  }, [runs, suiteTests, stepFilter, testNameFilter, sortMode])

  // Tests per stanza, from the measured width of the grid.
  const gridRef = useRef<HTMLDivElement>(null)
  const [perRow, setPerRow] = useState(1)
  // The grid only exists while there are tests, so re-attach when they
  // come back after a filter emptied the list.
  const hasTests = tests.length > 0
  useEffect(() => {
    const grid = gridRef.current
    if (!grid) return
    const measure = () => {
      const count = Math.floor((grid.clientWidth - LABEL_PX + GAP_PX) / (TILE_PX + GAP_PX))
      setPerRow(Math.max(1, count))
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(grid)
    return () => observer.disconnect()
  }, [hasTests])

  const stanzas = useMemo(() => {
    const out: HeatmapTest[][] = []
    for (let i = 0; i < tests.length; i += perRow) out.push(tests.slice(i, i + perRow))
    return out
  }, [tests, perRow])

  const tileStyle = (test: HeatmapTest, gi: number) => {
    const value = test.values[gi]
    if (value === undefined) return NO_DATA_STYLE
    if (colorMode === 'mgas') return { backgroundColor: getColorByThreshold(value, threshold) }

    const base = test.values[baselineIdx]
    if (base === undefined) return NO_DATA_STYLE
    return { backgroundColor: THRESHOLD_COLORS[thresholdStep(value / base)] }
  }

  if (tests.length === 0) return null

  return (
    <div className="relative flex flex-col gap-3 rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <LayoutGrid className="size-4 text-gray-400 dark:text-gray-500" />
          <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Performance Heatmap</h3>
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">
            {tests.length} tests × {runs.length} groups
          </span>
        </div>
        <div className="flex items-center gap-2 text-xs/5">
          {runs.map((run) => {
            const slot = RUN_SLOTS[run.index]
            return (
              <span key={slot.label} className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 font-medium ${slot.badgeBgClass} ${slot.badgeTextClass}`}>
                <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-3.5 rounded-full object-cover" />
                {formatRunLabel(slot, run, labelMode)}
              </span>
            )
          })}
        </div>
      </div>

      {/* Controls */}
      <div className="flex flex-wrap items-center gap-4">
        <ModeGroup
          label="Color by:"
          value={colorMode}
          onChange={setColorMode}
          options={[
            { value: 'baseline', label: 'vs Baseline', title: 'Ratio of each group to the baseline group on the same test' },
            { value: 'mgas', label: 'MGas/s', title: `Color against the ${threshold} MGas/s threshold` },
          ]}
        />
        <ModeGroup
          label="Sort by:"
          value={sortMode}
          onChange={setSortMode}
          options={[
            { value: 'order', label: 'Test #' },
            { value: 'spread', label: 'Spread', title: 'Largest gap between the best and the worst group first' },
          ]}
        />
        {colorMode === 'baseline' && runs.length >= 2 && (
          <div className="flex items-center gap-2 text-xs/5 text-gray-500 dark:text-gray-400">
            <span>Baseline:</span>
            <select
              value={baselineIdx}
              onChange={(e) => onBaselineChange(Number(e.target.value))}
              className="rounded-xs border border-gray-300 bg-white px-1.5 py-0.5 text-xs/5 text-gray-700 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
            >
              {runs.map((run) => (
                <option key={run.index} value={run.index}>
                  {formatRunLabel(RUN_SLOTS[run.index], run, labelMode)}
                </option>
              ))}
            </select>
          </div>
        )}
        {colorMode === 'mgas' && (
          <div className="flex items-center gap-2 text-xs/5 text-gray-500 dark:text-gray-400">
            <span>Threshold:</span>
            <input
              type="number"
              min={MIN_THRESHOLD}
              max={MAX_THRESHOLD}
              value={threshold}
              onChange={(e) => setThreshold(Math.max(MIN_THRESHOLD, Math.min(MAX_THRESHOLD, Number(e.target.value) || DEFAULT_THRESHOLD)))}
              className="w-16 rounded-xs border border-gray-300 bg-white px-1.5 py-0.5 text-center text-xs/5 text-gray-700 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
            />
            <span>MGas/s</span>
          </div>
        )}
      </div>

      {/* Grid */}
      <div ref={gridRef} className="flex flex-col gap-3">
        {stanzas.map((stanza, si) => (
          <div key={si} className="flex flex-col" style={{ gap: GAP_PX }}>
            {runs.map((run, gi) => (
              <div key={run.index} className="flex items-center" style={{ gap: GAP_PX }}>
                <img
                  src={`/img/clients/${run.config.instance.client}.jpg`}
                  alt={run.config.instance.client}
                  title={formatRunLabel(RUN_SLOTS[run.index], run, labelMode)}
                  className="shrink-0 rounded-full object-cover"
                  style={{ width: TILE_PX, height: TILE_PX, marginRight: LABEL_PX - TILE_PX - GAP_PX }}
                />
                {stanza.map((test) => (
                  <button
                    key={test.name}
                    onClick={() => onTestClick?.(test.name)}
                    onMouseEnter={(e) => setTooltip({ test, anchor: e.currentTarget.getBoundingClientRect() })}
                    onMouseLeave={() => setTooltip(null)}
                    className={clsx(
                      'shrink-0 cursor-pointer rounded-xs transition-transform hover:scale-150 hover:ring-2 hover:ring-gray-500 dark:hover:ring-gray-300',
                      test.fails[gi] && 'ring-1 ring-inset ring-red-500',
                    )}
                    style={{ width: TILE_PX, height: TILE_PX, ...tileStyle(test, gi) }}
                  />
                ))}
              </div>
            ))}
          </div>
        ))}
      </div>

      {/* Legend */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs/5 text-gray-500 dark:text-gray-400">
        <ColorScaleLegend
          colors={THRESHOLD_COLORS}
          startLabel={colorMode === 'mgas' ? 'Fast' : 'Faster than baseline'}
          endLabel={colorMode === 'mgas' ? 'Slow' : 'Slower'}
          title={colorMode === 'mgas' ? 'MGas/s' : 'Ratio to the baseline'}
          stepRange={(step) => (colorMode === 'mgas' ? thresholdStepRange(step, threshold) : baselineStepRange(step))}
          note={colorMode === 'mgas' ? `Yellow is the ${threshold} MGas/s threshold.` : 'Yellow is parity with the baseline.'}
        />
        <span className="flex items-center">
          <span className="mr-1 inline-block size-3 rounded-xs" style={NO_DATA_STYLE} />
          No data
        </span>
        <span className="flex items-center">
          <span className="mr-1 inline-block size-3 rounded-xs ring-1 ring-inset ring-red-500" style={{ backgroundColor: THRESHOLD_COLORS[THRESHOLD_LIMIT_STEP] }} />
          Failed executions
        </span>
        <span>Click a tile to open the test.</span>
      </div>

      {tooltip && (
        <div
          ref={tooltipRef}
          className="pointer-events-none fixed z-50 max-w-[80vw] rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-700"
          style={{ left: 0, top: 0, visibility: 'hidden' }}
        >
          <div className="flex w-96 max-w-[80vw] flex-col gap-1">
            <TestName name={tooltip.test.name} variant="full" />
            <table className="text-left">
              <tbody>
                {runs.map((run, gi) => {
                  const slot = RUN_SLOTS[run.index]
                  const value = tooltip.test.values[gi]
                  const base = tooltip.test.values[baselineIdx]
                  return (
                    <tr key={run.index}>
                      <td className={clsx('pr-2 font-medium', slot.diffTextClass)}>
                        <span className="inline-flex items-center gap-1.5">
                          <img src={`/img/clients/${run.config.instance.client}.jpg`} alt="" className="size-3.5 rounded-full object-cover" />
                          {formatRunLabel(slot, run, labelMode)}
                        </span>
                      </td>
                      <td className="pr-2 text-right font-mono">{value === undefined ? '—' : `${value.toFixed(1)} MGas/s`}</td>
                      <td className="text-right font-mono text-gray-500 dark:text-gray-400">
                        {value !== undefined && base !== undefined && gi !== baselineIdx && formatRatio(value / base)}
                        {gi === baselineIdx && runs.length >= 2 && 'baseline'}
                      </td>
                      <td className="pl-2 text-red-600 dark:text-red-400">{tooltip.test.fails[gi] && 'failed'}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            {tooltip.test.spread > 0 && (
              <div className="text-gray-500 dark:text-gray-400">Spread: best is {formatRatio(1 + tooltip.test.spread)} over worst</div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
