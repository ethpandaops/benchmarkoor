import { useDeferredValue, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import clsx from 'clsx'
import { LayoutGrid } from 'lucide-react'
import type { AggregatedStats, SuiteTest } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { ColorScaleLegend } from '@/components/shared/ColorScaleLegend'
import { TestName } from '@/components/shared/TestName'
import {
  DEFAULT_SLOW_MS,
  DEFAULT_THRESHOLD,
  MAX_SLOW_MS,
  MAX_THRESHOLD,
  MIN_SLOW_MS,
  MIN_THRESHOLD,
  SLOW_COLOR,
  SLOW_STEP_MS,
  THRESHOLD_COLORS,
  THRESHOLD_LIMIT_STEP,
  THRESHOLD_RATIOS,
  durationStepRange,
  formatSlowMs,
  isSlowPayload,
  thresholdStepRange,
} from '@/utils/perfThreshold'
import { formatDuration } from '@/utils/format'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'
import { type HeatmapColorModel, baselineRatio, formatRatio, heatmapColor } from './heatmapColor'

// One row per group, one column per test. When the tests do not fit the
// width, the matrix wraps into stanzas: each stanza repeats the group
// rows for its slice of tests, so a column is the same test in every row.

export type SortMode = 'order' | 'spread' | 'avg'

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
  mgas: (number | undefined)[]
  /** Total engine_newPayload time per group in nanoseconds, in `runs` order. */
  durations: (number | undefined)[]
  /** Whether the group reported failed executions for this test. */
  fails: boolean[]
  /** Positions in `runs` with the highest MGas/s, when at least two groups have a value. Same rule as the ranking. */
  winners: Set<number>
  /** Best over worst group value of the active metric, minus one. Zero when fewer than two groups have a value. */
  spread: number
  /** Mean of the active metric over the groups that have a value, or undefined when no group has one. */
  avg: number | undefined
}

interface GroupHeatmapProps {
  /** One synthetic run per group. */
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
  labelMode: LabelMode
  /**
   * Position of the baseline in `runs`, the same convention as the other
   * compare widgets. Not a group index: the page drops a group from
   * `runs` when it has no config or no results, so `run.index` can differ
   * from the position.
   */
  baselineIdx: number
  onBaselineChange: (idx: number) => void
  /** See heatmapColor.ts. Controlled by the page so the test modal can match the tiles. */
  model: HeatmapColorModel
  onModelChange: (patch: Partial<HeatmapColorModel>) => void
  testNameFilter?: (name: string) => boolean
  onTestClick?: (testName: string) => void
  /** Group index (`run.index`) whose won tests stay bright while the rest dim, or null. */
  highlightGroupIdx?: number | null
  onHighlightChange?: (groupIdx: number | null) => void
  /** Test order of the matrix. The page owns it so the URL keeps it. */
  sortMode: SortMode
  onSortModeChange: (mode: SortMode) => void
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

// LimitControl is the slider plus number box of an absolute limit, the
// same control as the run page. The slider sweeps, the box takes an
// exact value, and a reset link appears off the default. `boxScale`
// divides the value for the box, e.g. 1000 shows milliseconds as seconds.
function LimitControl({ label, unit, value, min, max, step, defaultValue, boxScale = 1, accent = 'blue', onChange }: {
  label: string
  unit: string
  value: number
  min: number
  max: number
  step: number
  defaultValue: number
  boxScale?: number
  accent?: 'blue' | 'fuchsia'
  onChange: (value: number) => void
}) {
  const clamp = (v: number) => Math.max(min, Math.min(max, v || defaultValue))
  return (
    <div className="flex items-center gap-2 text-xs/5 text-gray-500 dark:text-gray-400">
      <span>{label}</span>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className={clsx(
          'h-1.5 w-24 cursor-pointer appearance-none rounded-full bg-gray-200 dark:bg-gray-700',
          accent === 'fuchsia' ? 'accent-fuchsia-500' : 'accent-blue-500',
        )}
      />
      <input
        type="number"
        min={min / boxScale}
        max={max / boxScale}
        step={step / boxScale}
        value={value / boxScale}
        onChange={(e) => onChange(clamp(Number(e.target.value) * boxScale))}
        className="w-16 rounded-xs border border-gray-300 bg-white px-1.5 py-0.5 text-center text-xs/5 text-gray-700 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
      />
      <span>{unit}</span>
      {value !== defaultValue && (
        <button onClick={() => onChange(defaultValue)} className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300">
          reset
        </button>
      )}
    </div>
  )
}

export function GroupHeatmap({
  runs,
  suiteTests,
  stepFilter,
  labelMode,
  baselineIdx,
  onBaselineChange,
  model,
  onModelChange,
  testNameFilter,
  onTestClick,
  highlightGroupIdx = null,
  onHighlightChange,
  sortMode,
  onSortModeChange,
}: GroupHeatmapProps) {
  const { metric, mode, threshold, slowMs } = model
  // The sliders fire on every tick and each tick recolours every tile.
  // The controls follow the hand; the tiles follow this deferred copy.
  const deferredModel = useDeferredValue(model)
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
            mgas: new Array<number | undefined>(runs.length).fill(undefined),
            durations: new Array<number | undefined>(runs.length).fill(undefined),
            fails: new Array<boolean>(runs.length).fill(false),
            winners: new Set(),
            spread: 0,
            avg: undefined,
          }
          byName.set(name, test)
        }
        const stats = getAggregatedStats(entry, stepFilter)
        test.mgas[gi] = calculateMGasPerSec(stats)
        test.durations[gi] = stats && stats.gas_used_time_total > 0 ? stats.gas_used_time_total : undefined
        test.fails[gi] = (stats?.fail ?? 0) > 0
      }
    })

    const list = [...byName.values()]
    for (const test of list) {
      const known = (metric === 'mgas' ? test.mgas : test.durations).filter((v): v is number => v !== undefined)
      if (known.length >= 2) test.spread = Math.max(...known) / Math.min(...known) - 1
      if (known.length > 0) test.avg = known.reduce((sum, v) => sum + v, 0) / known.length

      const knownMgas = test.mgas.filter((v): v is number => v !== undefined)
      if (knownMgas.length >= 2) {
        const best = Math.max(...knownMgas)
        test.mgas.forEach((v, gi) => {
          if (v === best) test.winners.add(gi)
        })
      }
    }

    if (sortMode === 'spread') {
      list.sort((a, b) => b.spread - a.spread || a.order - b.order)
    } else if (sortMode === 'avg') {
      // Worst first: the lowest average MGas/s, or the longest average
      // duration. A test with no value at all goes last.
      list.sort((a, b) => {
        if (a.avg === undefined || b.avg === undefined) {
          return Number(a.avg === undefined) - Number(b.avg === undefined) || a.order - b.order
        }
        return (metric === 'mgas' ? a.avg - b.avg : b.avg - a.avg) || a.order - b.order
      })
    } else {
      list.sort((a, b) => a.order - b.order)
    }

    return list
  }, [runs, suiteTests, stepFilter, testNameFilter, sortMode, metric])

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

  // Tests with at least one group above the slow-payload limit.
  const slowTestCount = useMemo(
    () => tests.filter((t) => t.durations.some((d) => d !== undefined && isSlowPayload(d, slowMs))).length,
    [tests, slowMs],
  )

  const stanzas = useMemo(() => {
    const out: HeatmapTest[][] = []
    for (let i = 0; i < tests.length; i += perRow) out.push(tests.slice(i, i + perRow))
    return out
  }, [tests, perRow])

  // The highlighted group's position in `runs`, or -1 when none or when
  // the group has no result.
  const highlightPos = highlightGroupIdx === null ? -1 : runs.findIndex((r) => r.index === highlightGroupIdx)
  const highlightRun = highlightPos >= 0 ? runs[highlightPos] : undefined
  const highlightedTestCount = useMemo(
    () => (highlightPos >= 0 ? tests.filter((t) => t.winners.has(highlightPos)).length : 0),
    [tests, highlightPos],
  )

  const valuesOf = (test: HeatmapTest, m: HeatmapColorModel['metric']) => (m === 'mgas' ? test.mgas : test.durations)
  const tileStyle = (test: HeatmapTest, gi: number): React.CSSProperties => {
    const values = valuesOf(test, deferredModel.metric)
    const color = heatmapColor(values[gi], values[baselineIdx], deferredModel)
    const style: React.CSSProperties = color ? { backgroundColor: color } : { ...NO_DATA_STYLE }
    // With a highlighted group, every column it does not win dims, the
    // same treatment as a filtered-out tile on the run page.
    if (highlightPos >= 0 && !test.winners.has(highlightPos)) style.opacity = 0.15
    // A slow payload gets an inset outline in every mode, like the run
    // page. It sits inside the tile, so it stays readable next to the red
    // failure ring.
    const duration = test.durations[gi]
    if (duration !== undefined && isSlowPayload(duration, deferredModel.slowMs)) {
      style.outline = `2px solid ${SLOW_COLOR}`
      style.outlineOffset = '-2px'
    }
    return style
  }
  const formatValue = (value: number) => (metric === 'mgas' ? `${value.toFixed(1)} MGas/s` : formatDuration(value))

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
          {slowTestCount > 0 && (
            <span
              className="rounded-xs px-1.5 py-0.5 text-xs/5 font-medium"
              style={{ backgroundColor: `${SLOW_COLOR}26`, color: SLOW_COLOR }}
              title={`Tests with a group whose payload time is above ${formatSlowMs(slowMs)}`}
            >
              {slowTestCount} slow
            </span>
          )}
          {highlightRun && (
            <span className={clsx('inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-xs/5 font-medium', RUN_SLOTS[highlightRun.index].badgeBgClass, RUN_SLOTS[highlightRun.index].badgeTextClass)}>
              <img src={`/img/clients/${highlightRun.config.instance.client}.jpg`} alt="" className="size-3.5 rounded-full object-cover" />
              {highlightedTestCount} tests won by {highlightRun.config.instance.client}
              {onHighlightChange && (
                <button type="button" onClick={() => onHighlightChange(null)} className="ml-0.5 opacity-70 hover:opacity-100" title="Clear the highlight">
                  ×
                </button>
              )}
            </span>
          )}
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
          label="Metric:"
          value={metric}
          onChange={(m) => onModelChange({ metric: m })}
          options={[
            { value: 'mgas', label: 'MGas/s' },
            { value: 'duration', label: 'Duration', title: 'Total engine_newPayload time of the test' },
          ]}
        />
        <ModeGroup
          label="Color by:"
          value={mode}
          onChange={(m) => onModelChange({ mode: m })}
          options={[
            {
              value: 'absolute',
              label: metric === 'mgas' ? 'Threshold' : 'Slow limit',
              title: metric === 'mgas' ? `Color against the ${threshold} MGas/s threshold` : `Color against the ${formatSlowMs(slowMs)} slow-payload limit`,
            },
            { value: 'baseline', label: 'vs Baseline', title: 'Ratio of each group to the baseline group on the same test' },
          ]}
        />
        <ModeGroup
          label="Sort by:"
          value={sortMode}
          onChange={onSortModeChange}
          options={[
            { value: 'order', label: 'Test #' },
            { value: 'spread', label: 'Spread', title: 'Largest gap between the best and the worst group first' },
            {
              value: 'avg',
              label: 'Avg',
              title: metric === 'mgas' ? 'Lowest average MGas/s over the groups first' : 'Longest average duration over the groups first',
            },
          ]}
        />
        {mode === 'baseline' && runs.length >= 2 && (
          <div className="flex items-center gap-2 text-xs/5 text-gray-500 dark:text-gray-400">
            <span>Baseline:</span>
            <select
              value={baselineIdx}
              onChange={(e) => onBaselineChange(Number(e.target.value))}
              className="rounded-xs border border-gray-300 bg-white px-1.5 py-0.5 text-xs/5 text-gray-700 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
            >
              {runs.map((run, i) => (
                <option key={run.index} value={i}>
                  {formatRunLabel(RUN_SLOTS[run.index], run, labelMode)}
                </option>
              ))}
            </select>
          </div>
        )}
        {mode === 'absolute' && metric === 'mgas' && (
          <LimitControl
            label="Threshold:"
            unit="MGas/s"
            value={threshold}
            min={MIN_THRESHOLD}
            max={MAX_THRESHOLD}
            step={1}
            defaultValue={DEFAULT_THRESHOLD}
            onChange={(v) => onModelChange({ threshold: v })}
          />
        )}
        {/* Always shown: the limit marks slow tiles in every mode, and colours them in duration mode. */}
        <LimitControl
          label="Slow limit:"
          unit="s"
          value={slowMs}
          min={MIN_SLOW_MS}
          max={MAX_SLOW_MS}
          step={SLOW_STEP_MS}
          defaultValue={DEFAULT_SLOW_MS}
          boxScale={1000}
          accent="fuchsia"
          onChange={(v) => onModelChange({ slowMs: v })}
        />
      </div>

      {/* Grid */}
      <div ref={gridRef} className="flex flex-col gap-3">
        {stanzas.map((stanza, si) => (
          // No vertical gap: the tiles of one test stack into one bar, so a
          // column reads as one test across the groups.
          <div key={si} className="flex flex-col">
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
                      'relative shrink-0 cursor-pointer transition-transform hover:z-10 hover:scale-150 hover:ring-2 hover:ring-gray-500 dark:hover:ring-gray-300',
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
          startLabel={mode === 'absolute' ? 'Fast' : 'Faster than baseline'}
          endLabel={mode === 'absolute' ? 'Slow' : 'Slower'}
          title={mode === 'baseline' ? 'Ratio to the baseline' : metric === 'mgas' ? 'MGas/s' : 'Payload time'}
          stepRange={(step) =>
            mode === 'baseline'
              ? baselineStepRange(step)
              : metric === 'mgas'
                ? thresholdStepRange(step, threshold)
                : durationStepRange(step, slowMs)}
          note={
            mode === 'baseline'
              ? 'Yellow is parity with the baseline.'
              : metric === 'mgas'
                ? `Yellow is the ${threshold} MGas/s threshold.`
                : `Yellow is the ${formatSlowMs(slowMs)} slow-payload limit.`
          }
        />
        <span className="flex items-center">
          <span className="mr-1 inline-block size-3 rounded-xs" style={NO_DATA_STYLE} />
          No data
        </span>
        <span className="flex items-center">
          <span className="mr-1 inline-block size-3 rounded-xs ring-1 ring-inset ring-red-500" style={{ backgroundColor: THRESHOLD_COLORS[THRESHOLD_LIMIT_STEP] }} />
          Failed executions
        </span>
        <span className="flex items-center" title={`Payload time above ${formatSlowMs(slowMs)}`}>
          <span
            className="mr-1 inline-block size-3 rounded-xs"
            style={{ backgroundColor: THRESHOLD_COLORS[THRESHOLD_LIMIT_STEP], outline: `2px solid ${SLOW_COLOR}`, outlineOffset: '-2px' }}
          />
          Slow payload (&gt;{formatSlowMs(slowMs)})
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
            {tooltip.test.order > 0 && <div className="font-medium">Test #{tooltip.test.order}</div>}
            <TestName name={tooltip.test.name} variant="full" />
            <table className="text-left">
              <tbody>
                {runs.map((run, gi) => {
                  const slot = RUN_SLOTS[run.index]
                  const values = valuesOf(tooltip.test, metric)
                  const value = values[gi]
                  const base = values[baselineIdx]
                  const other = valuesOf(tooltip.test, metric === 'mgas' ? 'duration' : 'mgas')[gi]
                  return (
                    <tr key={run.index}>
                      <td className={clsx('pr-2 font-medium', slot.diffTextClass)}>
                        <span className="inline-flex items-center gap-1.5">
                          <img src={`/img/clients/${run.config.instance.client}.jpg`} alt="" className="size-3.5 rounded-full object-cover" />
                          {formatRunLabel(slot, run, labelMode)}
                        </span>
                      </td>
                      <td className="pr-2 text-right font-mono">{value === undefined ? '—' : formatValue(value)}</td>
                      <td className="pr-2 text-right font-mono text-gray-400 dark:text-gray-500">
                        {other !== undefined && (metric === 'mgas' ? formatDuration(other) : `${other.toFixed(1)} MGas/s`)}
                      </td>
                      <td className="text-right font-mono text-gray-500 dark:text-gray-400">
                        {value !== undefined && base !== undefined && gi !== baselineIdx && formatRatio(baselineRatio(value, base, metric))}
                        {gi === baselineIdx && runs.length >= 2 && 'baseline'}
                      </td>
                      <td className="pl-2">
                        {tooltip.test.durations[gi] !== undefined && isSlowPayload(tooltip.test.durations[gi], slowMs) && (
                          <span style={{ color: SLOW_COLOR }}>slow</span>
                        )}
                        {tooltip.test.fails[gi] && <span className="ml-1 text-red-600 dark:text-red-400">failed</span>}
                      </td>
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
