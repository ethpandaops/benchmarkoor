import { useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import clsx from 'clsx'
import type { TestEntry } from '@/api/types'
import { formatBytes } from '@/utils/format'

interface ResourceUsageChartsProps {
  tests: Record<string, TestEntry>
  isDark?: boolean
}

interface ResourceDataPoint {
  testIndex: number
  testName: string
  cpuUsec: number
  diskRead: number
  diskWrite: number
  diskReadOps: number
  diskWriteOps: number
}

type MetricType = 'cpu' | 'diskBytes' | 'diskOps'

function formatMicroseconds(usec: number): string {
  if (usec < 1000) {
    return `${usec.toFixed(0)} µs`
  }
  if (usec < 1_000_000) {
    return `${(usec / 1000).toFixed(1)} ms`
  }
  return `${(usec / 1_000_000).toFixed(2)} s`
}

function formatOps(ops: number): string {
  if (ops < 1000) {
    return `${ops.toFixed(0)}`
  }
  if (ops < 1_000_000) {
    return `${(ops / 1000).toFixed(1)}K`
  }
  return `${(ops / 1_000_000).toFixed(1)}M`
}

export function ResourceUsageCharts({ tests, isDark = false }: ResourceUsageChartsProps) {
  const [selectedMetric, setSelectedMetric] = useState<MetricType>('cpu')

  const { dataPoints, hasResourceData } = useMemo(() => {
    const points: ResourceDataPoint[] = []
    let hasData = false

    // Sort tests by their directory order (numeric prefix)
    const sortedTests = Object.entries(tests).sort(([, a], [, b]) => {
      const aNum = parseInt(a.dir, 10) || 0
      const bNum = parseInt(b.dir, 10) || 0
      return aNum - bNum
    })

    sortedTests.forEach(([testName, test], index) => {
      const agg = test.aggregated
      if (
        agg.cpu_usec_total !== undefined ||
        agg.disk_read_total !== undefined ||
        agg.disk_write_total !== undefined
      ) {
        hasData = true
        points.push({
          testIndex: index + 1,
          testName,
          cpuUsec: agg.cpu_usec_total ?? 0,
          diskRead: agg.disk_read_total ?? 0,
          diskWrite: agg.disk_write_total ?? 0,
          diskReadOps: agg.disk_read_iops_total ?? 0,
          diskWriteOps: agg.disk_write_iops_total ?? 0,
        })
      }
    })

    return { dataPoints: points, hasResourceData: hasData }
  }, [tests])

  const option = useMemo(() => {
    const textColor = isDark ? '#e5e7eb' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'

    let series: object[] = []
    let yAxisConfig: object = {}

    if (selectedMetric === 'cpu') {
      series = [
        {
          name: 'CPU Usage',
          type: 'bar',
          data: dataPoints.map((d) => [d.testIndex, d.cpuUsec, d.testName]),
          itemStyle: {
            color: '#8b5cf6',
          },
          barMaxWidth: 20,
        },
      ]
      yAxisConfig = {
        type: 'value',
        name: 'CPU Time',
        nameTextStyle: { color: textColor },
        axisLabel: {
          color: textColor,
          formatter: (value: number) => formatMicroseconds(value),
        },
      }
    } else if (selectedMetric === 'diskBytes') {
      series = [
        {
          name: 'Disk Read',
          type: 'bar',
          data: dataPoints.map((d) => [d.testIndex, d.diskRead, d.testName]),
          itemStyle: {
            color: '#3b82f6',
          },
          barMaxWidth: 20,
        },
        {
          name: 'Disk Write',
          type: 'bar',
          data: dataPoints.map((d) => [d.testIndex, d.diskWrite, d.testName]),
          itemStyle: {
            color: '#f97316',
          },
          barMaxWidth: 20,
        },
      ]
      yAxisConfig = {
        type: 'value',
        name: 'Bytes',
        nameTextStyle: { color: textColor },
        axisLabel: {
          color: textColor,
          formatter: (value: number) => formatBytes(value),
        },
      }
    } else {
      series = [
        {
          name: 'Read Ops',
          type: 'bar',
          data: dataPoints.map((d) => [d.testIndex, d.diskReadOps, d.testName]),
          itemStyle: {
            color: '#06b6d4',
          },
          barMaxWidth: 20,
        },
        {
          name: 'Write Ops',
          type: 'bar',
          data: dataPoints.map((d) => [d.testIndex, d.diskWriteOps, d.testName]),
          itemStyle: {
            color: '#ec4899',
          },
          barMaxWidth: 20,
        },
      ]
      yAxisConfig = {
        type: 'value',
        name: 'Operations',
        nameTextStyle: { color: textColor },
        axisLabel: {
          color: textColor,
          formatter: (value: number) => formatOps(value),
        },
      }
    }

    return {
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'axis',
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        formatter: (
          params: Array<{ seriesName: string; color: string; value: [number, number, string] }>,
        ) => {
          if (!params.length) return ''
          const testName = params[0].value[2]
          const testIndex = params[0].value[0]
          let content = `<strong>Test #${testIndex}</strong><br/><span style="font-size: 11px; color: ${isDark ? '#9ca3af' : '#6b7280'}">${testName}</span><br/><br/>`
          params.forEach((p) => {
            const value = p.value[1]
            let formatted: string
            if (selectedMetric === 'cpu') {
              formatted = formatMicroseconds(value)
            } else if (selectedMetric === 'diskBytes') {
              formatted = formatBytes(value)
            } else {
              formatted = formatOps(value) + ' ops'
            }
            content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${formatted}<br/>`
          })
          return content
        },
      },
      legend: {
        type: 'scroll',
        bottom: 0,
        textStyle: { color: textColor },
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '15%',
        top: '10%',
        containLabel: true,
      },
      xAxis: {
        type: 'value',
        name: 'Test #',
        nameTextStyle: { color: textColor },
        min: 1,
        max: dataPoints.length,
        minInterval: 1,
        axisLabel: {
          color: textColor,
          formatter: (value: number) => `#${value}`,
        },
        axisLine: { lineStyle: { color: axisLineColor } },
        splitLine: { show: false },
      },
      yAxis: {
        ...yAxisConfig,
        axisLine: { lineStyle: { color: axisLineColor } },
        splitLine: { lineStyle: { color: splitLineColor } },
      },
      series,
    }
  }, [dataPoints, isDark, selectedMetric])

  if (!hasResourceData) {
    return null
  }

  return (
    <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Resource Usage</h3>
        <div className="inline-flex rounded-sm border border-gray-300 dark:border-gray-600">
          <button
            onClick={() => setSelectedMetric('cpu')}
            className={clsx(
              'px-3 py-1 text-xs/5 font-medium transition-colors',
              selectedMetric === 'cpu'
                ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
            )}
          >
            CPU
          </button>
          <button
            onClick={() => setSelectedMetric('diskBytes')}
            className={clsx(
              'border-l border-gray-300 px-3 py-1 text-xs/5 font-medium transition-colors dark:border-gray-600',
              selectedMetric === 'diskBytes'
                ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
            )}
          >
            Disk I/O
          </button>
          <button
            onClick={() => setSelectedMetric('diskOps')}
            className={clsx(
              'border-l border-gray-300 px-3 py-1 text-xs/5 font-medium transition-colors dark:border-gray-600',
              selectedMetric === 'diskOps'
                ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
            )}
          >
            IOPS
          </button>
        </div>
      </div>
      <ReactECharts option={option} style={{ height: '250px', width: '100%' }} opts={{ renderer: 'svg' }} />
      <p className="mt-2 text-center text-xs/5 text-gray-500 dark:text-gray-400">
        Resource usage per test (ordered by execution)
      </p>
    </div>
  )
}
