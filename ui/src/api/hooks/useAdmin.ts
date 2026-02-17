import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { loadRuntimeConfig } from '@/config/runtime'
import type { AuthUser } from '@/api/auth-client'

interface GitHubOrgMapping {
  id: number
  org: string
  role: string
}

interface GitHubUserMapping {
  id: number
  username: string
  role: string
}

async function getApiBaseUrl(): Promise<string> {
  const cfg = await loadRuntimeConfig()
  return cfg.api?.baseUrl ?? ''
}

async function adminFetch<T>(path: string, options?: RequestInit): Promise<T> {
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

// Users
export function useUsers() {
  return useQuery<AuthUser[]>({
    queryKey: ['admin', 'users'],
    queryFn: () => adminFetch('/api/v1/admin/users'),
  })
}

export function useCreateUser() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (data: { username: string; password: string; role: string }) =>
      adminFetch('/api/v1/admin/users', {
        method: 'POST',
        body: JSON.stringify(data),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }),
  })
}

export function useUpdateUser() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...data }: { id: number; password?: string; role?: string }) =>
      adminFetch(`/api/v1/admin/users/${id}`, {
        method: 'PUT',
        body: JSON.stringify(data),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }),
  })
}

export function useDeleteUser() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) =>
      adminFetch(`/api/v1/admin/users/${id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }),
  })
}

// GitHub Org Mappings
export function useOrgMappings() {
  return useQuery<GitHubOrgMapping[]>({
    queryKey: ['admin', 'orgMappings'],
    queryFn: () => adminFetch('/api/v1/admin/github/org-mappings'),
  })
}

export function useUpsertOrgMapping() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (data: { org: string; role: string }) =>
      adminFetch('/api/v1/admin/github/org-mappings', {
        method: 'POST',
        body: JSON.stringify(data),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'orgMappings'] }),
  })
}

export function useDeleteOrgMapping() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) =>
      adminFetch(`/api/v1/admin/github/org-mappings/${id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'orgMappings'] }),
  })
}

// GitHub User Mappings
export function useUserMappings() {
  return useQuery<GitHubUserMapping[]>({
    queryKey: ['admin', 'userMappings'],
    queryFn: () => adminFetch('/api/v1/admin/github/user-mappings'),
  })
}

export function useUpsertUserMapping() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (data: { username: string; role: string }) =>
      adminFetch('/api/v1/admin/github/user-mappings', {
        method: 'POST',
        body: JSON.stringify(data),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'userMappings'] }),
  })
}

export function useDeleteUserMapping() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) =>
      adminFetch(`/api/v1/admin/github/user-mappings/${id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'userMappings'] }),
  })
}
