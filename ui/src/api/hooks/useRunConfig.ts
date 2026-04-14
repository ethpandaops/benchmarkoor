import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunConfig } from '../types'

// useRunConfig fetches a run's config.json from the storage backend.
// Pass enabled=false to skip the fetch entirely (e.g. when the caller
// already knows the run is live and hasn't been uploaded yet).
//
// retry is intentionally low: a 404 on config.json is the deterministic
// "file not uploaded yet" state, not a transient error, so multiple
// retries with backoff just delay the fallback live view.
export function useRunConfig(runId: string, enabled = true) {
  return useQuery({
    queryKey: ['run', runId, 'config'],
    queryFn: async () => {
      const { data, status } = await fetchData<RunConfig>(`runs/${runId}/config.json`)
      if (!data) {
        throw new Error(`Failed to fetch run config: ${status}`)
      }
      return data
    },
    enabled: !!runId && enabled,
    retry: 1,
  })
}
