import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunResult } from '../types'

export function useRunResult(runId: string) {
  return useQuery({
    queryKey: ['run', runId, 'result'],
    queryFn: () => fetchData<RunResult>(`runs/${runId}/result.json`),
    enabled: !!runId,
  })
}
