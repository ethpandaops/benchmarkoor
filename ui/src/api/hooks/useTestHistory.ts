import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteStats, TestHistory } from '../types'
import { loadRuntimeConfig, isIndexingEnabled } from '@/config/runtime'

// Keeps the N most recent runs per client, newest first. The API does this
// itself; the static stats.json holds every run of every test.
function limitRunsPerClient(history: TestHistory, maxRunsPerClient: number): TestHistory {
  const sorted = [...history.durations].sort((a, b) => b.run_start - a.run_start)
  const perClient = new Map<string, number>()

  return {
    durations: sorted.filter((d) => {
      const count = perClient.get(d.client) ?? 0
      if (count >= maxRunsPerClient) return false
      perClient.set(d.client, count + 1)
      return true
    }),
  }
}

export function useTestHistory(suiteHash: string, testName: string | undefined, maxRunsPerClient: number) {
  return useQuery({
    queryKey: ['testHistory', suiteHash, testName, maxRunsPerClient],
    queryFn: async (): Promise<TestHistory> => {
      const config = await loadRuntimeConfig()

      if (isIndexingEnabled(config) && config.api?.baseUrl) {
        const params = new URLSearchParams({
          test: testName!,
          max_runs_per_client: String(maxRunsPerClient),
        })
        const response = await fetch(
          `${config.api.baseUrl}/api/v1/index/suites/${suiteHash}/tests/history?${params}`,
          { credentials: 'include' },
        )

        if (!response.ok) {
          throw new Error(`Failed to fetch test history: ${response.status}`)
        }

        return response.json() as Promise<TestHistory>
      }

      const { data, status } = await fetchData<SuiteStats>(`suites/${suiteHash}/stats.json`)
      if (!data) {
        throw new Error(`Failed to fetch suite stats: ${status}`)
      }

      return limitRunsPerClient({ durations: data[testName!]?.durations ?? [] }, maxRunsPerClient)
    },
    enabled: !!suiteHash && !!testName,
  })
}
