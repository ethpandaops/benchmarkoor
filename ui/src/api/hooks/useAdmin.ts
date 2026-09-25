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
  const queryClient = useQueryClient()
  return useMutation<RunIndexerResponse, Error>({
    mutationFn: () =>
      adminFetch('/api/v1/admin/indexer/run', { method: 'POST' }),
    // The pass only finishes later, but the stats endpoint reports the
    // running pass straight away. Refetch on a failure too: a 409 means a
    // pass the UI had not polled yet is already running.
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: ['admin', 'indexerStats'] }),
  })
}

// Indexer pass history. The indexer writes a row when a pass ends, so this is
// how long passes take and what each one found. It is the only way to see a
// pass getting slower before it gets slow enough to notice.
export interface IndexerPass {
  started_at: string
  finished_at: string
  /** How long the pass took, in milliseconds. */
  duration_ms: number
  /** What started the pass: "startup", "schedule" or "manual". */
  trigger: string
  /**
   * How the pass ended: "completed", or "cancelled" for one a shutdown cut
   * short, whose counters cover only the work it got through.
   */
  status: string
  discovery_paths: number
  /** Run directories storage held. */
  storage_runs: number
  /** How many of those the index already had. The gap is the backlog. */
  indexed_runs: number
  runs_indexed: number
  runs_reindexed: number
  runs_failed: number
  /** Runs left unread because their failure record is still muted. */
  skipped_failures: number
  error?: string
}

/**
 * The pass in flight. A pass only writes its history row when it ends, so
 * until then this is the only place it appears.
 */
export interface RunningIndexerPass {
  started_at: string
  trigger: string
  /**
   * How long the pass had been running when the response was built. The
   * server measures it, so a browser with a skewed clock still shows the
   * right elapsed time.
   */
  elapsed_ms: number
}

export interface IndexerStatsResponse {
  /**
   * Whether a pass is in flight. Starting another one would only be refused,
   * so the UI waits instead.
   */
  running: boolean
  /** Absent when no pass is running. */
  current_pass?: RunningIndexerPass
  /** The configured delay between passes, as a Go duration. */
  interval?: string
  limit: number
  /** The most recent passes, newest first. */
  passes: IndexerPass[]
}

export const INDEXER_PASSES_LIMIT = 100

export function useIndexerStats(enabled: boolean) {
  return useQuery<IndexerStatsResponse>({
    queryKey: ['admin', 'indexerStats'],
    enabled,
    // A pass ends between polls, and the running state is what goes stale
    // fast, so this is short. The query reads a few hundred small rows.
    refetchInterval: 10_000,
    queryFn: () =>
      adminFetch(
        `/api/v1/admin/indexer/stats?limit=${INDEXER_PASSES_LIMIT}`,
      ),
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

// Deleting a whole suite. The server resolves the suite to its runs, so a
// suite with thousands of runs is still one small request. Runs that are
// still in progress are left alone and come back in `errors`.
export function useDeleteRunsBySuite() {
  const queryClient = useQueryClient()
  return useMutation<DeleteRunsResponse, Error, string[]>({
    mutationFn: (suiteHashes: string[]) =>
      adminFetch('/api/v1/admin/runs/delete', {
        method: 'POST',
        body: JSON.stringify({ suite_hashes: suiteHashes }),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['index'] }),
  })
}

// Index database report. Gathering it counts every row in every table, so the
// API caches it for a minute and `gathered_at` says when it was taken.
export interface DatabaseTableStat {
  name: string
  rows: number
}

export interface DatabaseSuiteUsage {
  suite_hash: string
  name?: string
  discovery_path?: string
  runs: number
  test_stats: number
  block_logs: number
  /** Unix seconds of the newest run. */
  last_run?: number
}

export interface DatabaseStats {
  driver: string
  /** The SQLite file. Absent for other drivers. */
  path?: string
  file_bytes?: number
  /** The write-ahead log, which a long-running writer can leave large. */
  wal_bytes?: number
  page_size?: number
  page_count?: number
  /** Pages a VACUUM would hand back. */
  free_pages?: number
  /**
   * Used and free do not add up to total: a filesystem reserves a slice for
   * root. A usage share is used/(used+free), which is what df prints.
   */
  volume_total_bytes?: number
  volume_used_bytes?: number
  volume_free_bytes?: number
  oldest_run?: number
  newest_run?: number
  tables: DatabaseTableStat[]
  top_suites: DatabaseSuiteUsage[]
}

export interface DatabaseStatsResponse {
  gathered_at: string
  stats: DatabaseStats
}

export function useDatabaseStats(enabled: boolean) {
  return useQuery<DatabaseStatsResponse>({
    queryKey: ['admin', 'database'],
    enabled,
    // The API caches the report for five minutes, so most of these polls are
    // answered from that cache and cost only a round-trip. Polling at exactly
    // the TTL would instead land just after every expiry and make each tab
    // pay for a full scan.
    refetchInterval: 60_000,
    queryFn: () => adminFetch('/api/v1/admin/database'),
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

// Indexer failures. Runs storage exposes that the indexer cannot index,
// usually because the run directory never got its config.json. The API only
// records a run once it is older than the indexer's grace period, so a run
// still uploading never shows up here.
export interface IndexerFailure {
  run_id: string
  discovery_path: string
  /** Unix seconds taken from the run ID. Absent on a malformed ID. */
  run_timestamp?: number
  error?: string
  attempts: number
  first_failed_at: string
  last_attempt_at: string
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
