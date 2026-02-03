import { useMemo } from 'react'
import type { ProcessedTestData, TestCategory } from '../types'
import { CacheBarChart } from '../charts/CacheBarChart'
import { CacheScatterChart } from '../charts/CacheScatterChart'
import { ALL_CATEGORIES } from '../utils/colors'

interface CacheTabProps {
  data: ProcessedTestData[]
  isDark: boolean
}

interface CacheStatCardProps {
  label: string
  value: string
  subLabel?: string
  isGood?: boolean
}

function CacheStatCard({ label, value, subLabel, isGood }: CacheStatCardProps) {
  return (
    <div className="rounded-sm bg-gray-50 px-4 py-3 dark:bg-gray-700/50">
      <div className="text-xs text-gray-500 dark:text-gray-400">{label}</div>
      <div className={`text-lg font-semibold ${isGood ? 'text-green-600 dark:text-green-400' : 'text-orange-600 dark:text-orange-400'}`}>
        {value}
      </div>
      {subLabel && <div className="text-xs text-gray-400 dark:text-gray-500">{subLabel}</div>}
    </div>
  )
}

export function CacheTab({ data, isDark }: CacheTabProps) {
  const activeCategories = useMemo<TestCategory[]>(() => {
    const categoriesInData = new Set(data.map(d => d.category))
    return ALL_CATEGORIES.filter(cat => categoriesInData.has(cat))
  }, [data])

  const cacheStats = useMemo(() => {
    if (data.length === 0) return null

    const accountRates = data.map((d) => d.accountCacheHitRate)
    const storageRates = data.map((d) => d.storageCacheHitRate)
    const codeRates = data.map((d) => d.codeCacheHitRate)

    const avgAccount = accountRates.reduce((a, b) => a + b, 0) / accountRates.length
    const avgStorage = storageRates.reduce((a, b) => a + b, 0) / storageRates.length
    const avgCode = codeRates.reduce((a, b) => a + b, 0) / codeRates.length

    const poorAccountCount = accountRates.filter((r) => r < 80).length
    const poorStorageCount = storageRates.filter((r) => r < 80).length
    const poorCodeCount = codeRates.filter((r) => r < 80).length

    return {
      avgAccount,
      avgStorage,
      avgCode,
      poorAccountCount,
      poorStorageCount,
      poorCodeCount,
      total: data.length,
    }
  }, [data])

  if (data.length === 0 || !cacheStats) {
    return (
      <div className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        No data available for the current filters.
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Stats Row */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
        <CacheStatCard
          label="Avg Account Cache"
          value={`${cacheStats.avgAccount.toFixed(1)}%`}
          isGood={cacheStats.avgAccount >= 80}
        />
        <CacheStatCard
          label="Avg Storage Cache"
          value={`${cacheStats.avgStorage.toFixed(1)}%`}
          isGood={cacheStats.avgStorage >= 80}
        />
        <CacheStatCard
          label="Avg Code Cache"
          value={`${cacheStats.avgCode.toFixed(1)}%`}
          isGood={cacheStats.avgCode >= 80}
        />
        <CacheStatCard
          label="Poor Account (<80%)"
          value={cacheStats.poorAccountCount.toString()}
          subLabel={`of ${cacheStats.total} tests`}
          isGood={cacheStats.poorAccountCount === 0}
        />
        <CacheStatCard
          label="Poor Storage (<80%)"
          value={cacheStats.poorStorageCount.toString()}
          subLabel={`of ${cacheStats.total} tests`}
          isGood={cacheStats.poorStorageCount === 0}
        />
        <CacheStatCard
          label="Poor Code (<80%)"
          value={cacheStats.poorCodeCount.toString()}
          subLabel={`of ${cacheStats.total} tests`}
          isGood={cacheStats.poorCodeCount === 0}
        />
      </div>

      {/* Cache Bar Charts */}
      <div className="rounded-xs border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <CacheBarChart cacheType="account" data={data} isDark={isDark} activeCategories={activeCategories} />
      </div>
      <div className="rounded-xs border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <CacheBarChart cacheType="storage" data={data} isDark={isDark} activeCategories={activeCategories} />
      </div>
      <div className="rounded-xs border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <CacheBarChart cacheType="code" data={data} isDark={isDark} activeCategories={activeCategories} />
      </div>

      {/* Scatter Chart for correlation view */}
      <div className="rounded-xs border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <CacheScatterChart data={data} isDark={isDark} activeCategories={activeCategories} />
      </div>
    </div>
  )
}
