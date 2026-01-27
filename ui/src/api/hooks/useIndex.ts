import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { Index } from '../types'

const emptyIndex: Index = { generated: 0, entries: [] }

export function useIndex() {
  return useQuery({
    queryKey: ['index'],
    queryFn: async () => {
      const { data, status } = await fetchData<Index>('runs/index.json')

      if (status === 404) {
        return emptyIndex
      }

      if (!data) {
        throw new Error(`Failed to fetch index: ${status}`)
      }

      return data
    },
  })
}
