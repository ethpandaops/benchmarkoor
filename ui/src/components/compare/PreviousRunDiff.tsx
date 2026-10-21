import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { GitCompare } from 'lucide-react'
import type { RunConfig } from '@/api/types'
import { fetchRunConfig, runConfigQueryKey, useRunConfig } from '@/api/hooks/useRunConfig'
import { useTestHistory } from '@/api/hooks/useTestHistory'
import { ConfigDiff } from '@/components/compare/ConfigDiff'
import { SegmentedControl } from '@/components/shared/SegmentedControl'
import { Spinner } from '@/components/shared/Spinner'
import { MAX_SAME_HOST_LOOKBACK, sameHostCandidates } from '@/utils/regressions'

type Reference = 'previous' | 'sameHost'

interface PreviousRunDiffProps {
  suiteHash: string
  testName: string
  client: string
  // The run to explain, and the last run of the same client before it.
  runId: string
  runStart: number
  previousRunId: string
  // The steps search param, passed on to the compare page.
  steps?: string
  // The runs per client of the test history that holds runId. The search
  // for a run on the same host reads this history.
  historyRuns: number
  // The column labels of the two runs, before their start times.
  labels?: [before: string, after: string]
}

function formatRunStart(runStart: number): string {
  return new Date(runStart * 1000).toLocaleString()
}

// PreviousRunDiff compares the config of a run with the last run of the same
// client before it, and with the last run before it on the same host. A pool
// of runners can differ a little in hardware, so the same-host diff shows a
// config change without the hardware noise.
export function PreviousRunDiff({
  suiteHash,
  testName,
  client,
  runId,
  runStart,
  previousRunId,
  steps,
  historyRuns,
  labels = ['Before', 'Since'],
}: PreviousRunDiffProps) {
  const queryClient = useQueryClient()
  const [reference, setReference] = useState<Reference>('previous')
  const [onlyDifferences, setOnlyDifferences] = useState(true)

  const sinceRunId = runId
  const lastRunId = previousRunId
  const since = useRunConfig(sinceRunId)
  const last = useRunConfig(lastRunId)

  const host = since.data?.system.hostname
  const lastOnSameHost = !!since.data && !!last.data && last.data.system.hostname === host

  // Search older runs only when the last run before the step ran elsewhere.
  const needsSearch = !!since.data && !!last.data && !lastOnSameHost
  const history = useTestHistory(suiteHash, needsSearch ? testName : undefined, historyRuns)
  const candidates = history.data
    ? sameHostCandidates(history.data.durations, client, sinceRunId, lastRunId)
    : undefined

  const sameHost = useQuery({
    queryKey: ['sameHostRun', sinceRunId, host, candidates],
    queryFn: async (): Promise<{ runId: string; config: RunConfig } | null> => {
      // One config at a time, newest first: the nearest match is usually a
      // few runs back, so this reads only a few configs.
      for (const runId of candidates!) {
        let config: RunConfig
        try {
          config = await queryClient.fetchQuery({
            queryKey: runConfigQueryKey(runId),
            queryFn: () => fetchRunConfig(runId),
          })
        } catch {
          // A run without a config cannot match; try the next one.
          continue
        }
        if (config.system.hostname === host) return { runId, config }
      }
      return null
    },
    enabled: needsSearch && candidates !== undefined,
  })

  if (since.isLoading || last.isLoading) {
    return (
      <div className="flex items-center gap-2 px-4 py-3 text-xs/5 text-gray-500 dark:text-gray-400">
        <Spinner size="sm" /> Loading the run configs...
      </div>
    )
  }

  if (!since.data || !last.data) {
    return (
      <p className="px-4 py-3 text-xs/5 text-gray-500 dark:text-gray-400">
        The config of {since.data ? 'the previous run' : 'this run'} is not available.
      </p>
    )
  }

  const searching = needsSearch && (history.isLoading || sameHost.isLoading)
  const showSameHost = reference === 'sameHost' && needsSearch
  const otherRun =
    showSameHost && sameHost.data
      ? { runId: sameHost.data.runId, config: sameHost.data.config }
      : { runId: lastRunId, config: last.data }

  return (
    <div className="flex flex-col gap-3 px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {needsSearch ? (
          <SegmentedControl
            ariaLabel="Compare with"
            value={reference}
            options={[
              { value: 'previous', label: 'Previous run' },
              { value: 'sameHost', label: `Previous run on ${host}` },
            ]}
            onChange={setReference}
          />
        ) : (
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">
            The previous run is on the same host ({host}).
          </span>
        )}
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex items-center gap-2 text-xs/5 text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              checked={onlyDifferences}
              onChange={(e) => setOnlyDifferences(e.target.checked)}
              className="rounded-xs border-gray-300 dark:border-gray-600"
            />
            Only differences
          </label>
          {!(showSameHost && !sameHost.data) && (
            <Link
              to="/compare"
              search={{ runs: `${otherRun.runId},${sinceRunId}`, steps }}
              className="inline-flex items-center gap-1 text-xs/5 font-medium text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
            >
              <GitCompare className="size-4" />
              Compare runs
            </Link>
          )}
        </div>
      </div>

      {showSameHost && searching ? (
        <div className="flex items-center gap-2 text-xs/5 text-gray-500 dark:text-gray-400">
          <Spinner size="sm" /> Looking for a run on {host}...
        </div>
      ) : showSameHost && !sameHost.data ? (
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          No run on {host} in the {MAX_SAME_HOST_LOOKBACK} runs before this run.
        </p>
      ) : (
        <ConfigDiff
          runs={[
            { runId: otherRun.runId, config: otherRun.config, result: null, index: 0 },
            { runId: sinceRunId, config: since.data, result: null, index: 1 },
          ]}
          labelMode="default"
          labels={[
            `${labels[0]} · ${formatRunStart(otherRun.config.timestamp)}`,
            `${labels[1]} · ${formatRunStart(runStart)}`,
          ]}
          onlyDifferences={onlyDifferences}
        />
      )}
    </div>
  )
}
