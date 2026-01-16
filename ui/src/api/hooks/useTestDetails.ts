import { useQuery } from '@tanstack/react-query'
import { fetchText, fetchData } from '../client'
import type { AggregatedStats, ResultDetails } from '../types'

export function useTestTimes(runId: string, testName: string, dir?: string) {
  const path = dir
    ? `runs/${runId}/${dir}/${testName}.result-details.json`
    : `runs/${runId}/${testName}.result-details.json`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'times'],
    queryFn: async () => {
      const details = await fetchData<ResultDetails>(path)
      return details.duration_ns
    },
    enabled: !!runId && !!testName,
  })
}

export function useTestResultDetails(runId: string, testName: string, dir?: string) {
  const path = dir
    ? `runs/${runId}/${dir}/${testName}.result-details.json`
    : `runs/${runId}/${testName}.result-details.json`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'result-details'],
    queryFn: () => fetchData<ResultDetails>(path),
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

export function useTestRequests(suiteHash: string, testName: string, dir?: string) {
  const path = dir
    ? `suites/${suiteHash}/tests/${dir}/${testName}`
    : `suites/${suiteHash}/tests/${testName}`

  return useQuery({
    queryKey: ['suite', suiteHash, 'test', testName, 'requests', dir],
    queryFn: async () => {
      const text = await fetchText(path)
      return text.trim().split('\n')
    },
    enabled: !!suiteHash && !!testName,
  })
}
