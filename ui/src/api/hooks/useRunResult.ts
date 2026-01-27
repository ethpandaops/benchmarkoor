import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunResult } from '../types'

export function useRunResult(runId: string) {
  return useQuery({
    queryKey: ['run', runId, 'result'],
    queryFn: async () => {
      const { data, status } = await fetchData<RunResult>(`runs/${runId}/result.json`)
      if (!data) {
        throw new Error(`Failed to fetch run result: ${status}`)
      }
      return data
    },
    enabled: !!runId,
  })
}
