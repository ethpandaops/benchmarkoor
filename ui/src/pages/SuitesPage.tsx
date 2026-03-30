import { useMemo, useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import { Plus, X } from 'lucide-react'
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
const DAY_MS = 24 * 60 * 60 * 1000
const INACTIVE_OPTIONS = [
  { label: '7d', days: 7 },
  { label: '14d', days: 14 },
  { label: '30d', days: 30 },
  { label: '90d', days: 90 },
] as const
const DEFAULT_INACTIVE_DAYS = 7

interface SuiteEntry {
  hash: string
  runCount: number
  lastRun: number
}

interface GroupEntry {
  labels: Record<string, string>
  suites: SuiteEntry[]
}

/** label filters: key → set of selected values (OR within key, AND across keys) */
type LabelFilters = Map<string, Set<string>>

function parseGroupBy(raw: string | undefined): string[] {
  if (!raw) return []
  return raw.split(',').filter(Boolean)
}

function serializeGroupBy(keys: string[]): string {
  return keys.length > 0 ? keys.join(',') : 'none'
}

/** Parse URL param `key:val1|val2,key2:val3` into a Map */
function parseLabelFilters(raw: string | undefined): LabelFilters {
  const filters: LabelFilters = new Map()
  if (!raw) return filters
  for (const segment of raw.split(',')) {
    const idx = segment.indexOf(':')
    if (idx < 1) continue
    const key = decodeURIComponent(segment.slice(0, idx))
    const values = segment.slice(idx + 1).split('|').map(decodeURIComponent).filter(Boolean)
    if (values.length > 0) filters.set(key, new Set(values))
  }
  return filters
}

function serializeLabelFilters(filters: LabelFilters): string | undefined {
  if (filters.size === 0) return undefined
  const parts: string[] = []
  for (const [key, values] of filters) {
    if (values.size === 0) continue
    parts.push(`${encodeURIComponent(key)}:${Array.from(values).map(encodeURIComponent).join('|')}`)
  }
  return parts.length > 0 ? parts.join(',') : undefined
}

// ---------------------------------------------------------------------------
// LabelFilterBar — Grafana-style dynamic filter chips with dropdowns
// ---------------------------------------------------------------------------

interface LabelFilterBarProps {
  labelKeys: string[]
  /** All known values per label key */
  labelValues: Map<string, string[]>
  filters: LabelFilters
  onFiltersChange: (filters: LabelFilters) => void
}

function LabelFilterBar({ labelKeys, labelValues, filters, onFiltersChange }: LabelFilterBarProps) {
  const [keyDropdownOpen, setKeyDropdownOpen] = useState(false)
  const [valueDropdownKey, setValueDropdownKey] = useState<string | null>(null)
  const keyRef = useRef<HTMLDivElement>(null)
  const valueRef = useRef<HTMLDivElement>(null)

  // Close dropdowns on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (keyDropdownOpen && keyRef.current && !keyRef.current.contains(e.target as Node)) {
        setKeyDropdownOpen(false)
      }
      if (valueDropdownKey && valueRef.current && !valueRef.current.contains(e.target as Node)) {
        setValueDropdownKey(null)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [keyDropdownOpen, valueDropdownKey])

  const availableKeys = labelKeys.filter((k) => !filters.has(k) && k !== valueDropdownKey)

  const toggleValue = (key: string, value: string) => {
    const next = new Map(filters)
    const values = new Set(next.get(key) ?? [])
    if (values.has(value)) {
      values.delete(value)
      if (values.size === 0) {
        next.delete(key)
      } else {
        next.set(key, values)
      }
    } else {
      values.add(value)
      next.set(key, values)
    }
    onFiltersChange(next)
  }

  const removeFilter = (key: string) => {
    const next = new Map(filters)
    next.delete(key)
    onFiltersChange(next)
    if (valueDropdownKey === key) setValueDropdownKey(null)
  }

  const addKey = (key: string) => {
    setKeyDropdownOpen(false)
    // Open value picker immediately for the new key
    setValueDropdownKey(key)
  }

  // Build the list of chips: active filters + pending key (not yet in filters)
  const chipKeys = useMemo(() => {
    const keys = Array.from(filters.keys())
    if (valueDropdownKey && !filters.has(valueDropdownKey)) {
      keys.push(valueDropdownKey)
    }
    return keys
  }, [filters, valueDropdownKey])

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* Filter chips (active + pending) */}
      {chipKeys.map((key) => {
        const values = filters.get(key)
        const isPending = !values
        return (
          <div key={key} className="relative" ref={valueDropdownKey === key ? valueRef : undefined}>
            <div
              role="button"
              tabIndex={0}
              onClick={() => setValueDropdownKey(valueDropdownKey === key ? null : key)}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setValueDropdownKey(valueDropdownKey === key ? null : key) } }}
              className={clsx(
                'flex cursor-pointer items-center gap-1.5 rounded-xs border px-2 py-1 text-xs/5 font-medium transition-colors',
                isPending
                  ? 'border-dashed border-blue-300 bg-blue-50/50 text-blue-500 dark:border-blue-700 dark:bg-blue-900/20 dark:text-blue-400'
                  : 'border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300 dark:hover:bg-blue-900/50',
              )}
            >
              <span className="font-semibold">{key}</span>
              {values && values.size > 0 && (
                <>
                  <span>=</span>
                  <span>{Array.from(values).join(', ')}</span>
                </>
              )}
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  if (isPending) { setValueDropdownKey(null) } else { removeFilter(key) }
                }}
                className="ml-0.5 rounded-xs p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800"
              >
                <X className="size-3" />
              </button>
            </div>

            {/* Value multi-select dropdown */}
            {valueDropdownKey === key && (
              <div className="absolute top-full left-0 z-50 mt-1 max-h-64 min-w-48 overflow-y-auto rounded-xs border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-800">
                {(labelValues.get(key) ?? []).map((val) => {
                  const selected = values?.has(val) ?? false
                  return (
                    <button
                      key={val}
                      onClick={() => toggleValue(key, val)}
                      className={clsx(
                        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm/6 transition-colors',
                        selected
                          ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                          : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700',
                      )}
                    >
                      <span className={clsx(
                        'flex size-4 shrink-0 items-center justify-center rounded-xs border text-xs/3',
                        selected
                          ? 'border-blue-500 bg-blue-500 text-white dark:border-blue-400 dark:bg-blue-400'
                          : 'border-gray-300 dark:border-gray-600',
                      )}>
                        {selected && '✓'}
                      </span>
                      {val}
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        )
      })}

      {/* Add filter button */}
      {availableKeys.length > 0 && (
        <div className="relative" ref={keyRef}>
          <button
            onClick={() => { setKeyDropdownOpen(!keyDropdownOpen); setValueDropdownKey(null) }}
            className="flex items-center gap-1 rounded-xs border border-dashed border-gray-300 px-2 py-1 text-xs/5 text-gray-500 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-gray-600 dark:text-gray-400 dark:hover:border-gray-500 dark:hover:text-gray-300"
          >
            <Plus className="size-3" />
            Filter
          </button>

          {keyDropdownOpen && (
            <div className="absolute top-full left-0 z-50 mt-1 min-w-36 overflow-hidden rounded-xs border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-800">
              {availableKeys.map((key) => (
                <button
                  key={key}
                  onClick={() => addKey(key)}
                  className="flex w-full px-3 py-1.5 text-left text-sm/6 text-gray-700 transition-colors hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700"
                >
                  {key}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// SuitesPage
// ---------------------------------------------------------------------------

export function SuitesPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/suites' }) as {
    page?: number
    sortBy?: SuiteSortColumn
    sortDir?: SuiteSortDirection
    groupBy?: string
    labels?: string
    hideInactive?: string
    inactiveDays?: string
  }
  const { page = 1, sortBy = 'lastRun', sortDir = 'desc' } = search
  const hideInactive = search.hideInactive === '1'
  const inactiveDays = Number(search.inactiveDays) || DEFAULT_INACTIVE_DAYS
  const inactiveThresholdMs = inactiveDays * DAY_MS
  const [now] = useState(() => Date.now())
  const groupByKeys = useMemo(
    () => parseGroupBy(search.groupBy === 'none' ? undefined : (search.groupBy ?? 'context')),
    [search.groupBy],
  )
  const labelFilters = useMemo(() => parseLabelFilters(search.labels), [search.labels])
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

  // Collect all unique label keys and per-key values across suites
  const { labelKeys, labelValues } = useMemo(() => {
    const valuesMap = new Map<string, Set<string>>()
    for (const info of suiteInfoMap.values()) {
      if (info.metadata?.labels) {
        for (const [key, value] of Object.entries(info.metadata.labels)) {
          if (key === 'name') continue
          let set = valuesMap.get(key)
          if (!set) {
            set = new Set()
            valuesMap.set(key, set)
          }
          set.add(value)
        }
      }
    }
    const keys = Array.from(valuesMap.keys()).sort()
    const values = new Map<string, string[]>()
    for (const [key, set] of valuesMap) {
      values.set(key, Array.from(set).sort())
    }
    return { labelKeys: keys, labelValues: values }
  }, [suiteInfoMap])

  // Apply label filters to suites
  const filteredSuites = useMemo(() => {
    if (labelFilters.size === 0) return suites
    return suites.filter((suite) => {
      const info = suiteInfoMap.get(suite.hash)
      const labels = info?.metadata?.labels
      for (const [key, allowedValues] of labelFilters) {
        const actual = labels?.[key]
        if (!actual || !allowedValues.has(actual)) return false
      }
      return true
    })
  }, [suites, suiteInfoMap, labelFilters])

  // Group filtered suites by the selected label keys
  const groups = useMemo((): GroupEntry[] | null => {
    if (groupByKeys.length === 0) return null

    const grouped = new Map<string, GroupEntry>()
    for (const suite of filteredSuites) {
      const info = suiteInfoMap.get(suite.hash)
      const labels: Record<string, string> = {}
      for (const key of groupByKeys) {
        labels[key] = info?.metadata?.labels?.[key] ?? NO_VALUE
      }
      const compositeKey = groupByKeys.map((k) => `${k}=${labels[k]}`).join('\0')
      const existing = grouped.get(compositeKey)
      if (existing) {
        existing.suites.push(suite)
      } else {
        grouped.set(compositeKey, { labels, suites: [suite] })
      }
    }

    const isAllInactive = (group: GroupEntry) =>
      group.suites.every((s) => (now - s.lastRun * 1000) > inactiveThresholdMs)

    return Array.from(grouped.values()).sort((a, b) => {
      // Groups where all suites are inactive sort to the bottom
      const aAllInactive = isAllInactive(a)
      const bAllInactive = isAllInactive(b)
      if (aAllInactive !== bAllInactive) return aAllInactive ? 1 : -1

      const aHasNoValue = Object.values(a.labels).some((v) => v === NO_VALUE)
      const bHasNoValue = Object.values(b.labels).some((v) => v === NO_VALUE)
      if (aHasNoValue !== bHasNoValue) return aHasNoValue ? 1 : -1
      for (const key of groupByKeys) {
        const cmp = a.labels[key].localeCompare(b.labels[key])
        if (cmp !== 0) return cmp
      }
      return 0
    })
  }, [groupByKeys, filteredSuites, suiteInfoMap, now, inactiveThresholdMs])

  const updateSearch = useCallback(
    (patch: Record<string, string | number | undefined>) => {
      navigate({
        to: '/suites',
        search: {
          page: search.page, sortBy: search.sortBy, sortDir: search.sortDir,
          groupBy: search.groupBy, labels: search.labels, hideInactive: search.hideInactive, inactiveDays: search.inactiveDays, ...patch,
        },
        replace: true,
      })
    },
    [navigate, search.page, search.sortBy, search.sortDir, search.groupBy, search.labels, search.hideInactive, search.inactiveDays],
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

  const handleLabelFiltersChange = (next: LabelFilters) => {
    updateSearch({ labels: serializeLabelFilters(next), page: 1 })
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

  const totalPages = groups ? 0 : Math.ceil(filteredSuites.length / PAGE_SIZE)
  const paginatedSuites = groups ? [] : filteredSuites.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">
          Test Suites ({filteredSuites.length}{labelFilters.size > 0 && ` / ${suites.length}`})
        </h1>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2 text-sm/6 text-gray-600 dark:text-gray-400">
            <span>Inactive:</span>
            <div className="flex gap-1">
              {INACTIVE_OPTIONS.map((opt) => (
                <button
                  key={opt.days}
                  onClick={() => updateSearch({ inactiveDays: opt.days === DEFAULT_INACTIVE_DAYS ? undefined : String(opt.days) })}
                  className={clsx(
                    'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                    inactiveDays === opt.days
                      ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                      : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                  )}
                >
                  {opt.label}
                </button>
              ))}
            </div>
            <button
              onClick={() => updateSearch({ hideInactive: hideInactive ? undefined : '1' })}
              className={clsx(
                'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                hideInactive
                  ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                  : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
              )}
            >
              Hide
            </button>
          </div>
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
      </div>

      {labelKeys.length > 0 && (
        <LabelFilterBar
          labelKeys={labelKeys}
          labelValues={labelValues}
          filters={labelFilters}
          onFiltersChange={handleLabelFiltersChange}
        />
      )}

      {groups ? (
        <div className="flex flex-col gap-8">
          {groups.map((group) => {
            const groupKey = groupByKeys.map((k) => `${k}=${group.labels[k]}`).join(', ')
            const inactiveCount = group.suites.filter((s) => (now - s.lastRun * 1000) > inactiveThresholdMs).length
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
                    ({group.suites.length}{inactiveCount > 0 && `, ${inactiveCount} inactive`})
                  </span>
                </h2>
                <SuitesTable suites={group.suites} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} hideInactive={hideInactive} inactiveThresholdMs={inactiveThresholdMs} />
              </div>
            )
          })}
        </div>
      ) : (
        <>
          <SuitesTable suites={paginatedSuites} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} hideInactive={hideInactive} inactiveThresholdMs={inactiveThresholdMs} />

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
