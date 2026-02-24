import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteInfo } from '../types'

export function useSuite(suiteHash: string | undefined) {
  return useQuery({
    queryKey: ['suite', suiteHash],
    queryFn: async () => {
      const { data, status } = await fetchData<SuiteInfo>(`suites/${suiteHash}/summary.json`, { cacheBustInterval: 3600 })
      if (!data) {
        throw new Error(`Failed to fetch suite: ${status}`)
      }
      return data
    },
    enabled: !!suiteHash,
  })
}
