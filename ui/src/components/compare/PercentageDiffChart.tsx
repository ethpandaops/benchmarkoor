import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { ArrowRightLeft } from 'lucide-react'
import type { SuiteTest, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { type ChartType, type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'
import type { ZoomRange } from './MGasComparisonChart'

interface PercentageDiffChartProps {
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
  baselineIdx: number
  onBaselineChange: (idx: number) => void
  labelMode: LabelMode
  diffFilter: 'all' | 'faster' | 'slower'
  onDiffFilterChange: (val: 'all' | 'faster' | 'slower') => void
  testNameFilter?: (name: string) => boolean
  zoomRange?: ZoomRange
  onZoomChange?: (range: ZoomRange) => void
  chartType?: ChartType
}

function calculateMGasPerSec(stats: AggregatedStats | undefined): number | undefined {
  if (!stats || stats.gas_used_time_total <= 0 || stats.gas_used_total <= 0) return undefined
  return (stats.gas_used_total * 1000) / stats.gas_used_time_total
}

function useDarkMode() {
  const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains('dark'))

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  return isDark
}

interface TestDiffPoint {
  testIndex: number
  testOrder: number
  testName: string
  baselineValue: number
  diffs: (number | null)[] // % diff per non-baseline run
  values: (number | null)[] // absolute MGas/s per non-baseline run
}

function buildDiffData(
  runs: CompareRun[],
  baselineIdx: number,
  suiteTests: SuiteTest[] | undefined,
  stepFilter: StepTypeOption[],
  nameFilter?: (name: string) => boolean,
): TestDiffPoint[] {
  const suiteOrder = new Map<string, number>()
  if (suiteTests) {
    suiteTests.forEach((t, i) => suiteOrder.set(t.name, i + 1))
  }

  const baselineResult = runs[baselineIdx].result
  if (!baselineResult) return []

  // Collect all test names from all runs
  const allTestNames = new Set<string>()
  for (const run of runs) {
    if (run.result) {
      for (const name of Object.keys(run.result.tests)) allTestNames.add(name)
    }
  }

  const entries: { name: string; order: number; baselineValue: number; diffs: (number | null)[]; values: (number | null)[] }[] = []

  for (const name of allTestNames) {
    if (nameFilter && !nameFilter(name)) continue
    const baseEntry = baselineResult.tests[name]
    const baseStats = baseEntry ? getAggregatedStats(baseEntry, stepFilter) : undefined
    const baseMGas = calculateMGasPerSec(baseStats)
    if (baseMGas === undefined || baseMGas <= 0) continue

    const order = suiteOrder.get(name) ?? (baseEntry ? parseInt(baseEntry.dir, 10) || 0 : 0)
    const diffs: (number | null)[] = []
    const values: (number | null)[] = []

    for (let i = 0; i < runs.length; i++) {
      if (i === baselineIdx) continue
      const entry = runs[i].result?.tests[name]
      const stats = entry ? getAggregatedStats(entry, stepFilter) : undefined
      const mgas = calculateMGasPerSec(stats)
      if (mgas === undefined) {
        diffs.push(null)
        values.push(null)
      } else {
        diffs.push(((mgas - baseMGas) / baseMGas) * 100)
        values.push(mgas)
      }
    }

    entries.push({ name, order, baselineValue: baseMGas, diffs, values })
  }

  entries.sort((a, b) => a.order - b.order)
  return entries.map((e, i) => ({
    testIndex: i + 1,
    testOrder: e.order,
    testName: e.name,
    baselineValue: e.baselineValue,
    diffs: e.diffs,
    values: e.values,
  }))
}

export function PercentageDiffChart({ runs, suiteTests, stepFilter, baselineIdx, onBaselineChange, labelMode, diffFilter, onDiffFilterChange, testNameFilter, zoomRange: externalZoom, onZoomChange, chartType = 'line' }: PercentageDiffChartProps) {
  const isDark = useDarkMode()
  const [internalZoom, setInternalZoom] = useState({ start: 0, end: 100 })
  const zoomRange = externalZoom ?? internalZoom
  const prevZoomRef = useRef(zoomRange)

  const handleZoom = useCallback((params: { start?: number; end?: number; batch?: Array<{ start: number; end: number }> }) => {
    let start: number | undefined
    let end: number | undefined
    if (params.batch && params.batch.length > 0) {
      start = params.batch[0].start
      end = params.batch[0].end
    } else {
      start = params.start
      end = params.end
    }
    if (start !== undefined && end !== undefined && (prevZoomRef.current.start !== start || prevZoomRef.current.end !== end)) {
      const newRange = { start, end }
      prevZoomRef.current = newRange
      setInternalZoom(newRange)
      onZoomChange?.(newRange)
    }
  }, [onZoomChange])

  const onEvents = useMemo(() => ({ datazoom: handleZoom }), [handleZoom])

  // Build the non-baseline run indices for series mapping
  const otherRunIndices = useMemo(
    () => runs.map((_, i) => i).filter((i) => i !== baselineIdx),
    [runs, baselineIdx],
  )

  const diffData = useMemo(
    () => buildDiffData(runs, baselineIdx, suiteTests, stepFilter, testNameFilter),
    [runs, baselineIdx, suiteTests, stepFilter, testNameFilter],
  )

  const option = useMemo(() => {
    const textColor = isDark ? '#ffffff' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'
    const maxLen = Math.max(diffData.length, 1)
    const indexToOrder = new Map(diffData.map((d) => [d.testIndex, d.testOrder]))

    return {
      backgroundColor: 'transparent',
      animation: maxLen <= 100,
      textStyle: { color: textColor },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '50',
        top: '15%',
        containLabel: true,
      },
      tooltip: {
        trigger: 'axis' as const,
        appendToBody: true,
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        extraCssText: 'max-width: 350px; white-space: normal;',
        formatter: (
          // value: [testIndex, %diff, testName, baselineMGas, absoluteMGas]
          params: Array<{ seriesName: string; color: string; value: [number, number, string, number, number, number] }>,
        ) => {
          if (!params.length) return ''
          const testName = params[0].value[2]
          const baseValue = params[0].value[3]
          const testOrder = params[0].value[5]
          const baseSlot = RUN_SLOTS[baselineIdx]
          const baseClient = runs[baselineIdx].config.instance.client
          const baseImg = `<img src="/img/clients/${baseClient}.jpg" style="display:inline-block;width:14px;height:14px;border-radius:50%;object-fit:cover;vertical-align:middle;margin-right:4px;" />`

          let content = `<strong>Test #${testOrder}</strong>`
          if (testName) content += `<br/><span style="font-size: 10px; color: ${isDark ? '#9ca3af' : '#6b7280'};">${testName}</span>`
          content += `<br/>${baseImg}<span style="font-size: 11px;">Baseline ${baseSlot.label}: ${baseValue.toFixed(2)} MGas/s</span><br/>`

          params.forEach((p) => {
            const diff = p.value[1]
            const absMGas = p.value[4]
            const sign = diff >= 0 ? '+' : ''
            const color = diff >= 0 ? '#10b981' : '#ef4444'
            const label = diff >= 0 ? 'faster' : 'slower'
            const seriesRunIdx = otherRunIndices.find((ri) => `vs ${formatRunLabel(RUN_SLOTS[ri], runs[ri], labelMode)}` === p.seriesName)
            const client = seriesRunIdx !== undefined ? runs[seriesRunIdx].config.instance.client : undefined
            const clientImg = client ? `<img src="/img/clients/${client}.jpg" style="display:inline-block;width:14px;height:14px;border-radius:50%;object-fit:cover;vertical-align:middle;margin-right:4px;" />` : ''
            content += `${clientImg}<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;vertical-align:middle;"></span>${p.seriesName}: ${absMGas.toFixed(2)} MGas/s <span style="color:${color};font-weight:600;">(${sign}${diff.toFixed(1)}% ${label})</span><br/>`
          })
          return content
        },
      },
      xAxis: {
        type: 'value' as const,
        min: 1,
        max: maxLen,
        minInterval: 1,
        axisLabel: {
          color: textColor,
          fontSize: 11,
          formatter: (value: number) => `#${indexToOrder.get(value) ?? value}`,
        },
        axisLine: { show: true, lineStyle: { color: axisLineColor } },
        axisTick: { show: true, lineStyle: { color: axisLineColor } },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value' as const,
        axisLabel: {
          color: textColor,
          fontSize: 11,
          formatter: (value: number) => `${value > 0 ? '+' : ''}${value.toFixed(0)}%`,
        },
        axisLine: { show: true, lineStyle: { color: axisLineColor } },
        axisTick: { show: true, lineStyle: { color: axisLineColor } },
        splitLine: { lineStyle: { color: splitLineColor } },
        name: '% Difference',
        nameTextStyle: { color: textColor, fontSize: 11 },
      },
      legend: {
        bottom: 25,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      dataZoom: [
        {
          type: 'slider' as const,
          xAxisIndex: 0,
          start: zoomRange.start,
          end: zoomRange.end,
          height: 20,
          bottom: 5,
          borderColor: axisLineColor,
          fillerColor: isDark ? 'rgba(139, 92, 246, 0.3)' : 'rgba(139, 92, 246, 0.1)',
          backgroundColor: isDark ? '#374151' : '#f3f4f6',
          textStyle: { color: textColor },
          labelFormatter: (value: number) => `#${indexToOrder.get(Math.round(value)) ?? Math.round(value)}`,
        },
        {
          type: 'inside' as const,
          xAxisIndex: 0,
          start: zoomRange.start,
          end: zoomRange.end,
          zoomOnMouseWheel: true,
          moveOnMouseMove: true,
          moveOnMouseWheel: false,
        },
      ],
      // Zero reference line
      markLine: undefined,
      series: [
        // Invisible reference series just for the zero line
        {
          name: '_zero',
          type: 'line' as const,
          data: [],
          markLine: {
            silent: true,
            symbol: 'none',
            lineStyle: {
              color: isDark ? '#6b7280' : '#9ca3af',
              type: 'dashed' as const,
              width: 1,
            },
            label: { show: false },
            data: [{ yAxis: 0 }],
          },
        },
        ...otherRunIndices.map((runIdx, seriesIdx) => {
          const slot = RUN_SLOTS[runIdx]
          const data = diffData.map((d) => {
            const diff = d.diffs[seriesIdx]
            const absMGas = d.values[seriesIdx]
            if (diff === null || absMGas === null) return null
            if (diffFilter === 'faster' && diff < 0) return null
            if (diffFilter === 'slower' && diff > 0) return null
            return {
              value: [d.testIndex, diff, d.testName, d.baselineValue, absMGas, d.testOrder],
            }
          }).filter(Boolean)
          const base = {
            name: `vs ${formatRunLabel(slot, runs[runIdx], labelMode)}`,
            data,
            itemStyle: { color: slot.color },
          }
          if (chartType === 'bar') {
            return { ...base, type: 'bar' as const, barMaxWidth: 6 }
          }
          if (chartType === 'dot') {
            return { ...base, type: 'scatter' as const, symbolSize: 4 }
          }
          return {
            ...base,
            type: 'line' as const,
            smooth: diffData.length <= 100,
            showSymbol: diffData.length <= 100,
            symbolSize: 4,
            lineStyle: { width: 2 },
            areaStyle: { opacity: 0.08, color: slot.color },
          }
        }),
      ],
    }
  }, [diffData, runs, otherRunIndices, baselineIdx, isDark, zoomRange, labelMode, diffFilter, chartType])

  if (runs.every((r) => r.result === null)) return null

  return (
    <div className="rounded-xs bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <ArrowRightLeft className="size-4 text-gray-400 dark:text-gray-500" />
          <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">% Difference vs Baseline</h3>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5 text-xs/5 text-gray-500 dark:text-gray-400">
            <span>Baseline:</span>
            <div className="flex gap-1">
              {runs.map((run, i) => {
                const slot = RUN_SLOTS[run.index]
                return (
                  <button
                    key={slot.label}
                    onClick={() => onBaselineChange(i)}
                    className={`inline-flex items-center gap-1 rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors ${
                      baselineIdx === i
                        ? `${slot.badgeBgClass} ${slot.badgeTextClass} ring-1 ring-current`
                        : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
                    }`}
                  >
                    <img
                      src={`/img/clients/${run.config.instance.client}.jpg`}
                      alt={run.config.instance.client}
                      className="size-3.5 rounded-full object-cover"
                    />
                    {formatRunLabel(slot, run, labelMode)}
                  </button>
                )
              })}
            </div>
          </div>
          <div className="flex items-center gap-2 text-xs/5">
            {otherRunIndices.map((ri) => {
              const slot = RUN_SLOTS[ri]
              const run = runs[ri]
              return (
                <span
                  key={slot.label}
                  className={`inline-flex items-center gap-1.5 rounded-xs px-2 py-0.5 font-medium ${slot.badgeBgClass} ${slot.badgeTextClass}`}
                >
                  <img
                    src={`/img/clients/${run.config.instance.client}.jpg`}
                    alt={run.config.instance.client}
                    className="size-3.5 rounded-full object-cover"
                  />
                  vs {formatRunLabel(slot, run, labelMode)}
                </span>
              )
            })}
          </div>
        </div>
      </div>
      <div className="mb-2 flex items-center justify-between">
        <p className="text-xs/5 text-gray-400 dark:text-gray-500">
          Positive = faster than baseline, Negative = slower
        </p>
        <div className="flex gap-1">
          {([['all', 'Show All'], ['faster', 'Faster'], ['slower', 'Slower']] as const).map(([value, label]) => (
            <button
              key={value}
              onClick={() => onDiffFilterChange(value)}
              className={`rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors ${
                diffFilter === value
                  ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                  : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <ReactECharts
        option={option}
        style={{ height: '300px', width: '100%' }}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
      />
      <div className="mt-3 border-t border-gray-200 pt-3 dark:border-gray-700">
        <table className="w-full text-xs/5">
          <thead>
            <tr className="text-gray-500 dark:text-gray-400">
              <th className="pb-1 text-left font-medium">Run</th>
              <th className="pb-1 text-right font-medium">Faster</th>
              <th className="pb-1 text-right font-medium">Avg %</th>
              <th className="pb-1 text-right font-medium">P95 %</th>
              <th className="pb-1 pr-3 text-right font-medium">P99 %</th>
              <th className="border-l border-gray-200 pb-1 pl-3 text-right font-medium dark:border-gray-600">Slower</th>
              <th className="pb-1 text-right font-medium">Avg %</th>
              <th className="pb-1 text-right font-medium">P95 %</th>
              <th className="pb-1 pr-3 text-right font-medium">P99 %</th>
              <th className="border-l border-gray-200 pb-1 pl-3 text-right font-medium dark:border-gray-600">Equal</th>
              <th className="pb-1 text-right font-medium">Total</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
            {otherRunIndices.map((ri, seriesIdx) => {
              const slot = RUN_SLOTS[ri]
              const run = runs[ri]
              const fasterPcts: number[] = []
              const slowerPcts: number[] = []
              let equal = 0
              for (const d of diffData) {
                const diff = d.diffs[seriesIdx]
                if (diff === null) continue
                if (diff > 0) fasterPcts.push(diff)
                else if (diff < 0) slowerPcts.push(Math.abs(diff))
                else equal++
              }
              const total = fasterPcts.length + slowerPcts.length + equal
              const avg = (arr: number[]) => arr.length > 0 ? arr.reduce((a, b) => a + b, 0) / arr.length : 0
              const percentile = (arr: number[], p: number) => {
                if (arr.length === 0) return 0
                const sorted = [...arr].sort((a, b) => a - b)
                const idx = Math.ceil((p / 100) * sorted.length) - 1
                return sorted[Math.max(0, idx)]
              }
              return (
                <tr key={slot.label}>
                  <td className="py-1">
                    <span className="inline-flex items-center gap-1.5 font-medium" style={{ color: slot.color }}>
                      <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-3.5 rounded-full object-cover" />
                      {formatRunLabel(slot, run, labelMode)}
                    </span>
                  </td>
                  <td className="py-1 text-right font-medium text-green-600 dark:text-green-400">{fasterPcts.length}</td>
                  <td className="py-1 text-right text-green-600 dark:text-green-400">{fasterPcts.length > 0 ? `+${avg(fasterPcts).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 text-right text-green-600 dark:text-green-400">{fasterPcts.length > 0 ? `+${percentile(fasterPcts, 95).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 pr-3 text-right text-green-600 dark:text-green-400">{fasterPcts.length > 0 ? `+${percentile(fasterPcts, 99).toFixed(1)}%` : '-'}</td>
                  <td className="border-l border-gray-200 py-1 pl-3 text-right font-medium text-red-600 dark:border-gray-600 dark:text-red-400">{slowerPcts.length}</td>
                  <td className="py-1 text-right text-red-600 dark:text-red-400">{slowerPcts.length > 0 ? `-${avg(slowerPcts).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 text-right text-red-600 dark:text-red-400">{slowerPcts.length > 0 ? `-${percentile(slowerPcts, 95).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 pr-3 text-right text-red-600 dark:text-red-400">{slowerPcts.length > 0 ? `-${percentile(slowerPcts, 99).toFixed(1)}%` : '-'}</td>
                  <td className="border-l border-gray-200 py-1 pl-3 text-right text-gray-400 dark:border-gray-600 dark:text-gray-500">{equal}</td>
                  <td className="py-1 text-right text-gray-500 dark:text-gray-400">{total}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
