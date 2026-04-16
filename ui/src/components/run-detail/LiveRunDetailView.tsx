import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Flame, Loader } from 'lucide-react'
import type { LiveRun, LiveTestStats, TestEntry, AggregatedStats } from '@/api/types'
import { ClientStat } from '@/components/shared/ClientStat'
import { JDenticon } from '@/components/shared/JDenticon'
import { RunConfiguration } from '@/components/run-detail/RunConfiguration'
import { MetadataLabels } from '@/components/run-detail/MetadataLabels'
import { GitHubSection } from '@/components/run-detail/GitHubSection'
import { ClientRunsStrip } from '@/components/run-detail/ClientRunsStrip'
import { TestHeatmap, type SortMode, type GroupMode } from '@/components/run-detail/TestHeatmap'
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

  // Local state for the heatmap controls. Live view doesn't need URL
  // persistence (unlike RunDetailPage), so a plain useState is enough.
  const [heatmapSort, setHeatmapSort] = useState<SortMode>('order')
  const [heatmapGroup, setHeatmapGroup] = useState<GroupMode>('none')
  const [heatmapThreshold, setHeatmapThreshold] = useState<number | undefined>(undefined)

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
        <span className="sm:ml-auto inline-flex items-center gap-2 rounded-sm bg-blue-100 px-2 py-0.5 text-xs/5 font-medium text-blue-800 dark:bg-blue-900/40 dark:text-blue-200">
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
            <span className="text-green-600 dark:text-green-400">{passed}</span>
            {failed > 0 && (
              <>
                <span className="text-gray-400 dark:text-gray-500">/</span>
                <span className="text-red-600 dark:text-red-400">{failed}</span>
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
          <p className="mt-1 text-2xl/8 font-semibold text-gray-900 dark:text-gray-100">
            {mgasPerSec !== undefined ? mgasPerSec.toFixed(2) : '-'}
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
            <span>{total > 0 ? `${progress.toFixed(1)}%` : '-'}</span>
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
            {formatNumber(completed)} of {formatNumber(total)} tests complete · Elapsed {formatHoursMinutes(eta.elapsedSec)}
            {eta.etaAtMs !== undefined && <> · ETA {formatClockHoursMinutes(new Date(eta.etaAtMs))}</>}
          </p>
          <div className="mt-2 h-1.5 w-full overflow-hidden rounded-sm bg-gray-200 dark:bg-gray-700">
            <div
              className="h-full bg-blue-500 transition-all dark:bg-blue-400"
              style={{ width: `${progress}%` }}
            />
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
        />
      )}

      {/* Live Performance Heatmap — fed by the per-test gas data the
          runner ships in every snapshot. Renders as soon as we have
          either suite info (un-processed tiles) or completed tests. */}
      {showHeatmap && (
        <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
          <div className="mb-4 flex items-center justify-between">
            <h3 className="flex items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
              <Flame className="size-4 text-gray-400 dark:text-gray-500" />
              Performance Heatmap (Live)
            </h3>
          </div>
          <TestHeatmap
            tests={heatmapTests}
            suiteTests={suite?.tests}
            runId={run.run_id}
            suiteHash={run.suite_hash}
            stepFilter={DEFAULT_INDEX_STEP_FILTER}
            sortMode={heatmapSort}
            groupMode={heatmapGroup}
            threshold={heatmapThreshold}
            onSortModeChange={setHeatmapSort}
            onGroupModeChange={setHeatmapGroup}
            onThresholdChange={setHeatmapThreshold}
          />
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
