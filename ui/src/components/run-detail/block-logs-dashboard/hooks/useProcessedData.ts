import { useMemo } from 'react'
import type { BlockLogs } from '@/api/types'
import type { ProcessedTestData, DashboardState, DashboardStats } from '../types'
import { parseCategory } from '../utils/categoryParser'
import { percentile, removeOutliers, normalizeValue, emptyCategoryBreakdown } from '../utils/statistics'

export function useProcessedData(
  blockLogs: BlockLogs | null | undefined,
  state: DashboardState,
  executionOrder: Map<string, number>
) {
  return useMemo(() => {
    if (!blockLogs || Object.keys(blockLogs).length === 0) {
      return {
        data: [],
        stats: null,
        allData: [],
      }
    }

    // Transform raw block logs to processed data
    let data: ProcessedTestData[] = Object.entries(blockLogs).map(([testName, entry]) => {
      const overheadMs = entry.timing.state_read_ms + entry.timing.state_hash_ms + entry.timing.commit_ms

      return {
        testName,
        testOrder: executionOrder.get(testName) ?? Infinity,
        category: parseCategory(testName),
        throughput: entry.throughput.mgas_per_sec,
        executionMs: entry.timing.execution_ms,
        totalMs: entry.timing.total_ms,
        overheadMs,
        stateReadMs: entry.timing.state_read_ms,
        stateHashMs: entry.timing.state_hash_ms,
        commitMs: entry.timing.commit_ms,
        accountCacheHitRate: entry.cache.account.hit_rate,
        storageCacheHitRate: entry.cache.storage.hit_rate,
        codeCacheHitRate: entry.cache.code.hit_rate,
        gasUsed: entry.block.gas_used,
        txCount: entry.block.tx_count,
        // Normalized values will be calculated after filtering
        normalizedThroughput: 0,
        normalizedSpeed: 0,
        normalizedLowOverhead: 0,
        normalizedAccountCache: 0,
        normalizedCodeCache: 0,
      }
    })

    // Store all data before filtering for stats
    const allData = [...data]

    // Apply category filter
    if (state.categories.length > 0) {
      data = data.filter((d) => state.categories.includes(d.category))
    }

    // Apply throughput range filter
    if (state.minThroughput !== undefined) {
      data = data.filter((d) => d.throughput >= state.minThroughput!)
    }
    if (state.maxThroughput !== undefined) {
      data = data.filter((d) => d.throughput <= state.maxThroughput!)
    }

    // Apply outlier exclusion (also exclude 0 MGas/s tests)
    if (state.excludeOutliers) {
      data = data.filter((d) => d.throughput > 0)
      if (data.length > 4) {
        data = removeOutliers(data, (d) => d.throughput)
      }
    }

    // Calculate normalization bounds from filtered data
    if (data.length > 0) {
      const throughputs = data.map((d) => d.throughput)
      const executions = data.map((d) => d.executionMs)
      const overheads = data.map((d) => d.overheadMs)
      const accountCaches = data.map((d) => d.accountCacheHitRate)
      const codeCaches = data.map((d) => d.codeCacheHitRate)

      const minThroughput = Math.min(...throughputs)
      const maxThroughput = Math.max(...throughputs)
      const minExecution = Math.min(...executions)
      const maxExecution = Math.max(...executions)
      const minOverhead = Math.min(...overheads)
      const maxOverhead = Math.max(...overheads)
      const minAccountCache = Math.min(...accountCaches)
      const maxAccountCache = Math.max(...accountCaches)
      const minCodeCache = Math.min(...codeCaches)
      const maxCodeCache = Math.max(...codeCaches)

      // Update normalized values
      data = data.map((d) => ({
        ...d,
        normalizedThroughput: normalizeValue(d.throughput, minThroughput, maxThroughput),
        normalizedSpeed: normalizeValue(d.executionMs, minExecution, maxExecution, true), // Lower is better
        normalizedLowOverhead: normalizeValue(d.overheadMs, minOverhead, maxOverhead, true), // Lower is better
        normalizedAccountCache: normalizeValue(d.accountCacheHitRate, minAccountCache, maxAccountCache),
        normalizedCodeCache: normalizeValue(d.codeCacheHitRate, minCodeCache, maxCodeCache),
      }))
    }

    // Sort data
    data.sort((a, b) => {
      let comparison = 0
      switch (state.sortBy) {
        case 'throughput':
          comparison = a.throughput - b.throughput
          break
        case 'execution':
          comparison = a.executionMs - b.executionMs
          break
        case 'overhead':
          comparison = a.overheadMs - b.overheadMs
          break
        case 'name':
          comparison = a.testName.localeCompare(b.testName)
          break
        case 'order':
          comparison = a.testOrder - b.testOrder
          break
        case 'category':
          comparison = a.category.localeCompare(b.category)
          break
        case 'accountCache':
          comparison = a.accountCacheHitRate - b.accountCacheHitRate
          break
        case 'storageCache':
          comparison = a.storageCacheHitRate - b.storageCacheHitRate
          break
        case 'codeCache':
          comparison = a.codeCacheHitRate - b.codeCacheHitRate
          break
      }
      return state.sortOrder === 'asc' ? comparison : -comparison
    })

    // Calculate category breakdown from all data (unfiltered) so counts are always visible
    const categoryBreakdown = emptyCategoryBreakdown()
    for (const d of allData) {
      categoryBreakdown[d.category]++
    }

    // Calculate stats
    const stats: DashboardStats | null = data.length > 0 ? (() => {
      const throughputs = data.map((d) => d.throughput).sort((a, b) => a - b)
      const executions = data.map((d) => d.executionMs)
      const overheads = data.map((d) => d.overheadMs)

      return {
        count: data.length,
        avgThroughput: throughputs.reduce((sum, v) => sum + v, 0) / throughputs.length,
        minThroughput: throughputs[0],
        maxThroughput: throughputs[throughputs.length - 1],
        medianThroughput: percentile(throughputs, 50),
        avgExecution: executions.reduce((sum, v) => sum + v, 0) / executions.length,
        avgOverhead: overheads.reduce((sum, v) => sum + v, 0) / overheads.length,
        categoryBreakdown,
      }
    })() : {
      count: 0,
      avgThroughput: 0,
      minThroughput: 0,
      maxThroughput: 0,
      medianThroughput: 0,
      avgExecution: 0,
      avgOverhead: 0,
      categoryBreakdown,
    }

    return {
      data,
      stats,
      allData,
    }
  }, [blockLogs, state.categories, state.minThroughput, state.maxThroughput, state.excludeOutliers, state.sortBy, state.sortOrder, executionOrder])
}
