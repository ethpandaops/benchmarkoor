import { useQuery } from '@tanstack/react-query'
import { fetchText, fetchData } from '../client'
import type { AggregatedStats } from '../types'

export function useTestTimes(runId: string, testName: string, dir?: string) {
  const path = dir
    ? `runs/${runId}/${dir}/${testName}.times`
    : `runs/${runId}/${testName}.times`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'times'],
    queryFn: async () => {
      const text = await fetchText(path)
      return text.trim().split('\n').map(Number)
    },
    enabled: !!runId && !!testName,
  })
}

export function useTestResponses(runId: string, testName: string, dir?: string) {
  const path = dir
    ? `runs/${runId}/${dir}/${testName}.response`
    : `runs/${runId}/${testName}.response`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'responses'],
    queryFn: async () => {
      const text = await fetchText(path)
      return text.trim().split('\n')
    },
    enabled: !!runId && !!testName,
  })
}

export function useTestAggregated(runId: string, testName: string, dir?: string) {
  const path = dir
    ? `runs/${runId}/${dir}/${testName}.times_aggregated.json`
    : `runs/${runId}/${testName}.times_aggregated.json`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'aggregated'],
    queryFn: () => fetchData<AggregatedStats>(path),
    enabled: !!runId && !!testName,
  })
}
