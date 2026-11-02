import { useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunConfig } from '../types'

export const runConfigQueryKey = (runId: string) => ['run', runId, 'config']

export async function fetchRunConfig(runId: string): Promise<RunConfig> {
  const { data, status } = await fetchData<RunConfig>(`runs/${runId}/config.json`)
  if (!data) {
    throw new Error(`Failed to fetch run config: ${status}`)
  }
  return data
}

// useRunConfig fetches a run's config.json from the storage backend.
// Pass enabled=false to skip the fetch entirely (e.g. when the caller
// already knows the run is live and hasn't been uploaded yet).
//
// retry is intentionally low: a 404 on config.json is the deterministic
// "file not uploaded yet" state, not a transient error, so multiple
// retries with backoff just delay the fallback live view.
export function useRunConfig(runId: string, enabled = true) {
  return useQuery({
    queryKey: runConfigQueryKey(runId),
    queryFn: () => fetchRunConfig(runId),
    enabled: !!runId && enabled,
    retry: 1,
  })
}

// useRunConfigs fetches config.json for several runs at once. The data list
// follows the order of runIds; a missing config is undefined.
export function useRunConfigs(runIds: string[]) {
  return useQueries({
    queries: runIds.map((runId) => ({
      queryKey: runConfigQueryKey(runId),
      queryFn: () => fetchRunConfig(runId),
      enabled: !!runId,
      retry: 1,
    })),
    combine: combineConfigs,
  })
}

function combineConfigs(results: UseQueryResult<RunConfig>[]) {
  return { data: results.map((r) => r.data) }
}
