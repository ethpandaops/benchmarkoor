import { useEffect, useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { BlockLogEntry } from '@/api/types'
import { formatBytes } from '@/utils/format'

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

// Color palette
const COLORS = {
  green: '#22c55e',
  blue: '#3b82f6',
  orange: '#f97316',
  purple: '#a855f7',
  red: '#ef4444',
  cyan: '#06b6d4',
  yellow: '#eab308',
  pink: '#ec4899',
}

interface MetricCardProps {
  label: string
  value: string
  subValue?: string
}

function MetricCard({ label, value, subValue }: MetricCardProps) {
  return (
    <div className="flex flex-col rounded-xs bg-gray-50 px-3 py-2 dark:bg-gray-700/50">
      <span className="text-lg font-semibold text-gray-900 dark:text-gray-100">{value}</span>
      <span className="text-xs text-gray-500 dark:text-gray-400">{label}</span>
      {subValue && <span className="text-xs text-gray-400 dark:text-gray-500">{subValue}</span>}
    </div>
  )
}

function formatGas(gas: number): string {
  if (gas >= 1_000_000_000) {
    return `${(gas / 1_000_000_000).toFixed(1)}B`
  }
  if (gas >= 1_000_000) {
    return `${(gas / 1_000_000).toFixed(1)}M`
  }
  if (gas >= 1_000) {
    return `${(gas / 1_000).toFixed(1)}K`
  }
  return gas.toString()
}

interface BlockLogDetailsProps {
  blockLog: BlockLogEntry
}

export function BlockLogDetails({ blockLog }: BlockLogDetailsProps) {
  const isDark = useDarkMode()

  const textColor = isDark ? '#e5e7eb' : '#374151'
  const subTextColor = isDark ? '#9ca3af' : '#6b7280'

  // Calculate overhead time (non-execution time)
  const overheadMs = blockLog.timing.state_read_ms + blockLog.timing.state_hash_ms + blockLog.timing.commit_ms
  const executionPct = (blockLog.timing.execution_ms / blockLog.timing.total_ms) * 100
  const overheadPct = (overheadMs / blockLog.timing.total_ms) * 100

  // Timing breakdown bar chart options
  const timingBarOption = useMemo(() => ({
    tooltip: {
      trigger: 'item',
      backgroundColor: isDark ? '#1f2937' : '#ffffff',
      borderColor: isDark ? '#374151' : '#e5e7eb',
      textStyle: { color: textColor },
      formatter: (params: { name: string; value: number }) => {
        const pct = ((params.value / blockLog.timing.total_ms) * 100).toFixed(1)
        return `${params.name}: ${params.value.toFixed(2)}ms (${pct}%)`
      },
    },
    grid: {
      left: 0,
      right: 0,
      top: 0,
      bottom: 0,
      containLabel: false,
    },
    xAxis: {
      type: 'value' as const,
      max: blockLog.timing.total_ms,
      show: false,
    },
    yAxis: {
      type: 'category' as const,
      data: [''],
      show: false,
    },
    series: [
      {
        name: 'Execution',
        type: 'bar',
        stack: 'total',
        barWidth: '100%',
        data: [blockLog.timing.execution_ms],
        itemStyle: { color: COLORS.green },
        emphasis: { itemStyle: { color: COLORS.green } },
      },
      {
        name: 'State Read',
        type: 'bar',
        stack: 'total',
        data: [blockLog.timing.state_read_ms],
        itemStyle: { color: COLORS.blue },
        emphasis: { itemStyle: { color: COLORS.blue } },
      },
      {
        name: 'State Hash',
        type: 'bar',
        stack: 'total',
        data: [blockLog.timing.state_hash_ms],
        itemStyle: { color: COLORS.orange },
        emphasis: { itemStyle: { color: COLORS.orange } },
      },
      {
        name: 'Commit',
        type: 'bar',
        stack: 'total',
        data: [blockLog.timing.commit_ms],
        itemStyle: { color: COLORS.purple },
        emphasis: { itemStyle: { color: COLORS.purple } },
      },
    ],
  }), [blockLog.timing, isDark, textColor])

  // Overhead pie chart options
  const overheadPieOption = useMemo(() => ({
    tooltip: {
      trigger: 'item',
      backgroundColor: isDark ? '#1f2937' : '#ffffff',
      borderColor: isDark ? '#374151' : '#e5e7eb',
      textStyle: { color: textColor },
      formatter: (params: { name: string; value: number; percent: number }) => {
        return `${params.name}: ${params.value.toFixed(2)}ms (${params.percent.toFixed(1)}%)`
      },
    },
    legend: {
      orient: 'vertical' as const,
      right: 0,
      top: 'center',
      textStyle: { color: textColor, fontSize: 11 },
      itemWidth: 12,
      itemHeight: 8,
    },
    series: [
      {
        type: 'pie',
        radius: ['50%', '70%'],
        center: ['35%', '50%'],
        avoidLabelOverlap: false,
        label: { show: false },
        labelLine: { show: false },
        data: [
          { name: 'State Read', value: blockLog.timing.state_read_ms, itemStyle: { color: COLORS.blue } },
          { name: 'State Hash', value: blockLog.timing.state_hash_ms, itemStyle: { color: COLORS.orange } },
          { name: 'Commit', value: blockLog.timing.commit_ms, itemStyle: { color: COLORS.purple } },
        ].filter(d => d.value > 0),
        emphasis: {
          itemStyle: {
            shadowBlur: 10,
            shadowOffsetX: 0,
            shadowColor: 'rgba(0, 0, 0, 0.5)',
          },
        },
      },
    ],
  }), [blockLog.timing, isDark, textColor])

  // Cache performance stacked bar chart
  const cacheBarOption = useMemo(() => {
    const categories = ['Account', 'Storage', 'Code']
    const hits = [blockLog.cache.account.hits, blockLog.cache.storage.hits, blockLog.cache.code.hits]
    const misses = [blockLog.cache.account.misses, blockLog.cache.storage.misses, blockLog.cache.code.misses]
    const hitRates = [blockLog.cache.account.hit_rate, blockLog.cache.storage.hit_rate, blockLog.cache.code.hit_rate]

    return {
      tooltip: {
        trigger: 'axis' as const,
        axisPointer: { type: 'shadow' as const },
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        formatter: (params: Array<{ seriesName: string; name: string; value: number; color: string; dataIndex: number }>) => {
          const idx = params[0].dataIndex
          const total = hits[idx] + misses[idx]
          let content = `<strong>${params[0].name}</strong><br/>`
          content += `Hit Rate: ${(hitRates[idx] * 100).toFixed(1)}%<br/>`
          params.forEach(p => {
            content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${p.value.toLocaleString()} (${((p.value / total) * 100).toFixed(1)}%)<br/>`
          })
          return content
        },
      },
      legend: {
        bottom: 0,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: 30,
        top: 10,
        containLabel: true,
      },
      xAxis: {
        type: 'category' as const,
        data: categories,
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: isDark ? '#4b5563' : '#d1d5db' } },
        axisTick: { show: false },
      },
      yAxis: {
        type: 'value' as const,
        axisLabel: {
          color: textColor,
          fontSize: 11,
          formatter: (value: number) => {
            if (value >= 1000000) return `${(value / 1000000).toFixed(0)}M`
            if (value >= 1000) return `${(value / 1000).toFixed(0)}K`
            return value.toString()
          },
        },
        axisLine: { show: false },
        splitLine: { lineStyle: { color: isDark ? '#374151' : '#e5e7eb' } },
      },
      series: [
        {
          name: 'Hits',
          type: 'bar',
          stack: 'total',
          data: hits,
          itemStyle: { color: COLORS.green },
          emphasis: { focus: 'series' as const },
        },
        {
          name: 'Misses',
          type: 'bar',
          stack: 'total',
          data: misses,
          itemStyle: { color: COLORS.red },
          emphasis: { focus: 'series' as const },
        },
      ],
    }
  }, [blockLog.cache, isDark, textColor])

  // State operations grouped bar chart
  const stateOpsOption = useMemo(() => {
    const categories = ['Accounts', 'Storage', 'Code']
    const reads = [blockLog.state_reads.accounts, blockLog.state_reads.storage_slots, blockLog.state_reads.code]
    const writes = [blockLog.state_writes.accounts, blockLog.state_writes.storage_slots, blockLog.state_writes.code]
    const deleted = [blockLog.state_writes.accounts_deleted, blockLog.state_writes.storage_slots_deleted, 0]

    return {
      tooltip: {
        trigger: 'axis' as const,
        axisPointer: { type: 'shadow' as const },
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        formatter: (params: Array<{ seriesName: string; name: string; value: number; color: string }>) => {
          let content = `<strong>${params[0].name}</strong><br/>`
          params.forEach(p => {
            if (p.value > 0) {
              content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${p.value.toLocaleString()}<br/>`
            }
          })
          return content
        },
      },
      legend: {
        bottom: 0,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: 30,
        top: 10,
        containLabel: true,
      },
      xAxis: {
        type: 'category' as const,
        data: categories,
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: isDark ? '#4b5563' : '#d1d5db' } },
        axisTick: { show: false },
      },
      yAxis: {
        type: 'value' as const,
        axisLabel: {
          color: textColor,
          fontSize: 11,
          formatter: (value: number) => {
            if (value >= 1000000) return `${(value / 1000000).toFixed(0)}M`
            if (value >= 1000) return `${(value / 1000).toFixed(0)}K`
            return value.toString()
          },
        },
        axisLine: { show: false },
        splitLine: { lineStyle: { color: isDark ? '#374151' : '#e5e7eb' } },
      },
      series: [
        {
          name: 'Reads',
          type: 'bar',
          data: reads,
          itemStyle: { color: COLORS.blue },
          emphasis: { focus: 'series' as const },
        },
        {
          name: 'Writes',
          type: 'bar',
          data: writes,
          itemStyle: { color: COLORS.orange },
          emphasis: { focus: 'series' as const },
        },
        {
          name: 'Deleted',
          type: 'bar',
          data: deleted,
          itemStyle: { color: COLORS.red },
          emphasis: { focus: 'series' as const },
        },
      ],
    }
  }, [blockLog.state_reads, blockLog.state_writes, isDark, textColor])

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="rounded-xs bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/50 dark:text-purple-300">
            Block Logs
          </span>
        </div>
        <span className="text-sm text-gray-500 dark:text-gray-400">
          Block #{blockLog.block.number}
        </span>
      </div>

      {/* Key Metrics Row */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <MetricCard
          label="MGas/s"
          value={blockLog.throughput.mgas_per_sec.toFixed(1)}
        />
        <MetricCard
          label="Total Time"
          value={`${blockLog.timing.total_ms.toFixed(1)}ms`}
        />
        <MetricCard
          label="Gas Used"
          value={formatGas(blockLog.block.gas_used)}
        />
        <MetricCard
          label="Transactions"
          value={blockLog.block.tx_count.toString()}
        />
      </div>

      {/* Timing Breakdown */}
      <div className="flex flex-col gap-2">
        <div className="text-xs font-medium text-gray-700 dark:text-gray-300">Time Breakdown</div>
        <div className="h-6 overflow-hidden rounded-xs">
          <ReactECharts
            option={timingBarOption}
            style={{ height: '24px', width: '100%' }}
            opts={{ renderer: 'svg' }}
          />
        </div>
        <div className="flex justify-between text-xs">
          <span style={{ color: subTextColor }}>
            <span className="mr-1 inline-block size-2 rounded-xs" style={{ backgroundColor: COLORS.green }} />
            Execution: {blockLog.timing.execution_ms.toFixed(1)}ms ({executionPct.toFixed(1)}%)
          </span>
          <span style={{ color: subTextColor }}>
            Overhead: {overheadMs.toFixed(1)}ms ({overheadPct.toFixed(1)}%)
          </span>
        </div>
      </div>

      {/* Two-column grid for Overhead Pie and Cache Performance */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {/* Overhead Breakdown Pie */}
        <div className="flex flex-col gap-2 rounded-xs bg-gray-50 p-3 dark:bg-gray-700/50">
          <div className="text-xs font-medium text-gray-700 dark:text-gray-300">Overhead Breakdown</div>
          <ReactECharts
            option={overheadPieOption}
            style={{ height: '150px', width: '100%' }}
            opts={{ renderer: 'svg' }}
          />
        </div>

        {/* Cache Performance */}
        <div className="flex flex-col gap-2 rounded-xs bg-gray-50 p-3 dark:bg-gray-700/50">
          <div className="text-xs font-medium text-gray-700 dark:text-gray-300">Cache Performance</div>
          <ReactECharts
            option={cacheBarOption}
            style={{ height: '150px', width: '100%' }}
            opts={{ renderer: 'svg' }}
          />
        </div>
      </div>

      {/* State Operations */}
      <div className="flex flex-col gap-2 rounded-xs bg-gray-50 p-3 dark:bg-gray-700/50">
        <div className="flex items-center justify-between">
          <div className="text-xs font-medium text-gray-700 dark:text-gray-300">State Operations</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">
            Code bytes: {formatBytes(blockLog.state_reads.code_bytes)} read, {formatBytes(blockLog.state_writes.code_bytes)} written
          </div>
        </div>
        <ReactECharts
          option={stateOpsOption}
          style={{ height: '180px', width: '100%' }}
          opts={{ renderer: 'svg' }}
        />
      </div>
    </div>
  )
}
