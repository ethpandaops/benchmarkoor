import { loadRuntimeConfig, getDataUrl } from '@/config/runtime'

export async function fetchData<T>(path: string): Promise<T> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`Failed to fetch ${path}: ${response.status}`)
  }

  return response.json()
}

export async function fetchText(path: string): Promise<string> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`Failed to fetch ${path}: ${response.status}`)
  }

  return response.text()
}
