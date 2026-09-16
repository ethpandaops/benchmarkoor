import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunResult } from '../types'
import { withoutBaselineTests } from '@/utils/baselineTests'

// fetchRunResult fetches a run's result.json from the storage backend.
// Baseline calibration tests are dropped here so every consumer (tables,
// heatmaps, aggregates, compare pages) is unskewed by them. All fetches of
// result.json must go through this function — the compare pages share the
// ['run', runId, 'result'] cache key with useRunResult, so a filtered and an
// unfiltered fetcher on the same key would race to define the cached shape.
export async function fetchRunResult(runId: string): Promise<RunResult | null> {
  const { data } = await fetchData<RunResult>(`runs/${runId}/result.json`)
  if (!data) return null
  return { ...data, tests: withoutBaselineTests(data.tests) }
}

// useRunResult fetches a run's result.json from the storage backend.
// Pass enabled=false to skip the fetch (e.g. for runs we already know
// are still live and haven't been uploaded yet).
export function useRunResult(runId: string, enabled = true) {
  return useQuery({
    queryKey: ['run', runId, 'result'],
    queryFn: () => fetchRunResult(runId),
    enabled: !!runId && enabled,
  })
}
