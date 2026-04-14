import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunResult } from '../types'

// useRunResult fetches a run's result.json from the storage backend.
// Pass enabled=false to skip the fetch (e.g. for runs we already know
// are still live and haven't been uploaded yet).
export function useRunResult(runId: string, enabled = true) {
  return useQuery({
    queryKey: ['run', runId, 'result'],
    queryFn: async () => {
      const { data } = await fetchData<RunResult>(`runs/${runId}/result.json`)
      return data ?? null
    },
    enabled: !!runId && enabled,
  })
}
