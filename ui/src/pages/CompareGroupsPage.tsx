import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearch, useNavigate } from '@tanstack/react-router'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import type { RunConfig, RunResult } from '@/api/types'
import { fetchData } from '@/api/client'
import { testNameMatches, toggleSearchTerm, TEST_FILTER_HINT } from '@/utils/eestNameFilter'
import { useIndex } from '@/api/hooks/useIndex'
import { useSuite } from '@/api/hooks/useSuite'
import { LoadingState } from '@/components/shared/Spinner'
import { JDenticon } from '@/components/shared/JDenticon'
import { FacetPanel } from '@/components/shared/FacetPanel'
import { type StepTypeOption, ALL_STEP_TYPES, DEFAULT_STEP_FILTER } from '@/pages/RunDetailPage'
import { type CompareRun, type ChartType, CHART_TYPE_OPTIONS } from '@/components/compare/constants'
import { MetricsComparison } from '@/components/compare/MetricsComparison'
import { MGasComparisonChart } from '@/components/compare/MGasComparisonChart'
import { CVComparisonChart } from '@/components/compare/CVComparisonChart'
import { PercentageDiffChart } from '@/components/compare/PercentageDiffChart'
import { TestComparisonTable } from '@/components/compare/TestComparisonTable'
import { ResourceComparisonCharts } from '@/components/compare/ResourceComparisonCharts'
import { GroupBuilder } from '@/components/compare/GroupBuilder'
import { type GroupDef, parseGroupsParam, encodeGroupsParam } from '@/components/compare/groupUtils'
import { averageResults } from '@/utils/averageResults'
import { TestDetailModal } from '@/components/compare/TestDetailModal'

function parseStepFilter(param: string | undefined): StepTypeOption[] {
  if (!param) return DEFAULT_STEP_FILTER
  const steps = param.split(',').filter((s): s is StepTypeOption => ALL_STEP_TYPES.includes(s as StepTypeOption))
  return steps.length > 0 ? steps : DEFAULT_STEP_FILTER
}

export function CompareGroupsPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/compare/groups' }) as {
    suite?: string
    groups?: string
    sample?: string
    agg?: string
    steps?: string
    baseline?: string
    tableBase?: string
    chart?: string
    sort?: string
    sortDir?: string
    filter?: string
    filterRegex?: string
    gasBuckets?: string
    diffFilter?: string
  }

  const suiteHash = search.suite ?? ''
  const groups = useMemo(() => parseGroupsParam(search.groups), [search.groups])
  const sampleSize = Math.max(1, Math.min(50, parseInt(search.sample ?? '5', 10) || 5))
  const aggMode = (search.agg === 'median' ? 'median' : 'avg') as 'avg' | 'median'
  const stepFilter = parseStepFilter(search.steps)

  const { data: index } = useIndex()
  const { data: suite } = useSuite(suiteHash)

  const updateSearch = useCallback(
    (patch: Record<string, string | undefined>) => {
      navigate({
        to: '/compare/groups',
        search: {
          suite: search.suite,
          groups: search.groups,
          sample: search.sample,
          agg: search.agg,
          steps: search.steps,
          baseline: search.baseline,
          tableBase: search.tableBase,
          chart: search.chart,
          sort: search.sort,
          sortDir: search.sortDir,
          filter: search.filter,
          filterRegex: search.filterRegex,
          gasBuckets: search.gasBuckets,
          diffFilter: search.diffFilter,
          ...patch,
        },
        replace: true,
      })
    },
    [navigate, search],
  )

  const setSuiteHash = useCallback(
    (hash: string) => updateSearch({ suite: hash || undefined, groups: undefined }),
    [updateSearch],
  )
  const setGroups = useCallback(
    (g: GroupDef[]) => updateSearch({ groups: encodeGroupsParam(g) || undefined }),
    [updateSearch],
  )
  const setSampleSize = useCallback(
    (n: number) => updateSearch({ sample: n === 5 ? undefined : String(n) }),
    [updateSearch],
  )
  const setAggMode = useCallback(
    (m: 'avg' | 'median') => updateSearch({ agg: m === 'avg' ? undefined : m }),
    [updateSearch],
  )

  // ─── Run selection from the index ──────────────────────────────
  // For each group, find the latest N runs matching the criteria.
  // All matched entries per group (sorted newest-first, NOT truncated
  // to sample size). Used for the run-boxes display in the builder.
  const groupMatchedEntries = useMemo(() => {
    if (!index || !suiteHash || groups.length === 0) return []

    return groups.map((group) =>
      index.entries
        .filter((e) => {
          if (e.suite_hash !== suiteHash) return false
          if (e.instance.client !== group.client) return false
          for (const [key, val] of Object.entries(group.metadata)) {
            if (e.metadata?.[key] !== val) return false
          }
          return true
        })
        .sort((a, b) => (b.timestamp ?? 0) - (a.timestamp ?? 0)),
    )
  }, [index, suiteHash, groups])

  // Run IDs used for data fetching (truncated to sample size).
  const groupRuns = useMemo(
    () => groupMatchedEntries.map((entries) => entries.slice(0, sampleSize).map((e) => e.run_id)),
    [groupMatchedEntries, sampleSize],
  )

  // Flatten all run IDs for batch fetching.
  const allRunIds = useMemo(() => groupRuns.flat(), [groupRuns])

  // ─── Data fetching ─────────────────────────────────────────────
  const configQueries = useQueries({
    queries: groups.map((_, i) => {
      const runId = groupRuns[i]?.[0]
      return {
        queryKey: ['run', runId, 'config'],
        queryFn: async () => {
          const { data, status } = await fetchData<RunConfig>(`runs/${runId}/config.json`)
          if (!data) throw new Error(`Failed to fetch config: ${status}`)
          return data
        },
        enabled: !!runId,
      }
    }),
  })

  const resultQueries = useQueries({
    queries: allRunIds.map((runId) => ({
      queryKey: ['run', runId, 'result'],
      queryFn: async () => {
        const { data } = await fetchData<RunResult>(`runs/${runId}/result.json`)
        return data ?? null
      },
      enabled: !!runId,
    })),
  })

  const isLoading = configQueries.some((q) => q.isLoading) || resultQueries.some((q) => q.isLoading)

  // ─── Compute averages and build synthetic CompareRun[] ─────────
  const { syntheticRuns, varianceMap } = useMemo(() => {
    if (groups.length === 0 || isLoading) return { syntheticRuns: [] as CompareRun[], varianceMap: new Map() }

    const runs: CompareRun[] = []
    const varMap = new Map<number, Record<string, { mgasStddev: number; mgasMean: number; mgasMin: number; mgasMax: number }>>()
    let resultOffset = 0

    for (let gi = 0; gi < groups.length; gi++) {
      const runIds = groupRuns[gi] ?? []
      const config = configQueries[gi]?.data

      // Gather the results for this group.
      const results: RunResult[] = []
      for (let ri = 0; ri < runIds.length; ri++) {
        const r = resultQueries[resultOffset + ri]?.data
        if (r) results.push(r)
      }
      resultOffset += runIds.length

      if (!config || results.length === 0) continue

      const averaged = averageResults(results, stepFilter, aggMode)

      // Build a label from the group criteria.
      const metaStr = Object.entries(groups[gi].metadata)
        .map(([k, v]) => `${k}=${v}`)
        .join(', ')
      // Client name is omitted since the logo image already identifies
      // it. Show only the metadata filters, or a generic "default" if none.
      const label = metaStr || 'default'

      const synConfig: RunConfig = {
        ...config,
        instance: { ...config.instance, id: label },
      }

      runs.push({
        runId: `group-${gi}`,
        config: synConfig,
        result: averaged.result,
        index: gi,
      })

      varMap.set(gi, averaged.variance)
    }

    return { syntheticRuns: runs, varianceMap: varMap }
  }, [groups, groupRuns, configQueries, resultQueries, stepFilter, aggMode, isLoading])

  // ─── Available suites + clients for the builder ────────────────
  const availableSuites = useMemo(() => {
    if (!index) return []
    const suites = new Map<string, number>()
    for (const e of index.entries) {
      if (e.suite_hash) suites.set(e.suite_hash, (suites.get(e.suite_hash) ?? 0) + 1)
    }
    return [...suites.entries()]
      .sort((a, b) => b[1] - a[1])
      .map(([hash]) => hash)
  }, [index])

  const availableClients = useMemo(() => {
    if (!index || !suiteHash) return []
    const clients = new Set<string>()
    for (const e of index.entries) {
      if (e.suite_hash === suiteHash) clients.add(e.instance.client)
    }
    return [...clients].sort()
  }, [index, suiteHash])

  const availableMetadataKeys = useMemo(() => {
    if (!index || !suiteHash) return new Map<string, Set<string>>()
    const keys = new Map<string, Set<string>>()
    for (const e of index.entries) {
      if (e.suite_hash !== suiteHash) continue
      if (!e.metadata) continue
      for (const [k, v] of Object.entries(e.metadata)) {
        if (k.startsWith('github.')) continue // skip reserved keys
        let vals = keys.get(k)
        if (!vals) {
          vals = new Set()
          keys.set(k, vals)
        }
        vals.add(v)
      }
    }
    return keys
  }, [index, suiteHash])

  // ─── Table/chart controls ─────────────────────────────────────
  const baselineIdx = Math.min(Math.max(parseInt(search.baseline ?? '0', 10) || 0, 0), Math.max(syntheticRuns.length - 1, 0))
  const tableBaseline: 'best' | 'worst' | number = search.tableBase === 'worst'
    ? 'worst'
    : search.tableBase !== undefined && search.tableBase !== 'best'
      ? Math.min(parseInt(search.tableBase, 10) || 0, syntheticRuns.length - 1)
      : 'best'
  const chartType: ChartType = (search.chart as ChartType) ?? 'line'
  const [sharedZoom, setSharedZoom] = useState(true)
  const [chartZoom, setChartZoom] = useState({ start: 0, end: 100 })
  const tableSortBy = (search.sort ?? 'order') as 'order' | 'name' | 'gasUsed' | 'avgValue' | `run-${number}`
  const tableSortDir = (search.sortDir === 'desc' ? 'desc' : 'asc') as 'asc' | 'desc'
  const testFilter = search.filter ?? ''
  const testFilterRegex = search.filterRegex === '1'

  // ─── Gas bucket filter ─────────────────────────────────────────
  const GAS_BUCKET_STEP = 30_000_000

  const selectedGasBuckets = useMemo(() => {
    if (!search.gasBuckets) return new Set<number>()
    return new Set(
      search.gasBuckets.split(',').map((s) => parseInt(s, 10) * 1_000_000).filter((n) => !isNaN(n)),
    )
  }, [search.gasBuckets])

  // Compute per-test gas from the first synthetic result for bucketing.
  const testGasMap = useMemo(() => {
    const map = new Map<string, number>()
    const result = syntheticRuns.find((r) => r.result)?.result
    if (!result) return map
    for (const [name, entry] of Object.entries(result.tests)) {
      const step = entry.steps?.test
      if (step) map.set(name, step.aggregated.gas_used_total)
    }
    return map
  }, [syntheticRuns])

  const availableGasBuckets = useMemo(() => {
    const buckets = new Set<number>()
    for (const gas of testGasMap.values()) {
      buckets.add(Math.round(gas / GAS_BUCKET_STEP) * GAS_BUCKET_STEP)
    }
    return [...buckets].sort((a, b) => a - b)
  }, [testGasMap, GAS_BUCKET_STEP])

  // ─── Combined test name filter ────────────────────────────────
  const testNameFilter = useMemo(() => {
    const textFn = (() => {
      if (!testFilter) return undefined
      if (testFilterRegex) {
        try {
          const re = new RegExp(testFilter, 'i')
          return (name: string) => re.test(name)
        } catch {
          return undefined
        }
      }
      return (name: string) => testNameMatches(name, testFilter)
    })()

    const needsGasFilter = selectedGasBuckets.size > 0
    const gasFn = needsGasFilter
      ? (name: string) => {
          const gas = testGasMap.get(name)
          if (gas === undefined) return false
          const bucket = Math.round(gas / GAS_BUCKET_STEP) * GAS_BUCKET_STEP
          return selectedGasBuckets.has(bucket)
        }
      : undefined

    if (!textFn && !gasFn) return undefined
    if (textFn && !gasFn) return textFn
    if (!textFn && gasFn) return gasFn
    return (name: string) => textFn!(name) && gasFn!(name)
  }, [testFilter, testFilterRegex, selectedGasBuckets, testGasMap, GAS_BUCKET_STEP])

  const hasResults = syntheticRuns.length >= 1 && syntheticRuns.every((r) => r.result !== null)

  // ─── Sticky bar ────────────────────────────────────────────────
  const sentinelRef = useRef<HTMLDivElement>(null)
  const [stickyVisible, setStickyVisible] = useState(false)

  useEffect(() => {
    const el = sentinelRef.current
    if (!el) return
    const observer = new IntersectionObserver(
      ([entry]) => setStickyVisible(!entry.isIntersecting),
      { threshold: 0 },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  // ─── Test detail modal ─────────────────────────────────────────
  const [selectedTest, setSelectedTest] = useState<string | null>(null)

  // Per-group individual results (not averaged) for the detail modal.
  const groupResultsForModal = useMemo(() => {
    if (!selectedTest) return []
    let offset = 0
    return groups.map((_, gi) => {
      const runIds = groupRuns[gi] ?? []
      const results: RunResult[] = []
      for (let ri = 0; ri < runIds.length; ri++) {
        const r = resultQueries[offset + ri]?.data
        if (r) results.push(r)
      }
      offset += runIds.length
      return results
    })
  }, [selectedTest, groups, groupRuns, resultQueries])

  const groupTimestampsForModal = useMemo(() => {
    return groupMatchedEntries.map((entries) =>
      entries.slice(0, sampleSize).map((e) => e.timestamp),
    )
  }, [groupMatchedEntries, sampleSize])

  return (
    <div className="flex flex-col gap-6">
      {/* Breadcrumb */}
      <div className="flex min-w-0 items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/runs" className="shrink-0 hover:text-gray-700 dark:hover:text-gray-300">
          Runs
        </Link>
        <span>/</span>
        {suiteHash && suite && (
          <>
            <Link
              to="/suites/$suiteHash"
              params={{ suiteHash }}
              className="flex min-w-0 items-center gap-1.5 hover:text-gray-700 dark:hover:text-gray-300"
            >
              <JDenticon value={suiteHash} size={16} className="shrink-0 rounded-xs" />
              <span className="truncate">{suite?.metadata?.labels?.name ?? suiteHash}</span>
            </Link>
            <span>/</span>
          </>
        )}
        <span className="shrink-0 text-gray-900 dark:text-gray-100">Group Compare</span>
      </div>

      {/* Group Builder (sentinel for sticky bar) */}
      <div ref={sentinelRef}>
      <GroupBuilder
        availableSuites={availableSuites}
        selectedSuite={suiteHash}
        onSuiteChange={setSuiteHash}
        suiteName={suite?.metadata?.labels?.name}
        groups={groups}
        onGroupsChange={setGroups}
        availableClients={availableClients}
        availableMetadataKeys={availableMetadataKeys}
        sampleSize={sampleSize}
        onSampleSizeChange={setSampleSize}
        aggMode={aggMode}
        onAggModeChange={setAggMode}
        groupRunCounts={groupRuns.map((ids) => ids.length)}
        groupMatchedRuns={groupMatchedEntries}
      />
      </div>

      {/* Sticky bar — appears when the group builder scrolls out of view */}
      {stickyVisible && hasResults && (
        <div className="fixed top-0 right-0 left-0 z-50 border-b border-gray-200 bg-white/95 backdrop-blur-sm dark:border-gray-700 dark:bg-gray-900/95">
          <div className="mx-auto flex max-w-7xl flex-col gap-1 px-4 py-2">
            <div className="flex items-center justify-center gap-4">
              {groups.map((group, gi) => {
                const metaStr = Object.entries(group.metadata).map(([k, v]) => `${k}=${v}`).join(', ')
                const runCount = groupRuns[gi]?.length ?? 0
                return (
                  <span key={gi} className="inline-flex items-center gap-1.5 rounded-sm bg-gray-100 px-2 py-0.5 text-xs/5 font-medium text-gray-700 dark:bg-gray-700 dark:text-gray-200">
                    <img src={`/img/clients/${group.client}.jpg`} alt={group.client} className="size-3.5 rounded-full object-cover" />
                    {metaStr || group.client}
                    <span className="font-mono text-gray-400 dark:text-gray-500" title={`${runCount} run${runCount === 1 ? '' : 's'} sampled`}>
                      ({runCount})
                    </span>
                  </span>
                )
              })}
              <span className="flex items-center gap-1 text-xs text-gray-400 dark:text-gray-500">
                {aggMode === 'avg' ? 'Average' : 'Median'} of
                <input
                  type="number"
                  min={1}
                  max={50}
                  value={sampleSize}
                  onChange={(e) => setSampleSize(Math.max(1, Math.min(50, parseInt(e.target.value, 10) || 5)))}
                  title="Sample size — latest runs per group"
                  className="w-12 rounded-xs border border-gray-300 bg-white px-1 py-0.5 text-center text-xs/5 text-gray-700 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
                />
              </span>
            </div>
            <div className="flex items-center justify-center gap-4 text-xs/5 text-gray-500 dark:text-gray-400">
              <div className="flex items-center gap-1.5">
                <span>Filter:</span>
                <input
                  type="text"
                  placeholder={testFilterRegex ? 'Regex...' : 'Filter or e.g. opcode:ORIGIN'}
                  title={testFilterRegex
                    ? 'Regex against the raw test name.'
                    : TEST_FILTER_HINT}
                  value={testFilter}
                  onChange={(e) => updateSearch({ filter: e.target.value || undefined })}
                  className={clsx(
                    'w-36 rounded-xs border bg-white px-2 py-0.5 text-xs/5 placeholder-gray-400 focus:outline-hidden focus:ring-1 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500',
                    'border-gray-300 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-600',
                  )}
                />
                <button
                  onClick={() => updateSearch({ filterRegex: testFilterRegex ? undefined : '1' })}
                  title={testFilterRegex ? 'Regex mode' : 'Text mode'}
                  className={clsx(
                    'rounded-xs px-1 py-0.5 font-mono text-xs/5 transition-colors',
                    testFilterRegex
                      ? 'bg-blue-500 text-white'
                      : 'border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                  )}
                >
                  .*
                </button>
              </div>
              {availableGasBuckets.length > 1 && (
                <div className="flex items-center gap-1.5">
                  <span>Gas:</span>
                  <div className="flex flex-wrap gap-1">
                    <button
                      onClick={() => updateSearch({ gasBuckets: undefined })}
                      className={clsx(
                        'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                        selectedGasBuckets.size === 0
                          ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                          : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                      )}
                    >
                      All
                    </button>
                    {availableGasBuckets.map((bucket) => {
                      const isSelected = selectedGasBuckets.has(bucket)
                      return (
                        <button
                          key={bucket}
                          onClick={() => {
                            const next = new Set(selectedGasBuckets)
                            if (isSelected) next.delete(bucket); else next.add(bucket)
                            const sorted = [...next].sort((a, b) => a - b)
                            updateSearch({ gasBuckets: sorted.length > 0 ? sorted.map((v) => String(v / 1_000_000)).join(',') : undefined })
                          }}
                          className={clsx(
                            'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                            isSelected
                              ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                              : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                          )}
                        >
                          {Math.round(bucket / 1_000_000)}M
                        </button>
                      )
                    })}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {isLoading && allRunIds.length > 0 && (
        <LoadingState message={`Loading results for ${allRunIds.length} runs across ${groups.length} groups...`} />
      )}

      {/* Comparison results */}
      {hasResults && (
        <>
          <div className="flex flex-wrap items-center gap-4 text-xs/5 text-gray-500 dark:text-gray-400">
            <div className="flex items-center gap-1.5">
              <span>Aggregation:</span>
              {(['avg', 'median'] as const).map((m) => (
                <button
                  key={m}
                  onClick={() => setAggMode(m)}
                  className={clsx(
                    'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                    aggMode === m
                      ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                      : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                  )}
                >
                  {m === 'avg' ? 'Average' : 'Median'}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-1.5">
              <span>Chart:</span>
              <div className="flex gap-1">
                {CHART_TYPE_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    onClick={() => updateSearch({ chart: opt.value === 'line' ? undefined : opt.value })}
                    className={clsx(
                      'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                      chartType === opt.value
                        ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                        : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                    )}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex items-center gap-1.5">
              <span>Shared Zoom:</span>
              <button
                onClick={() => setSharedZoom(!sharedZoom)}
                className={clsx(
                  'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                  sharedZoom
                    ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                )}
              >
                {sharedZoom ? 'On' : 'Off'}
              </button>
            </div>
            <div className="flex items-center gap-1.5">
              <span>Filter:</span>
              <input
                type="text"
                placeholder={testFilterRegex ? 'Regex pattern...' : 'Filter… or e.g. opcode:ORIGIN'}
                title={testFilterRegex ? 'Regex against the raw test name.' : TEST_FILTER_HINT}
                value={testFilter}
                onChange={(e) => updateSearch({ filter: e.target.value || undefined })}
                className={clsx(
                  'w-36 rounded-xs border bg-white px-2 py-0.5 text-xs/5 placeholder-gray-400 focus:outline-hidden focus:ring-1 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500',
                  testFilterRegex && testFilter && (() => { try { new RegExp(testFilter); return false } catch { return true } })()
                    ? 'border-red-400 focus:border-red-500 focus:ring-red-500 dark:border-red-500'
                    : 'border-gray-300 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-600',
                )}
              />
              <button
                onClick={() => updateSearch({ filterRegex: testFilterRegex ? undefined : '1' })}
                title={testFilterRegex ? 'Regex mode (click to switch to text)' : 'Text mode (click to switch to regex)'}
                className={clsx(
                  'rounded-xs px-1.5 py-0.5 font-mono text-xs/5 transition-colors',
                  testFilterRegex
                    ? 'bg-blue-500 text-white'
                    : 'border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                )}
              >
                .*
              </button>
            </div>
            {availableGasBuckets.length > 1 && (
              <div className="flex items-center gap-1.5">
                <span>Gas:</span>
                <div className="flex flex-wrap gap-1">
                  <button
                    onClick={() => updateSearch({ gasBuckets: undefined })}
                    className={clsx(
                      'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                      selectedGasBuckets.size === 0
                        ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                        : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                    )}
                  >
                    All
                  </button>
                  {availableGasBuckets.map((bucket) => {
                    const isSelected = selectedGasBuckets.has(bucket)
                    return (
                      <button
                        key={bucket}
                        onClick={() => {
                          const next = new Set(selectedGasBuckets)
                          if (isSelected) next.delete(bucket); else next.add(bucket)
                          const sorted = [...next].sort((a, b) => a - b)
                          updateSearch({ gasBuckets: sorted.length > 0 ? sorted.map((v) => String(v / 1_000_000)).join(',') : undefined })
                        }}
                        className={clsx(
                          'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                          isSelected
                            ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                            : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                        )}
                      >
                        {Math.round(bucket / 1_000_000)}M
                      </button>
                    )
                  })}
                </div>
              </div>
            )}
          </div>

          <FacetPanel
            testNames={[...testGasMap.keys()]}
            query={testFilter}
            onToggle={(term) => updateSearch({ filter: toggleSearchTerm(testFilter, term) || undefined })}
          />

          <MetricsComparison
            runs={syntheticRuns}
            stepFilter={stepFilter}
            baselineIdx={baselineIdx}
            onBaselineChange={(idx) => updateSearch({ baseline: idx > 0 ? String(idx) : undefined })}
            labelMode="instance-id" // shows the group label we set
            testNameFilter={testNameFilter}
          />

          <MGasComparisonChart
            runs={syntheticRuns}
            suiteTests={suite?.tests}
            stepFilter={stepFilter}
            labelMode="instance-id"
            testNameFilter={testNameFilter}
            zoomRange={sharedZoom ? chartZoom : undefined}
            onZoomChange={sharedZoom ? setChartZoom : undefined}
            chartType={chartType}
            onTestClick={setSelectedTest}
          />

          {syntheticRuns.length >= 2 && (
            <PercentageDiffChart
              runs={syntheticRuns}
              suiteTests={suite?.tests}
              stepFilter={stepFilter}
              baselineIdx={baselineIdx}
              onBaselineChange={(idx) => updateSearch({ baseline: idx > 0 ? String(idx) : undefined })}
              labelMode="instance-id"
              diffFilter={search.diffFilter === 'faster' || search.diffFilter === 'slower' ? search.diffFilter : 'all'}
              onDiffFilterChange={(val) => updateSearch({ diffFilter: val === 'all' ? undefined : val })}
              testNameFilter={testNameFilter}
              zoomRange={sharedZoom ? chartZoom : undefined}
              onZoomChange={sharedZoom ? setChartZoom : undefined}
              chartType={chartType}
              onTestClick={setSelectedTest}
            />
          )}

          <CVComparisonChart
            runs={syntheticRuns}
            suiteTests={suite?.tests}
            labelMode="instance-id"
            testNameFilter={testNameFilter}
            zoomRange={sharedZoom ? chartZoom : undefined}
            onZoomChange={sharedZoom ? setChartZoom : undefined}
            chartType={chartType}
            varianceByRunIndex={varianceMap}
            onTestClick={setSelectedTest}
          />

          <ResourceComparisonCharts
            runs={syntheticRuns}
            labelMode="instance-id"
            testNameFilter={testNameFilter}
            suiteTests={suite?.tests}
            zoomRange={sharedZoom ? chartZoom : undefined}
            onZoomChange={sharedZoom ? setChartZoom : undefined}
            chartType={chartType}
            onTestClick={setSelectedTest}
          />

          <TestComparisonTable
            runs={syntheticRuns}
            suiteTests={suite?.tests}
            stepFilter={stepFilter}
            labelMode="instance-id"
            tableBaseline={tableBaseline}
            onTableBaselineChange={(val) => updateSearch({ tableBase: val === 'best' ? undefined : String(val) })}
            sortBy={tableSortBy}
            sortDir={tableSortDir}
            onSortChange={(col, dir) => updateSearch({ sort: col === 'order' ? undefined : col, sortDir: dir === 'asc' ? undefined : dir })}
            testNameFilter={testNameFilter}
            onTestClick={setSelectedTest}
          />
        </>
      )}

      {!isLoading && groups.length >= 1 && allRunIds.length > 0 && !hasResults && (
        <div className="rounded-sm bg-yellow-50 p-4 text-sm/6 text-yellow-800 dark:bg-yellow-900/20 dark:text-yellow-300">
          Not enough result data to compare. Make sure the selected runs have completed with results.
        </div>
      )}

      {/* Test detail modal — shows per-run breakdown for a single test */}
      {selectedTest && (
        <TestDetailModal
          testName={selectedTest}
          testOrder={suite?.tests ? suite.tests.findIndex((t) => t.name === selectedTest) + 1 : undefined}
          groups={groups}
          groupResults={groupResultsForModal}
          groupTimestamps={groupTimestampsForModal}
          groupRunIds={groupRuns}
          stepFilter={stepFilter}
          sampleSize={sampleSize}
          searchQuery={testFilter}
          onChipFilterToggle={(term) => updateSearch({ filter: toggleSearchTerm(testFilter, term) || undefined })}
          onClose={() => setSelectedTest(null)}
        />
      )}
    </div>
  )
}
