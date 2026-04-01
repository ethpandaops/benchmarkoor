import { useQuery } from '@tanstack/react-query'
import { fetchText, fetchData, fetchHead, fetchLineSummaries } from '../client'
import type { AggregatedStats, ResultDetails } from '../types'

const MAX_REQUEST_FILE_SIZE = 10 * 1024 * 1024 // 10MB

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
      // Check file size first to avoid crashing on huge request files
      const head = await fetchHead(path)
      if (head.exists && head.size !== null && head.size > MAX_REQUEST_FILE_SIZE) {
        throw new Error(
          `Request file too large (${(head.size / 1024 / 1024).toFixed(0)}MB). ` +
          `Execution details cannot be displayed.`,
        )
      }

      const { data, status } = await fetchText(path)
      if (!data) {
        throw new Error(`Failed to fetch requests: ${status}`)
      }
      return data.trim().split('\n')
    },
    enabled: !!suiteHash && !!testName,
  })
}

/**
 * Stream the request file and return per-line summaries (byte size +
 * first 256 bytes for method extraction). Works for any file size since
 * the full content is never held in memory.
 */
export function useTestRequestSummaries(suiteHash: string, testName: string, stepType: StepType) {
  const path = `suites/${suiteHash}/${testName}/${stepType}.request`

  return useQuery({
    queryKey: ['suite', suiteHash, 'test', testName, 'step', stepType, 'request-summaries'],
    queryFn: async () => {
      const { data, status } = await fetchLineSummaries(path)
      if (!data) {
        throw new Error(`Failed to stream request file: ${status}`)
      }
      return data
    },
    enabled: !!suiteHash && !!testName,
  })
}
