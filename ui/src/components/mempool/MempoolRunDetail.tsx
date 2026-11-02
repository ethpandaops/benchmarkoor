import { useMemo } from 'react'
import type { RunConfig } from '@/api/types'
import { useMempoolResult } from '@/api/hooks/useMempoolResult'
import { sortScenarios } from './mempoolUtils'
import { ScenarioCard } from './ScenarioCard'
import { MempoolComparison } from './MempoolComparison'

interface MempoolRunDetailProps {
  runId: string
  config: RunConfig
}

// MempoolRunDetail renders the scenario results and the client comparison
// of a mempool run. It takes the place of the replay sections (heatmaps,
// opcode panels, block logs, tests table).
export function MempoolRunDetail({ runId, config }: MempoolRunDetailProps) {
  const { data: result } = useMempoolResult(runId)
  const scenarios = useMemo(() => sortScenarios(result?.scenarios ?? []), [result])

  if (!result) return null

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-base/7 font-semibold text-gray-900 dark:text-gray-100">Scenarios</h2>
      {scenarios.length === 0 ? (
        <p className="text-sm/6 text-gray-500 dark:text-gray-400">This run has no scenario results.</p>
      ) : (
        scenarios.map((s) => <ScenarioCard key={s.name} scenario={s} />)
      )}
      <MempoolComparison runId={runId} client={config.instance.client} suiteHash={config.suite_hash} />
    </div>
  )
}
