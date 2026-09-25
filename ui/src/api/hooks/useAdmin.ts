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
    throw new Error(data.error || data.message || `Request failed: ${resp.status}`)
  }
  return resp.json()
}

// Sessions
export interface AdminSession {
  id: number
  user_id: number
  username: string
  source: string
  expires_at: string
  created_at: string
  last_active_at: string
}

export function useSessions() {
  return useQuery<AdminSession[]>({
    queryKey: ['admin', 'sessions'],
    queryFn: () => adminFetch('/api/v1/admin/sessions'),
  })
}

export function useDeleteSession() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) =>
      adminFetch(`/api/v1/admin/sessions/${id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'sessions'] }),
  })
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

// Indexer
interface RunIndexerResponse {
  status: string
  message: string
}

export function useRunIndexer() {
  return useMutation<RunIndexerResponse, Error>({
    mutationFn: () =>
      adminFetch('/api/v1/admin/indexer/run', { method: 'POST' }),
  })
}

// Run deletion. The API queues the runs and a background worker deletes
// them in order, so the response only reports how many were queued.
interface DeleteRunsResponse {
  status: string
  queued: number
  errors?: string[]
}

export function useDeleteRuns() {
  const queryClient = useQueryClient()
  return useMutation<DeleteRunsResponse, Error, string[]>({
    mutationFn: (runIds: string[]) =>
      adminFetch('/api/v1/admin/runs/delete', {
        method: 'POST',
        body: JSON.stringify({ run_ids: runIds }),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['index'] }),
  })
}

interface CancelDeleteRunsResponse {
  status: string
  cancelled: number
  errors?: string[]
}

export function useCancelDeleteRuns() {
  const queryClient = useQueryClient()
  return useMutation<CancelDeleteRunsResponse, Error, string[]>({
    mutationFn: (runIds: string[]) =>
      adminFetch('/api/v1/admin/runs/delete/cancel', {
        method: 'POST',
        body: JSON.stringify({ run_ids: runIds }),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['index'] }),
  })
}

// Indexer failures. Runs storage holds that the indexer gave up on, usually
// because the run directory never got its config.json. The API only records a
// run once it is older than the indexer's grace period, so a run still
// uploading never shows up here. Past that age the failure is final.
export interface IndexerFailure {
  run_id: string
  discovery_path: string
  /** Unix seconds taken from the run ID. Absent on a malformed ID. */
  run_timestamp?: number
  /** Why the indexer gave up on the run. */
  error?: string
  /** When the indexer gave up. There is only one attempt. */
  failed_at: string
  /** Present while the run sits in the deletion queue. */
  deletion_requested_at?: string
  /** The last failed deletion attempt. The run stays queued and is retried. */
  deletion_error?: string
}

export interface IndexerFailuresResponse {
  /** Recorded failures in total, not just on this page. */
  total: number
  limit: number
  offset: number
  entries: IndexerFailure[]
}

export const INDEXER_FAILURES_PAGE_SIZE = 100

export function useIndexerFailures(offset: number, enabled: boolean) {
  return useQuery<IndexerFailuresResponse>({
    queryKey: ['admin', 'indexerFailures', offset],
    enabled,
    // A queued record disappears once the worker deletes its data, so keep
    // the list moving while a bulk delete drains.
    refetchInterval: 10_000,
    queryFn: () =>
      adminFetch(
        `/api/v1/admin/indexer/failures?limit=${INDEXER_FAILURES_PAGE_SIZE}` +
          `&offset=${offset}`,
      ),
  })
}

/** Selects either an explicit set of runs or every recorded failure. */
export type IndexerFailureSelection = { runIds: string[] } | { all: true }

function selectionBody(selection: IndexerFailureSelection): string {
  return JSON.stringify(
    'all' in selection ? { all: true } : { run_ids: selection.runIds },
  )
}

interface DeleteIndexerFailuresResponse {
  status: string
  queued: number
  errors?: string[]
}

export function useDeleteIndexerFailures() {
  const queryClient = useQueryClient()
  return useMutation<DeleteIndexerFailuresResponse, Error, IndexerFailureSelection>({
    mutationFn: (selection) =>
      adminFetch('/api/v1/admin/indexer/failures/delete', {
        method: 'POST',
        body: selectionBody(selection),
      }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ['admin', 'indexerFailures'] }),
  })
}

interface CancelIndexerFailuresResponse {
  status: string
  cancelled: number
  errors?: string[]
}

export function useCancelDeleteIndexerFailures() {
  const queryClient = useQueryClient()
  return useMutation<CancelIndexerFailuresResponse, Error, IndexerFailureSelection>({
    mutationFn: (selection) =>
      adminFetch('/api/v1/admin/indexer/failures/delete/cancel', {
        method: 'POST',
        body: selectionBody(selection),
      }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ['admin', 'indexerFailures'] }),
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
