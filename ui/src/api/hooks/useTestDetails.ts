import { useQuery } from '@tanstack/react-query'
import { fetchText, fetchData } from '../client'
import type { AggregatedStats, ResultDetails } from '../types'

// Step types for test execution
export type StepType = 'setup' | 'test' | 'cleanup' | 'pre_run'

export function useTestResultDetails(runId: string, testName: string, stepType: StepType) {
  // Path: runs/{runId}/{testName}/{stepType}.result-details.json
  const path = `runs/${runId}/${testName}/${stepType}.result-details.json`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'step', stepType, 'result-details'],
    queryFn: async () => {
      const { data, status } = await fetchData<ResultDetails>(path)
      if (!data) {
        throw new Error(`Failed to fetch result details: ${status}`)
      }
      return data
    },
    enabled: !!runId && !!testName,
  })
}

export function useTestResponses(runId: string, testName: string, stepType: StepType) {
  // Path: runs/{runId}/{testName}/{stepType}.response
  const path = `runs/${runId}/${testName}/${stepType}.response`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'step', stepType, 'responses'],
    queryFn: async () => {
      const { data, status } = await fetchText(path)
      if (!data) {
        throw new Error(`Failed to fetch responses: ${status}`)
      }
      return data.trim().split('\n')
    },
    enabled: !!runId && !!testName,
  })
}

export function useTestAggregated(runId: string, testName: string, stepType: StepType) {
  // Path: runs/{runId}/{testName}/{stepType}.result-aggregated.json
  const path = `runs/${runId}/${testName}/${stepType}.result-aggregated.json`

  return useQuery({
    queryKey: ['run', runId, 'test', testName, 'step', stepType, 'aggregated'],
    queryFn: async () => {
      const { data, status } = await fetchData<AggregatedStats>(path)
      if (!data) {
        throw new Error(`Failed to fetch aggregated stats: ${status}`)
      }
      return data
    },
    enabled: !!runId && !!testName,
  })
}

export function useTestRequests(suiteHash: string, testName: string, stepType: StepType) {
  // Path: suites/{suiteHash}/{testName}/{stepType}.request
  const path = `suites/${suiteHash}/${testName}/${stepType}.request`

  return useQuery({
    queryKey: ['suite', suiteHash, 'test', testName, 'step', stepType, 'requests'],
    queryFn: async () => {
      const { data, status } = await fetchText(path)
      if (!data) {
        throw new Error(`Failed to fetch requests: ${status}`)
      }
      return data.trim().split('\n')
    },
    enabled: !!suiteHash && !!testName,
  })
}
