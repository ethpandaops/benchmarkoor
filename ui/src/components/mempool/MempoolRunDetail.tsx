import { useMemo, useState } from 'react'
import type { RunConfig } from '@/api/types'
import { useMempoolResult } from '@/api/hooks/useMempoolResult'
import { SegmentedControl } from '@/components/shared/SegmentedControl'
import { sortScenarios } from './mempoolUtils'
import { filterScenarios, type ScenarioFilter } from './runOverview'
import { clientVersion } from './issueMarkdown'
import { ScenarioCard, type IssueContext } from './ScenarioCard'
import { MempoolComparison } from './MempoolComparison'
import { InvariantsPanel } from './InvariantsPanel'
import { ProblemsBanner } from './ProblemsBanner'
import { PolicyProfileCard } from './PolicyProfileCard'

interface MempoolRunDetailProps {
  runId: string
  config: RunConfig
}

// MempoolProblems is the red banner of a mempool run with hard failures or
// scenario errors. The run page shows it at the top.
export function MempoolProblems({ runId }: { runId: string }) {
  const { data: result } = useMempoolResult(runId)
  if (!result) return null
  return <ProblemsBanner scenarios={result.scenarios ?? []} />
}

// MempoolPolicyProfile is the pool policy card of the run's client.
export function MempoolPolicyProfile({ runId, config }: MempoolRunDetailProps) {
  const { data: result } = useMempoolResult(runId)
  if (!result) return null
  return <PolicyProfileCard client={config.instance.client} result={result} wide />
}

const FILTER_OPTIONS: { value: ScenarioFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'problems', label: 'Findings & failures' },
]

// MempoolRunDetail renders the invariants, the scenario results and the
// client comparison of a mempool run. It takes the place of the replay
// sections (heatmaps, opcode panels, block logs, tests table).
export function MempoolRunDetail({ runId, config }: MempoolRunDetailProps) {
  const { data: result } = useMempoolResult(runId)
  const scenarios = useMemo(() => sortScenarios(result?.scenarios ?? []), [result])
  const [filter, setFilter] = useState<ScenarioFilter>('all')
  const [hidePassing, setHidePassing] = useState(false)
  const shown = useMemo(() => filterScenarios(scenarios, filter, hidePassing), [scenarios, filter, hidePassing])
  const filtering = filter !== 'all' || hidePassing
  const issueContext: IssueContext = { client: config.instance.client, runId, version: clientVersion(config) }

  if (!result) return null

  return (
    <div className="flex flex-col gap-4">
      <InvariantsPanel scenarios={scenarios} />
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <h2 className="text-base/7 font-semibold text-gray-900 dark:text-gray-100">Scenarios</h2>
        <SegmentedControl value={filter} onChange={setFilter} options={FILTER_OPTIONS} ariaLabel="Which checks to show" />
        <label className="inline-flex cursor-pointer items-center gap-2 text-xs/5 text-gray-700 dark:text-gray-300">
          <input type="checkbox" checked={hidePassing} onChange={(e) => setHidePassing(e.target.checked)} className="size-4 rounded-xs accent-blue-600" />
          Hide passing scenarios
        </label>
        {filtering && (
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">
            {shown.length} of {scenarios.length} scenarios
          </span>
        )}
      </div>
      {scenarios.length === 0 ? (
        <p className="text-sm/6 text-gray-500 dark:text-gray-400">This run has no scenario results.</p>
      ) : shown.length === 0 ? (
        <p className="text-sm/6 text-gray-500 dark:text-gray-400">No scenario matches the filter: every check passed.</p>
      ) : (
        shown.map((f) => (
          <ScenarioCard
            key={f.scenario.name}
            scenario={f.scenario}
            assertions={f.assertions}
            observations={f.observations}
            forceOpen={filtering}
            issueContext={issueContext}
          />
        ))
      )}
      <MempoolComparison runId={runId} client={config.instance.client} suiteHash={config.suite_hash} />
    </div>
  )
}
