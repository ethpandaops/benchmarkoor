import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteInfo } from '../types'

export function useSuite(suiteHash: string | undefined) {
  return useQuery({
    queryKey: ['suite', suiteHash],
    queryFn: () => fetchData<SuiteInfo>(`suites/${suiteHash}/summary.json`),
    enabled: !!suiteHash,
  })
}
