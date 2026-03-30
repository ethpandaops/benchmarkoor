import { useMemo, useState, useEffect, useCallback } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import { fetchData } from '@/api/client'
import type { SuiteInfo } from '@/api/types'
import { useIndex } from '@/api/hooks/useIndex'
import { SuitesTable, type SuiteSortColumn, type SuiteSortDirection } from '@/components/suites/SuitesTable'
import { Pagination } from '@/components/shared/Pagination'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { EmptyState } from '@/components/shared/EmptyState'

const PAGE_SIZE = 20
const NO_VALUE = '(no value)'

interface SuiteEntry {
  hash: string
  runCount: number
  lastRun: number
}

interface GroupEntry {
  /** Per-key label values, e.g. { context: "ABC", network: "mainnet" } */
  labels: Record<string, string>
  suites: SuiteEntry[]
}

function parseGroupBy(raw: string | undefined): string[] {
  if (!raw) return []
  return raw.split(',').filter(Boolean)
}

function serializeGroupBy(keys: string[]): string | undefined {
  return keys.length > 0 ? keys.join(',') : undefined
}

export function SuitesPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/suites' }) as {
    page?: number
    sortBy?: SuiteSortColumn
    sortDir?: SuiteSortDirection
    groupBy?: string
  }
  const { page = 1, sortBy = 'lastRun', sortDir = 'desc' } = search
  const groupByKeys = useMemo(() => parseGroupBy(search.groupBy), [search.groupBy])
  const { data: index, isLoading, error, refetch } = useIndex()
  const [currentPage, setCurrentPage] = useState(page)

  useEffect(() => {
    setCurrentPage(page)
  }, [page])

  const suites = useMemo(() => {
    if (!index) return []

    const suiteMap = new Map<string, { runCount: number; lastRun: number }>()
    for (const entry of index.entries) {
      if (entry.suite_hash) {
        const existing = suiteMap.get(entry.suite_hash)
        if (existing) {
          existing.runCount++
          existing.lastRun = Math.max(existing.lastRun, entry.timestamp)
        } else {
          suiteMap.set(entry.suite_hash, { runCount: 1, lastRun: entry.timestamp })
        }
      }
    }

    return Array.from(suiteMap.entries()).map(([hash, { runCount, lastRun }]) => ({ hash, runCount, lastRun }))
  }, [index])

  // Fetch all suite infos to extract label keys and group
  const suiteQueries = useQueries({
    queries: suites.map((s) => ({
      queryKey: ['suite', s.hash],
      queryFn: async () => {
        const { data } = await fetchData<SuiteInfo>(`suites/${s.hash}/summary.json`, { cacheBustInterval: 3600 })
        return data
      },
    })),
  })

  const suiteInfoMap = useMemo(() => {
    const map = new Map<string, SuiteInfo>()
    for (let i = 0; i < suites.length; i++) {
      const info = suiteQueries[i]?.data
      if (info) map.set(suites[i].hash, info)
    }
    return map
  }, [suites, suiteQueries])

  // Collect all unique label keys across suites
  const labelKeys = useMemo(() => {
    const keys = new Set<string>()
    for (const info of suiteInfoMap.values()) {
      if (info.metadata?.labels) {
        for (const key of Object.keys(info.metadata.labels)) {
          keys.add(key)
        }
      }
    }
    keys.delete('name')
    return Array.from(keys).sort()
  }, [suiteInfoMap])

  // Group suites by the selected label keys (supports multi-key combos)
  const groups = useMemo((): GroupEntry[] | null => {
    if (groupByKeys.length === 0) return null

    const grouped = new Map<string, GroupEntry>()
    for (const suite of suites) {
      const info = suiteInfoMap.get(suite.hash)
      const labels: Record<string, string> = {}
      for (const key of groupByKeys) {
        labels[key] = info?.metadata?.labels?.[key] ?? NO_VALUE
      }
      // Stable composite key from sorted key=value pairs
      const compositeKey = groupByKeys.map((k) => `${k}=${labels[k]}`).join('\0')
      const existing = grouped.get(compositeKey)
      if (existing) {
        existing.suites.push(suite)
      } else {
        grouped.set(compositeKey, { labels, suites: [suite] })
      }
    }

    return Array.from(grouped.values()).sort((a, b) => {
      // Groups where ALL keys have real values sort first;
      // any group containing a NO_VALUE sorts to the end.
      const aHasNoValue = Object.values(a.labels).some((v) => v === NO_VALUE)
      const bHasNoValue = Object.values(b.labels).some((v) => v === NO_VALUE)
      if (aHasNoValue !== bHasNoValue) return aHasNoValue ? 1 : -1

      // Within the same tier, sort alphabetically by each key's value
      for (const key of groupByKeys) {
        const cmp = a.labels[key].localeCompare(b.labels[key])
        if (cmp !== 0) return cmp
      }
      return 0
    })
  }, [groupByKeys, suites, suiteInfoMap])

  const updateSearch = useCallback(
    (patch: Record<string, string | number | undefined>) => {
      navigate({
        to: '/suites',
        search: { page: search.page, sortBy: search.sortBy, sortDir: search.sortDir, groupBy: search.groupBy, ...patch },
        replace: true,
      })
    },
    [navigate, search.page, search.sortBy, search.sortDir, search.groupBy],
  )

  const handlePageChange = (newPage: number) => {
    setCurrentPage(newPage)
    updateSearch({ page: newPage })
  }

  const handleSortChange = (newSortBy: SuiteSortColumn, newSortDir: SuiteSortDirection) => {
    updateSearch({ sortBy: newSortBy, sortDir: newSortDir })
  }

  const toggleGroupByKey = (key: string) => {
    const next = groupByKeys.includes(key)
      ? groupByKeys.filter((k) => k !== key)
      : [...groupByKeys, key]
    updateSearch({ groupBy: serializeGroupBy(next), page: 1 })
    setCurrentPage(1)
  }

  if (isLoading) {
    return <LoadingState message="Loading suites..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (suites.length === 0) {
    return <EmptyState title="No suites found" message="No test suites have been used yet." />
  }

  const totalPages = groups ? 0 : Math.ceil(suites.length / PAGE_SIZE)
  const paginatedSuites = groups ? [] : suites.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">Test Suites ({suites.length})</h1>
        {labelKeys.length > 0 && (
          <div className="flex items-center gap-2 text-sm/6 text-gray-600 dark:text-gray-400">
            <span>Group by:</span>
            <div className="flex gap-1">
              {labelKeys.map((key) => (
                <button
                  key={key}
                  onClick={() => toggleGroupByKey(key)}
                  className={clsx(
                    'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                    groupByKeys.includes(key)
                      ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                      : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                  )}
                >
                  {key}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {groups ? (
        <div className="flex flex-col gap-8">
          {groups.map((group) => {
            const groupKey = groupByKeys.map((k) => `${k}=${group.labels[k]}`).join(', ')
            return (
              <div key={groupKey} className="flex flex-col gap-3">
                <h2 className="flex flex-wrap items-center gap-2 text-lg/7 font-semibold text-gray-900 dark:text-gray-100">
                  {groupByKeys.map((key) => (
                    <span key={key} className="flex items-center gap-1">
                      <span className="rounded-xs bg-gray-100 px-2 py-0.5 text-sm/6 font-medium text-gray-700 dark:bg-gray-700 dark:text-gray-300">
                        {key}
                      </span>
                      <span>{group.labels[key]}</span>
                    </span>
                  ))}
                  <span className="text-sm/6 font-normal text-gray-500 dark:text-gray-400">
                    ({group.suites.length})
                  </span>
                </h2>
                <SuitesTable suites={group.suites} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} />
              </div>
            )
          })}
        </div>
      ) : (
        <>
          <SuitesTable suites={paginatedSuites} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} />

          {totalPages > 1 && (
            <div className="flex justify-center">
              <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={handlePageChange} />
            </div>
          )}
        </>
      )}
    </div>
  )
}
