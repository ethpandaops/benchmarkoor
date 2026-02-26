import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { loadRuntimeConfig } from '@/config/runtime'

export interface ApiKey {
  id: number
  name: string
  key_prefix: string
  user_id: number
  expires_at: string | null
  last_used_at: string | null
  created_at: string
}

export interface AdminApiKey extends ApiKey {
  username: string
}

export interface CreateApiKeyResponse {
  key: string
  api_key: ApiKey
}

async function getApiBaseUrl(): Promise<string> {
  const cfg = await loadRuntimeConfig()
  return cfg.api?.baseUrl ?? ''
}

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const baseUrl = await getApiBaseUrl()
  const resp = await fetch(`${baseUrl}${path}`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({ error: 'Request failed' }))
    throw new Error(data.error || `Request failed: ${resp.status}`)
  }
  return resp.json()
}

// User-facing hooks

export function useMyApiKeys() {
  return useQuery<ApiKey[]>({
    queryKey: ['api-keys', 'mine'],
    queryFn: () => apiFetch('/api/v1/auth/api-keys'),
  })
}

export function useCreateApiKey() {
  const queryClient = useQueryClient()
  return useMutation<CreateApiKeyResponse, Error, { name: string; expires_at?: string }>({
    mutationFn: (data) =>
      apiFetch('/api/v1/auth/api-keys', {
        method: 'POST',
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })
}

export function useDeleteMyApiKey() {
  const queryClient = useQueryClient()
  return useMutation<unknown, Error, number>({
    mutationFn: (id) =>
      apiFetch(`/api/v1/auth/api-keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })
}

// Admin hooks

export function useAdminApiKeys() {
  return useQuery<AdminApiKey[]>({
    queryKey: ['api-keys', 'admin'],
    queryFn: () => apiFetch('/api/v1/admin/api-keys'),
  })
}

export function useDeleteAdminApiKey() {
  const queryClient = useQueryClient()
  return useMutation<unknown, Error, number>({
    mutationFn: (id) =>
      apiFetch(`/api/v1/admin/api-keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })
}
