import { useEffect, useReducer } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchText, fetchData, fetchHead, streamLineSummaries, type LineSummary } from '../client'
import type { AggregatedStats, ResultDetails } from '../types'

const MAX_INLINE_FILE_SIZE = 50 * 1024 * 1024 // 50MB — skip full fetch above this

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
      const head = await fetchHead(path)
      if (head.exists && head.size !== null && head.size > MAX_INLINE_FILE_SIZE) {
        return null
      }

      const { data, status } = await fetchText(path, { cacheBust: false })
      if (!data) {
        throw new Error(`Failed to fetch responses: ${status}`)
      }
      return data.trim().split('\n')
    },
    enabled: !!runId && !!testName,
  })
}

/** Stream response file and return per-line summaries progressively. */
export function useTestResponseSummaries(runId: string, testName: string, stepType: StepType) {
  const path = `runs/${runId}/${testName}/${stepType}.response`
  return useStreamingSummaries(path, !!runId && !!testName)
}

/**
 * Progressive streaming hook — streams a newline-delimited file and
 * updates state as each line is scanned so UI rows appear one by one.
 */

type StreamAction =
  | { type: 'reset'; path: string }
  | { type: 'line'; summary: LineSummary }
  | { type: 'done' }

interface StreamState {
  data: LineSummary[] | undefined
  isStreaming: boolean
  path: string
}

function streamReducer(state: StreamState, action: StreamAction): StreamState {
  switch (action.type) {
    case 'reset':
      return { data: undefined, isStreaming: true, path: action.path }
    case 'line':
      return { ...state, data: state.data ? [...state.data, action.summary] : [action.summary] }
    case 'done':
      return { ...state, isStreaming: false }
  }
}

function useStreamingSummaries(path: string, enabled: boolean) {
  const [state, dispatch] = useReducer(streamReducer, { data: undefined, isStreaming: false, path: '' })

  useEffect(() => {
    if (!enabled) return

    let cancelled = false
    dispatch({ type: 'reset', path })

    streamLineSummaries(path, (summary) => {
      if (!cancelled) dispatch({ type: 'line', summary })
    }).finally(() => {
      if (!cancelled) dispatch({ type: 'done' })
    })

    return () => { cancelled = true }
  }, [path, enabled])

  const current = state.path === path ? state : { data: undefined, isStreaming: enabled }
  return { data: current.data, isStreaming: current.isStreaming }
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
      // Skip full fetch for very large files — streaming summaries handle the rest
      const head = await fetchHead(path)
      if (head.exists && head.size !== null && head.size > MAX_INLINE_FILE_SIZE) {
        return null
      }

      const { data, status } = await fetchText(path, { cacheBust: false })
      if (!data) {
        throw new Error(`Failed to fetch requests: ${status}`)
      }
      return data.trim().split('\n')
    },
    enabled: !!suiteHash && !!testName,
  })
}

/**
 * Stream the request file and return per-line summaries progressively.
 * Updates state as each line is scanned so early rows appear immediately.
 */
export function useTestRequestSummaries(suiteHash: string, testName: string, stepType: StepType) {
  const path = `suites/${suiteHash}/${testName}/${stepType}.request`
  return useStreamingSummaries(path, !!suiteHash && !!testName)
}
