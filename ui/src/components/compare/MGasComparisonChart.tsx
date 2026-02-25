import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { RunResult, SuiteTest, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { type CompareRun, RUN_SLOTS } from './constants'

interface MGasComparisonChartProps {
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
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

interface MGasDataPoint {
  testIndex: number
  testName: string
  mgas: number
}

function buildMGasData(
  result: RunResult,
  suiteTests: SuiteTest[] | undefined,
  stepFilter: StepTypeOption[],
): MGasDataPoint[] {
  const suiteOrder = new Map<string, number>()
  if (suiteTests) {
    suiteTests.forEach((t, i) => suiteOrder.set(t.name, i + 1))
  }

  const entries: { name: string; order: number; mgas: number }[] = []
  for (const [name, entry] of Object.entries(result.tests)) {
    const stats = getAggregatedStats(entry, stepFilter)
    const mgas = calculateMGasPerSec(stats)
    if (mgas === undefined) continue
    const order = suiteOrder.get(name) ?? (parseInt(entry.dir, 10) || 0)
    entries.push({ name, order, mgas })
  }

  entries.sort((a, b) => a.order - b.order)
  return entries.map((e, i) => ({ testIndex: i + 1, testName: e.name, mgas: e.mgas }))
}

export function MGasComparisonChart({ runs, suiteTests, stepFilter }: MGasComparisonChartProps) {
  const isDark = useDarkMode()
  const [zoomRange, setZoomRange] = useState({ start: 0, end: 100 })
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
      prevZoomRef.current = { start, end }
      setZoomRange({ start, end })
    }
  }, [])

  const onEvents = useMemo(() => ({ datazoom: handleZoom }), [handleZoom])

  const pointsPerRun = useMemo(
    () => runs.map((r) => r.result ? buildMGasData(r.result, suiteTests, stepFilter) : []),
    [runs, suiteTests, stepFilter],
  )

  const option = useMemo(() => {
    const textColor = isDark ? '#ffffff' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'
    const maxLen = Math.max(...pointsPerRun.map((p) => p.length))

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
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        extraCssText: 'max-width: 300px; white-space: normal;',
        formatter: (
          params: Array<{ seriesName: string; color: string; value: [number, number, string] }>,
        ) => {
          if (!params.length) return ''
          const testIndex = params[0].value[0]
          let content = `<strong>Test #${testIndex}</strong><br/>`
          params.forEach((p) => {
            const value = p.value[1]
            const testName = p.value[2]
            content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${value.toFixed(2)} MGas/s`
            if (testName) content += `<br/><span style="font-size: 10px; color: ${isDark ? '#9ca3af' : '#6b7280'};">${testName}</span>`
            content += '<br/>'
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
          formatter: (value: number) => `#${value}`,
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
          formatter: (value: number) => `${value.toFixed(0)}`,
        },
        axisLine: { show: true, lineStyle: { color: axisLineColor } },
        axisTick: { show: true, lineStyle: { color: axisLineColor } },
        splitLine: { lineStyle: { color: splitLineColor } },
        name: 'MGas/s',
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
          labelFormatter: (value: number) => `#${Math.round(value)}`,
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
      series: runs.map((_run, i) => {
        const slot = RUN_SLOTS[i]
        const points = pointsPerRun[i]
        return {
          name: `Run ${slot.label}`,
          type: 'line' as const,
          smooth: maxLen <= 100,
          showSymbol: maxLen <= 100,
          symbolSize: 4,
          lineStyle: { width: 2 },
          data: points.map((d) => [d.testIndex, d.mgas, d.testName]),
          itemStyle: { color: slot.color },
          areaStyle: { opacity: 0.08, color: slot.color },
        }
      }),
    }
  }, [pointsPerRun, runs, isDark, zoomRange])

  if (pointsPerRun.every((p) => p.length === 0)) return null

  return (
    <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">MGas/s per Test</h3>
        <div className="flex items-center gap-3 text-xs/5">
          {runs.map((run) => {
            const slot = RUN_SLOTS[run.index]
            return (
              <span key={slot.label} className="flex items-center gap-1">
                <span className={`inline-block size-2.5 rounded-full ${slot.bgDotClass}`} />
                <span className="text-gray-500 dark:text-gray-400">Run {slot.label}</span>
              </span>
            )
          })}
        </div>
      </div>
      <ReactECharts
        option={option}
        style={{ height: '300px', width: '100%' }}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
      />
    </div>
  )
}
