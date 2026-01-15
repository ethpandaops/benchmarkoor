import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { Index } from '../types'

export function useIndex() {
  return useQuery({
    queryKey: ['index'],
    queryFn: () => fetchData<Index>('runs/index.json'),
  })
}
