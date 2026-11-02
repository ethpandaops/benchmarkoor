import { useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { MempoolRunResult } from '../types'

const mempoolResultQueryKey = (runId: string) => ['run', runId, 'mempool-result']

async function fetchMempoolResult(runId: string): Promise<MempoolRunResult | null> {
  const { data } = await fetchData<MempoolRunResult>(`runs/${runId}/mempool-result.json`)
  return data ?? null
}

// useMempoolResult fetches a mempool run's mempool-result.json. It returns
// null when the file does not exist (e.g. the run failed before it wrote it).
export function useMempoolResult(runId: string, enabled = true) {
  return useQuery({
    queryKey: mempoolResultQueryKey(runId),
    queryFn: () => fetchMempoolResult(runId),
    enabled: !!runId && enabled,
  })
}

// combineResults is at module level so useQueries keeps a stable result
// between renders when no query changed.
function combineResults(results: UseQueryResult<MempoolRunResult | null>[]) {
  return {
    data: results.map((r) => r.data ?? null),
    isLoading: results.some((r) => r.isLoading),
  }
}

// useMempoolResults fetches mempool-result.json for several runs at once.
// The data list follows the order of runIds. It shares the cache with
// useMempoolResult, so the current run is not fetched twice.
export function useMempoolResults(runIds: string[]) {
  return useQueries({
    queries: runIds.map((runId) => ({
      queryKey: mempoolResultQueryKey(runId),
      queryFn: () => fetchMempoolResult(runId),
      enabled: !!runId,
    })),
    combine: combineResults,
  })
}
