import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Flame, Loader } from 'lucide-react'
import { DEFAULT_THRESHOLD, MAX_THRESHOLD, MIN_THRESHOLD } from '@/utils/perfThreshold'
import type { LiveRun, LiveTestStats, TestEntry, AggregatedStats } from '@/api/types'
import { ClientStat } from '@/components/shared/ClientStat'
import { JDenticon } from '@/components/shared/JDenticon'
import { RunConfiguration } from '@/components/run-detail/RunConfiguration'
import { MetadataLabels } from '@/components/run-detail/MetadataLabels'
import { GitHubSection } from '@/components/run-detail/GitHubSection'
import { ClientRunsStrip } from '@/components/run-detail/ClientRunsStrip'
import { TestHeatmap, type SortMode, type GroupMode } from '@/components/run-detail/TestHeatmap'
import { LiveRunLogPanel } from '@/components/run-detail/LiveRunLogPanel'
import { useSuite } from '@/api/hooks/useSuite'
import { useIndex } from '@/api/hooks/useIndex'
import { formatTimestamp } from '@/utils/date'
import { formatNumber } from '@/utils/format'
import { computeLiveEta, formatEtaShort, formatEtaTooltip, formatHoursMinutes, formatClockHoursMinutes } from '@/components/runs/liveEta'
import { DEFAULT_INDEX_STEP_FILTER } from '@/api/types'

interface LiveRunDetailViewProps {
  run: LiveRun
}

/**
 * LiveRunDetailView renders a view mirroring RunDetailPage as closely as
 * possible, but sourced from the live ingest snapshot's payload instead
 * of on-disk config.json / result.json. Used when a run is still in
 * progress and its files haven't landed on the storage backend yet.
 */
export function LiveRunDetailView({ run }: LiveRunDetailViewProps) {
  const { data: suite } = useSuite(run.suite_hash ?? '')
  const { data: index } = useIndex()

  const cfg = run.config
  const total = run.tests_total || 0
  const passed = run.tests_passed || 0
  const failed = run.tests_failed || 0
  const completed = passed + failed
  const progress = total > 0 ? Math.min(100, (completed / total) * 100) : 0

  // Prefer values from the reported config.json (richer) and fall back to
  // the ingest report's scalars when we don't have a config yet.
  const clientName = cfg?.instance.client ?? run.client ?? ''
  const instanceID = cfg?.instance.id ?? run.instance_id ?? ''
  const rollbackStrategy = cfg?.instance.rollback_strategy ?? run.rollback_strategy
  const startTimestamp = cfg?.timestamp ?? run.timestamp
  const labels = cfg?.metadata?.labels ?? run.metadata

  const eta = computeLiveEta(startTimestamp, completed, total)
  const etaShort = formatEtaShort(eta)
  const etaTooltip = formatEtaTooltip(eta)

  // Live MGas/s estimate from completed tests' aggregated `test`-step gas.
  // Mirrors the formula used in RunDetailPage: gas_used (gwei units) * 1000
  // divided by gas_used_duration_ns yields gas-per-second in millions.
  const totalGasUsed = run.total_gas_used ?? 0
  const totalGasUsedDurationNs = run.total_gas_used_duration_ns ?? 0
  const mgasPerSec =
    totalGasUsedDurationNs > 0 ? (totalGasUsed * 1000) / totalGasUsedDurationNs : undefined

  const clientRuns = useMemo(() => {
    if (!index || !run.suite_hash || !clientName) return []
    return index.entries.filter(
      (r) => r.suite_hash === run.suite_hash && r.instance.client === clientName,
    )
  }, [index, run.suite_hash, clientName])

  // Per-test gas data for the live Performance Heatmap. Comes from the
  // same snapshot payload that drives the rest of this view, so heatmap
  // tiles stay consistent with the aggregate counters above. The
  // heatmap itself fills in faded tiles for every suite test that hasn't
  // been processed yet, so we render it as soon as we have *either* the
  // suite or some completed tests.
  const heatmapTests = useMemo(
    () => (run.tests ? liveTestsToHeatmapEntries(run.tests) : {}),
    [run.tests],
  )
  const showHeatmap =
    Object.keys(heatmapTests).length > 0 || (suite?.tests?.length ?? 0) > 0

  // Heuristic for "the test the runner is currently working on": the
  // first suite test (in execution order) that hasn't been reported as
  // completed yet. Only meaningful while the run is still in flight.
  const inProgressKey = useMemo(() => {
    if (run.status !== 'running' || !suite?.tests) return undefined

    const completed = run.tests ?? {}
    for (const t of suite.tests) {
      if (!completed[t.name]) return t.name
    }

    return undefined
  }, [run.status, run.tests, suite])

  // Local state for the heatmap controls. Live view doesn't need URL
  // persistence (unlike RunDetailPage), so a plain useState is enough.
  const [heatmapSort, setHeatmapSort] = useState<SortMode>('order')
  const [heatmapGroup, setHeatmapGroup] = useState<GroupMode>('none')
  const [heatmapThreshold, setHeatmapThreshold] = useState<number | undefined>(undefined)

  // Smoothly tween the live counters between snapshots so users see
  // numbers ticking up rather than jumping. Integer counters are
  // rounded at the render site; the MGas/s float is formatted directly.
  const animatedPassed = useAnimatedNumber(passed)
  const animatedFailed = useAnimatedNumber(failed)
  const animatedCompleted = useAnimatedNumber(completed)
  const animatedProgress = useAnimatedNumber(progress)
  const animatedMgas = useAnimatedNumber(mgasPerSec ?? 0)

  // Pulse the "Live" badge whenever a new snapshot arrives (i.e.
  // last_reported_at advances). Confirms data freshness and the
  // runner→API link is healthy without an extra widget.
  const prevReportRef = useRef(run.last_reported_at)
  const [reportPulse, setReportPulse] = useState(false)

  useEffect(() => {
    if (prevReportRef.current === run.last_reported_at) return

    prevReportRef.current = run.last_reported_at
    // One-shot animation signal — setState here is intentional, the
    // effect runs only when last_reported_at advances and emits one
    // render to apply the pulse class plus one to clear it.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setReportPulse(true)

    const t = setTimeout(() => setReportPulse(false), 700)
    return () => clearTimeout(t)
  }, [run.last_reported_at])

  return (
    <div className="flex flex-col gap-6">
      {/* Breadcrumb (matches RunDetailPage). */}
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm/6 text-gray-500 dark:text-gray-400">
        <div className="flex min-w-0 items-center gap-2">
          <Link to="/suites" className="shrink-0 hover:text-gray-700 dark:hover:text-gray-300">
            Suites
          </Link>
          <span>/</span>
          {run.suite_hash && (
            <>
              <Link
                to="/suites/$suiteHash"
                params={{ suiteHash: run.suite_hash }}
                className={`flex min-w-0 items-center gap-1.5 hover:text-gray-700 dark:hover:text-gray-300${suite?.metadata?.labels?.name ? '' : ' font-mono'}`}
              >
                <JDenticon value={run.suite_hash} size={16} className="shrink-0 rounded-xs" />
                <span className="truncate">{suite?.metadata?.labels?.name ?? run.suite_hash}</span>
              </Link>
              <span>/</span>
            </>
          )}
          <span className="truncate font-mono text-gray-900 dark:text-gray-100">{run.run_id}</span>
        </div>
        <span
          className="sm:ml-auto inline-flex items-center gap-2 rounded-sm bg-blue-100 px-2 py-0.5 text-xs/5 font-medium text-blue-800 dark:bg-blue-900/40 dark:text-blue-200"
          style={reportPulse ? { animation: 'liveBadgePulse 0.7s ease-out' } : undefined}
        >
          <Loader className="size-3.5 animate-spin" />
          Live — last report {formatRelativeAge(run.last_reported_at)}
        </span>
      </div>

      {/* Recent runs strip, same as the real detail page. */}
      {clientRuns.length > 0 && (
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">
            <ClientRunsStrip
              runs={clientRuns}
              currentRunId={run.run_id}
              stepFilter={DEFAULT_INDEX_STEP_FILTER}
            />
          </div>
        </div>
      )}

      {/* Top stat cards. Calls / Test Duration cards don't exist for a live
          run (they require result.json), but MGas/s can be estimated from a
          running total of completed tests' gas aggregates so we surface it
          here. The rest of the row is the "In progress" progress bar. */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
        {clientName && (
          <ClientStat
            client={clientName}
            runId={instanceID}
            rollbackStrategy={rollbackStrategy}
          />
        )}
        <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
          <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Tests</p>
          <p className="mt-1 flex items-center gap-2 text-2xl/8 font-semibold">
            <span className="text-gray-900 dark:text-gray-100">{total}</span>
            <span className="text-gray-400 dark:text-gray-500">/</span>
            <span className="text-green-600 dark:text-green-400 tabular-nums">
              {Math.round(animatedPassed)}
            </span>
            {failed > 0 && (
              <>
                <span className="text-gray-400 dark:text-gray-500">/</span>
                <span className="text-red-600 dark:text-red-400 tabular-nums">
                  {Math.round(animatedFailed)}
                </span>
              </>
            )}
          </p>
          <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">Started at</p>
          <p className="text-xs/5 text-gray-900 dark:text-gray-100">
            {formatTimestamp(startTimestamp)}
          </p>
        </div>
        <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
          <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">MGas/s</p>
          <p className="mt-1 text-2xl/8 font-semibold text-gray-900 dark:text-gray-100 tabular-nums">
            {mgasPerSec !== undefined ? animatedMgas.toFixed(2) : '-'}
          </p>
          <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
            {mgasPerSec !== undefined
              ? `${(totalGasUsed / 1_000_000).toFixed(2)} MGas`
              : 'Waiting for first completed test'}
          </p>
          <p className="text-xs/5 text-gray-500 dark:text-gray-400">Test step, running average</p>
        </div>
        <div className="col-span-2 rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
          <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Progress</p>
          <p className="mt-1 flex items-baseline gap-3 text-2xl/8 font-semibold text-gray-900 dark:text-gray-100">
            <span className="tabular-nums">
              {total > 0 ? `${animatedProgress.toFixed(1)}%` : '-'}
            </span>
            {etaShort && (
              <span
                className="text-sm/6 font-normal text-gray-500 dark:text-gray-400"
                title={etaTooltip}
              >
                {etaShort}
              </span>
            )}
          </p>
          <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400" title={etaTooltip}>
            <span className="tabular-nums">{formatNumber(Math.round(animatedCompleted))}</span> of{' '}
            {formatNumber(total)} tests complete · Elapsed {formatHoursMinutes(eta.elapsedSec)}
            {eta.etaAtMs !== undefined && <> · ETA {formatClockHoursMinutes(new Date(eta.etaAtMs))}</>}
          </p>
          <div className="mt-2 h-1.5 w-full overflow-hidden rounded-sm bg-gray-200 dark:bg-gray-700">
            <div
              className="relative h-full overflow-hidden bg-blue-500 transition-all dark:bg-blue-400"
              style={{ width: `${animatedProgress}%` }}
            >
              {/* Shimmer sweep — only while the run is in flight, so a
                  completed/canceled run renders a static bar. */}
              {run.status === 'running' && (
                <span
                  aria-hidden="true"
                  className="absolute inset-0 bg-linear-to-r from-transparent via-white/40 to-transparent"
                  style={{ animation: 'liveShimmer 1.6s ease-in-out infinite' }}
                />
              )}
            </div>
          </div>
          <p className="mt-3 text-xs/5 text-gray-500 dark:text-gray-400">
            Full per-test results (calls, durations) will appear here once the run completes and gets uploaded.
          </p>
        </div>
      </div>

      <MetadataLabels labels={labels} />

      <GitHubSection labels={labels} />

      {/* Configuration panel — shown once the runner has reported its
          config.json via an ingest snapshot. Rendered before the heatmap
          so users see what's actually being run before drilling into the
          per-test results below. */}
      {cfg && (
        <RunConfiguration
          instance={cfg.instance}
          system={cfg.system}
          startBlock={cfg.start_block}
          metadata={cfg.metadata}
          benchmarkoorVersion={cfg.benchmarkoor_version}
        />
      )}

      {/* Live log stream — collapsed by default; opening the panel is
          what signals the runner to start pushing log bytes. */}
      <LiveRunLogPanel
        runId={run.run_id}
        client={clientName || undefined}
        instanceId={instanceID || undefined}
      />

      {/* Live Performance Heatmap — fed by the per-test gas data the
          runner ships in every snapshot. Renders as soon as we have
          either suite info (un-processed tiles) or completed tests. */}
      {showHeatmap && (
        <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
          <div className="flex flex-wrap items-center gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
            <h3 className="flex items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
              <Flame className="size-4 text-gray-400 dark:text-gray-500" />
              Performance Heatmap
            </h3>
            <div className="ml-auto flex items-center gap-2 text-xs/5 text-gray-500 dark:text-gray-400">
              <span>Slow threshold:</span>
              <input
                type="range"
                min={MIN_THRESHOLD}
                max={MAX_THRESHOLD}
                value={heatmapThreshold ?? DEFAULT_THRESHOLD}
                onChange={(e) => setHeatmapThreshold(Number(e.target.value))}
                className="h-1.5 w-24 cursor-pointer appearance-none rounded-full bg-gray-200 accent-blue-500 dark:bg-gray-700"
              />
              <input
                type="number"
                min={MIN_THRESHOLD}
                max={MAX_THRESHOLD}
                value={heatmapThreshold ?? DEFAULT_THRESHOLD}
                onChange={(e) => setHeatmapThreshold(Number(e.target.value))}
                className="w-16 rounded-sm border border-gray-300 bg-white px-1.5 py-0.5 text-center text-xs/5 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              />
              <span>MGas/s</span>
              {(heatmapThreshold ?? DEFAULT_THRESHOLD) !== DEFAULT_THRESHOLD && (
                <button
                  onClick={() => setHeatmapThreshold(DEFAULT_THRESHOLD)}
                  className="text-xs/5 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  Reset
                </button>
              )}
            </div>
          </div>
          <div className="p-4">
            <TestHeatmap
              tests={heatmapTests}
              suiteTests={suite?.tests}
              runId={run.run_id}
              suiteHash={run.suite_hash}
              stepFilter={DEFAULT_INDEX_STEP_FILTER}
              sortMode={heatmapSort}
              groupMode={heatmapGroup}
              threshold={heatmapThreshold}
              inProgressTestKey={inProgressKey}
              onSortModeChange={setHeatmapSort}
              onGroupModeChange={setHeatmapGroup}
            />
          </div>
        </div>
      )}
    </div>
  )
}

// liveTestsToHeatmapEntries adapts the live per-test gas map into the
// Record<string, TestEntry> shape that the existing TestHeatmap consumes.
// Only the `test` step's aggregated gas/duration is populated — that's
// all the heatmap reads when DEFAULT_INDEX_STEP_FILTER is active. Failed
// tests carry zero gas (rendered as no-data tiles by the heatmap).
function liveTestsToHeatmapEntries(tests: Record<string, LiveTestStats>): Record<string, TestEntry> {
  const out: Record<string, TestEntry> = {}

  for (const [name, t] of Object.entries(tests)) {
    out[name] = {
      dir: '',
      steps: {
        test: { aggregated: liveTestStatsToAggregated(t) },
      },
    }
  }

  return out
}

function liveTestStatsToAggregated(t: LiveTestStats): AggregatedStats {
  const gasUsed = t.gas_used ?? 0
  const gasUsedDurationNs = t.gas_used_duration_ns ?? 0

  return {
    time_total: gasUsedDurationNs,
    gas_used_total: gasUsed,
    gas_used_time_total: gasUsedDurationNs,
    success: t.passed ? 1 : 0,
    fail: t.passed ? 0 : 1,
    msg_count: 0,
    method_stats: { times: {}, mgas_s: {} },
  }
}

// useAnimatedNumber tweens between successive `target` values via
// requestAnimationFrame. Returns the in-flight value; callers round /
// format as needed. Mid-animation target changes restart from the
// currently-displayed value so we never visually skip backwards. Used
// by LiveRunDetailView so the live counters glide instead of jumping.
const ANIMATE_DURATION_MS = 450

function useAnimatedNumber(target: number): number {
  const [displayed, setDisplayed] = useState(target)
  // displayedRef tracks the in-flight tween value so a target change
  // mid-animation can resume from where we are rather than restarting.
  // Only written inside the rAF step (and seeded by useState's initial
  // value), never during render.
  const displayedRef = useRef(target)

  useEffect(() => {
    const start = displayedRef.current
    if (start === target) return

    const delta = target - start
    const startTime = performance.now()
    let raf = 0

    const step = (now: number) => {
      const elapsed = now - startTime
      const t = Math.min(1, elapsed / ANIMATE_DURATION_MS)
      // ease-out cubic: fast start, soft landing.
      const eased = 1 - Math.pow(1 - t, 3)
      const value = start + delta * eased
      displayedRef.current = value
      // One-shot animation tick — the cascade is bounded by `t < 1`.
       
      setDisplayed(value)

      if (t < 1) raf = requestAnimationFrame(step)
    }

    raf = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf)
  }, [target])

  return displayed
}

function formatRelativeAge(isoTimestamp: string): string {
  const then = new Date(isoTimestamp).getTime()
  const now = Date.now()
  const diffSec = Math.max(0, Math.round((now - then) / 1000))

  if (diffSec < 10) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`

  const diffMin = Math.round(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`

  const diffHr = Math.round(diffMin / 60)
  return `${diffHr}h ago`
}
