import { useReducer, useEffect, useMemo, useCallback, useState, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Copy, Check, Loader2, Plus, X, ChevronLeft, ChevronRight, ChevronUp, ChevronDown } from 'lucide-react'
import { loadRuntimeConfig } from '@/config/runtime'

// --- Column & operator metadata ---

interface ColumnGroup {
  label: string
  columns: string[]
}

const RUNS_COLUMN_GROUPS: ColumnGroup[] = [
  { label: 'Identity', columns: ['id', 'discovery_path', 'run_id', 'suite_hash', 'instance_id'] },
  { label: 'Client', columns: ['client', 'image', 'rollback_strategy'] },
  { label: 'Status', columns: ['status', 'termination_reason', 'has_result'] },
  { label: 'Tests', columns: ['tests_total', 'tests_passed', 'tests_failed'] },
  { label: 'Timing', columns: ['timestamp', 'timestamp_end', 'indexed_at', 'reindexed_at'] },
]

const TEST_STAT_COLUMN_GROUPS: ColumnGroup[] = [
  { label: 'Identity', columns: ['id', 'suite_hash', 'run_id', 'test_name', 'client'] },
  { label: 'Totals', columns: ['total_gas_used', 'total_time_ns', 'total_mgas_s'] },
  { label: 'Setup', columns: ['setup_gas_used', 'setup_time_ns', 'setup_mgas_s', 'setup_rpc_calls_count'] },
  {
    label: 'Setup Resources',
    columns: [
      'setup_resource_cpu_usec', 'setup_resource_memory_delta_bytes', 'setup_resource_memory_bytes',
      'setup_resource_disk_read_bytes', 'setup_resource_disk_write_bytes',
      'setup_resource_disk_read_iops', 'setup_resource_disk_write_iops',
    ],
  },
  { label: 'Test', columns: ['test_gas_used', 'test_time_ns', 'test_mgas_s', 'test_rpc_calls_count'] },
  {
    label: 'Test Resources',
    columns: [
      'test_resource_cpu_usec', 'test_resource_memory_delta_bytes', 'test_resource_memory_bytes',
      'test_resource_disk_read_bytes', 'test_resource_disk_write_bytes',
      'test_resource_disk_read_iops', 'test_resource_disk_write_iops',
    ],
  },
  { label: 'Timing', columns: ['run_start', 'run_end'] },
]

const TEST_STATS_BLOCK_LOG_COLUMN_GROUPS: ColumnGroup[] = [
  { label: 'Identity', columns: ['id', 'suite_hash', 'run_id', 'test_name', 'client'] },
  { label: 'Block', columns: ['block_number', 'block_hash', 'block_gas_used', 'block_tx_count'] },
  {
    label: 'Timing',
    columns: [
      'timing_execution_ms', 'timing_state_read_ms', 'timing_state_hash_ms',
      'timing_commit_ms', 'timing_total_ms',
    ],
  },
  { label: 'Throughput', columns: ['throughput_mgas_per_sec'] },
  { label: 'State Reads', columns: ['state_read_accounts', 'state_read_storage_slots', 'state_read_code', 'state_read_code_bytes'] },
  {
    label: 'State Writes',
    columns: [
      'state_write_accounts', 'state_write_accounts_deleted', 'state_write_storage_slots',
      'state_write_slots_deleted', 'state_write_code', 'state_write_code_bytes',
    ],
  },
  {
    label: 'Cache',
    columns: [
      'cache_account_hits', 'cache_account_misses', 'cache_account_hit_rate',
      'cache_storage_hits', 'cache_storage_misses', 'cache_storage_hit_rate',
      'cache_code_hits', 'cache_code_misses', 'cache_code_hit_rate',
      'cache_code_hit_bytes', 'cache_code_miss_bytes',
    ],
  },
]

const RUNS_COLUMNS = RUNS_COLUMN_GROUPS.flatMap((g) => g.columns)
const TEST_STAT_COLUMNS = TEST_STAT_COLUMN_GROUPS.flatMap((g) => g.columns)
const TEST_STATS_BLOCK_LOG_COLUMNS = TEST_STATS_BLOCK_LOG_COLUMN_GROUPS.flatMap((g) => g.columns)

const OPERATORS = [
  { value: 'eq', label: '= equals' },
  { value: 'neq', label: '!= not equals' },
  { value: 'gt', label: '> greater than' },
  { value: 'gte', label: '>= greater or equal' },
  { value: 'lt', label: '< less than' },
  { value: 'lte', label: '<= less or equal' },
  { value: 'like', label: 'LIKE pattern' },
  { value: 'in', label: 'IN list' },
  { value: 'is', label: 'IS null/true/false' },
]

const VALID_OPERATORS = new Set(OPERATORS.map((o) => o.value))

const TIMESTAMP_COLUMNS = new Set([
  'timestamp', 'timestamp_end', 'indexed_at', 'reindexed_at', 'run_start', 'run_end',
])

// --- Types ---

type Endpoint = 'runs' | 'test_stats' | 'test_stats_block_logs'

interface FilterRow {
  id: string
  column: string
  operator: string
  value: string
}

interface OrderRow {
  id: string
  column: string
  direction: 'asc' | 'desc'
}

interface QueryBuilderState {
  endpoint: Endpoint
  filters: FilterRow[]
  orders: OrderRow[]
  limit: number
  offset: number
  selectedColumns: string[]
}

// --- URL <-> State serialization ---

interface QuerySearchParams {
  endpoint?: string
  f?: string    // comma-separated "col:op:val" entries
  order?: string // comma-separated "col.dir" entries
  select?: string
  limit?: string
  offset?: string
}

function stateToSearchParams(state: QueryBuilderState): QuerySearchParams {
  const params: QuerySearchParams = {}

  if (state.endpoint !== 'runs') {
    params.endpoint = state.endpoint
  }

  if (state.filters.length > 0) {
    const parts = state.filters.map((f) => `${f.column}:${f.operator}:${encodeURIComponent(f.value)}`)
    params.f = parts.join(',')
  }

  if (state.orders.length > 0) {
    params.order = state.orders.map((o) => `${o.column}.${o.direction}`).join(',')
  }

  if (state.selectedColumns.length > 0) {
    params.select = state.selectedColumns.join(',')
  }

  if (state.limit !== 20) {
    params.limit = String(state.limit)
  }

  if (state.offset > 0) {
    params.offset = String(state.offset)
  }

  return params
}

function searchParamsToState(params: QuerySearchParams): QueryBuilderState | null {
  // Only restore if there's at least one meaningful param.
  const hasParams = params.endpoint || params.f || params.order ||
    params.select || params.limit || params.offset
  if (!hasParams) return null

  const endpoint: Endpoint =
    params.endpoint === 'test_stats' ? 'test_stats'
    : params.endpoint === 'test_stats_block_logs' ? 'test_stats_block_logs'
    : 'runs'
  const validCols = new Set(columnsForEndpoint(endpoint))

  const filters: FilterRow[] = []
  if (params.f) {
    for (const part of params.f.split(',')) {
      const firstColon = part.indexOf(':')
      if (firstColon < 0) continue
      const secondColon = part.indexOf(':', firstColon + 1)
      if (secondColon < 0) continue
      const column = part.slice(0, firstColon)
      const operator = part.slice(firstColon + 1, secondColon)
      const value = decodeURIComponent(part.slice(secondColon + 1))
      if (validCols.has(column) && VALID_OPERATORS.has(operator)) {
        filters.push({ id: uid(), column, operator, value })
      }
    }
  }

  const orders: OrderRow[] = []
  if (params.order) {
    for (const part of params.order.split(',')) {
      const dotIdx = part.lastIndexOf('.')
      if (dotIdx < 0) continue
      const column = part.slice(0, dotIdx)
      const direction = part.slice(dotIdx + 1)
      if (validCols.has(column) && (direction === 'asc' || direction === 'desc')) {
        orders.push({ id: uid(), column, direction })
      }
    }
  }

  const selectedColumns: string[] = []
  if (params.select) {
    for (const col of params.select.split(',')) {
      const trimmed = col.trim()
      if (trimmed && validCols.has(trimmed)) {
        selectedColumns.push(trimmed)
      }
    }
  }

  const limit = params.limit ? Math.min(Math.max(1, Number(params.limit) || 20), 1000) : 20
  const offset = params.offset ? Math.max(0, Number(params.offset) || 0) : 0

  return { endpoint, filters, orders, limit, offset, selectedColumns }
}

// --- Reducer ---

type Action =
  | { type: 'SET_ENDPOINT'; endpoint: Endpoint }
  | { type: 'ADD_FILTER' }
  | { type: 'REMOVE_FILTER'; id: string }
  | { type: 'UPDATE_FILTER'; id: string; field: keyof FilterRow; value: string }
  | { type: 'ADD_ORDER' }
  | { type: 'REMOVE_ORDER'; id: string }
  | { type: 'UPDATE_ORDER'; id: string; field: 'column' | 'direction'; value: string }
  | { type: 'TOGGLE_ORDER'; column: string }
  | { type: 'SET_LIMIT'; limit: number }
  | { type: 'SET_OFFSET'; offset: number }
  | { type: 'SET_COLUMNS'; columns: string[] }
  | { type: 'LOAD_PRESET'; preset: Omit<QueryBuilderState, 'selectedColumns'> & { selectedColumns?: string[] } }

let nextId = 1
function uid() {
  return String(nextId++)
}

function columnsForEndpoint(ep: Endpoint) {
  if (ep === 'runs') return RUNS_COLUMNS
  if (ep === 'test_stats_block_logs') return TEST_STATS_BLOCK_LOG_COLUMNS
  return TEST_STAT_COLUMNS
}

function columnGroupsForEndpoint(ep: Endpoint): ColumnGroup[] {
  if (ep === 'runs') return RUNS_COLUMN_GROUPS
  if (ep === 'test_stats_block_logs') return TEST_STATS_BLOCK_LOG_COLUMN_GROUPS
  return TEST_STAT_COLUMN_GROUPS
}

function makeInitialState(): QueryBuilderState {
  return {
    endpoint: 'runs',
    filters: [],
    orders: [],
    limit: 20,
    offset: 0,
    selectedColumns: [],
  }
}

function reducer(state: QueryBuilderState, action: Action): QueryBuilderState {
  switch (action.type) {
    case 'SET_ENDPOINT':
      return { ...makeInitialState(), endpoint: action.endpoint }

    case 'ADD_FILTER': {
      const cols = columnsForEndpoint(state.endpoint)
      return {
        ...state,
        filters: [...state.filters, { id: uid(), column: cols[0], operator: 'eq', value: '' }],
      }
    }
    case 'REMOVE_FILTER':
      return { ...state, filters: state.filters.filter((f) => f.id !== action.id) }
    case 'UPDATE_FILTER':
      return {
        ...state,
        filters: state.filters.map((f) =>
          f.id === action.id ? { ...f, [action.field]: action.value } : f,
        ),
      }

    case 'ADD_ORDER': {
      const cols = columnsForEndpoint(state.endpoint)
      return {
        ...state,
        orders: [...state.orders, { id: uid(), column: cols[0], direction: 'asc' }],
      }
    }
    case 'REMOVE_ORDER':
      return { ...state, orders: state.orders.filter((o) => o.id !== action.id) }
    case 'UPDATE_ORDER':
      return {
        ...state,
        orders: state.orders.map((o) =>
          o.id === action.id ? { ...o, [action.field]: action.value } : o,
        ),
      }
    case 'TOGGLE_ORDER': {
      const existing = state.orders.find((o) => o.column === action.column)
      if (!existing) {
        return {
          ...state,
          orders: [...state.orders, { id: uid(), column: action.column, direction: 'asc' }],
        }
      }
      if (existing.direction === 'asc') {
        return {
          ...state,
          orders: state.orders.map((o) =>
            o.column === action.column ? { ...o, direction: 'desc' as const } : o,
          ),
        }
      }
      return {
        ...state,
        orders: state.orders.filter((o) => o.column !== action.column),
      }
    }

    case 'SET_LIMIT':
      return { ...state, limit: Math.min(Math.max(1, action.limit), 1000), offset: 0 }
    case 'SET_OFFSET':
      return { ...state, offset: Math.max(0, action.offset) }
    case 'SET_COLUMNS':
      return { ...state, selectedColumns: action.columns }

    case 'LOAD_PRESET':
      return {
        endpoint: action.preset.endpoint,
        filters: action.preset.filters,
        orders: action.preset.orders,
        limit: action.preset.limit,
        offset: action.preset.offset,
        selectedColumns: action.preset.selectedColumns ?? [],
      }
  }
}

// --- API URL generation ---

function buildQueryUrl(state: QueryBuilderState, apiBaseUrl: string): string {
  const params = new URLSearchParams()

  for (const f of state.filters) {
    if (f.value || f.operator === 'is') {
      params.append(f.column, `${f.operator}.${f.value}`)
    }
  }

  if (state.orders.length > 0) {
    params.set('order', state.orders.map((o) => `${o.column}.${o.direction}`).join(','))
  }

  if (state.selectedColumns.length > 0) {
    params.set('select', state.selectedColumns.join(','))
  }

  params.set('limit', String(state.limit))

  if (state.offset > 0) {
    params.set('offset', String(state.offset))
  }

  const qs = params.toString()
  return `${apiBaseUrl}/api/v1/index/query/${state.endpoint}${qs ? `?${qs}` : ''}`
}

// --- Example presets ---

interface Preset {
  label: string
  state: Omit<QueryBuilderState, 'selectedColumns'> & { selectedColumns?: string[] }
}

const PRESETS: Preset[] = [
  {
    label: 'Recent geth runs',
    state: {
      endpoint: 'runs',
      filters: [{ id: uid(), column: 'client', operator: 'eq', value: 'geth' }],
      orders: [{ id: uid(), column: 'timestamp', direction: 'desc' }],
      limit: 20,
      offset: 0,
    },
  },
  {
    label: 'Failed runs',
    state: {
      endpoint: 'runs',
      filters: [{ id: uid(), column: 'tests_failed', operator: 'gt', value: '0' }],
      orders: [{ id: uid(), column: 'timestamp', direction: 'desc' }],
      limit: 100,
      offset: 0,
      selectedColumns: ['timestamp', 'run_id', 'suite_hash', 'client', 'tests_total', 'tests_passed', 'tests_failed'],
    },
  },
  {
    label: 'Slow tests',
    state: {
      endpoint: 'test_stats',
      filters: [{ id: uid(), column: 'test_mgas_s', operator: 'gt', value: '0' }],
      orders: [{ id: uid(), column: 'test_mgas_s', direction: 'asc' }],
      limit: 20,
      offset: 0,
      selectedColumns: ['run_id', 'test_mgas_s', 'test_name', 'suite_hash'],
    },
  },
  {
    label: 'Compare clients',
    state: {
      endpoint: 'runs',
      filters: [
        { id: uid(), column: 'client', operator: 'in', value: 'geth,reth,nethermind' },
        { id: uid(), column: 'status', operator: 'eq', value: 'completed' },
      ],
      orders: [],
      limit: 100,
      offset: 0,
    },
  },
  {
    label: 'Suite test durations',
    state: {
      endpoint: 'test_stats',
      filters: [{ id: uid(), column: 'suite_hash', operator: 'eq', value: '<fill in>' }],
      orders: [{ id: uid(), column: 'total_time_ns', direction: 'desc' }],
      limit: 100,
      offset: 0,
    },
  },
  {
    label: 'Slowest blocks',
    state: {
      endpoint: 'test_stats_block_logs',
      filters: [],
      orders: [{ id: uid(), column: 'timing_total_ms', direction: 'desc' }],
      limit: 20,
      offset: 0,
      selectedColumns: ['run_id', 'test_name', 'client', 'block_number', 'timing_total_ms', 'throughput_mgas_per_sec', 'block_gas_used'],
    },
  },
]

// --- API response type ---

interface QueryResponse {
  data: Record<string, unknown>[]
  total: number
  limit: number
  offset: number
}

// --- Formatting helpers ---

function formatCellValue(key: string, value: unknown): string {
  if (value === null || value === undefined) return ''
  if (TIMESTAMP_COLUMNS.has(key) && typeof value === 'string') {
    try {
      return new Date(value).toLocaleString()
    } catch {
      return String(value)
    }
  }
  return String(value)
}

// --- Component ---

export function QueryBuilderPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/query' }) as QuerySearchParams

  const [state, dispatch] = useReducer(reducer, search, (initial) => {
    const restored = searchParamsToState(initial)
    return restored ?? makeInitialState()
  })
  const [apiBaseUrl, setApiBaseUrl] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  // Sync state -> URL search params.
  useEffect(() => {
    const params = stateToSearchParams(state)
    navigate({
      to: '/query',
      search: params as Record<string, string>,
      replace: true,
    })
  }, [state, navigate])

  useEffect(() => {
    loadRuntimeConfig().then((cfg) => {
      if (cfg.api?.baseUrl) {
        setApiBaseUrl(cfg.api.baseUrl)
      }
    })
  }, [])

  const queryUrl = useMemo(
    () => (apiBaseUrl ? buildQueryUrl(state, apiBaseUrl) : ''),
    [state, apiBaseUrl],
  )

  const { data, error, isFetching, refetch } = useQuery<QueryResponse>({
    queryKey: ['query-builder', queryUrl],
    queryFn: async () => {
      const res = await fetch(queryUrl, { credentials: 'include' })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(`${res.status}: ${text}`)
      }
      return res.json()
    },
    enabled: false,
  })

  const executeQuery = useCallback(() => {
    if (queryUrl) refetch()
  }, [queryUrl, refetch])

  // Track whether a query has been executed at least once
  const hasExecuted = useRef(false)

  useEffect(() => {
    if (data) hasExecuted.current = true
  }, [data])

  // Reset when endpoint changes so switching tables doesn't auto-fetch
  useEffect(() => {
    hasExecuted.current = false
  }, [state.endpoint])

  // Auto-refetch when offset or orders change after initial execution
  useEffect(() => {
    if (hasExecuted.current && queryUrl) refetch()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.offset, state.orders])

  // Ctrl/Cmd+Enter shortcut
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
        e.preventDefault()
        executeQuery()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [executeQuery])

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(queryUrl)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }, [queryUrl])

  const columns = columnsForEndpoint(state.endpoint)
  const columnGroups = columnGroupsForEndpoint(state.endpoint)

  const rows = useMemo(() => data?.data ?? [], [data])
  const totalCount = data?.total ?? 0

  // Derive table columns from data or selected columns
  const tableColumns = useMemo(() => {
    if (rows.length > 0) return Object.keys(rows[0])
    if (state.selectedColumns.length > 0) return state.selectedColumns
    return []
  }, [rows, state.selectedColumns])

  if (apiBaseUrl === null) {
    return (
      <div className="flex min-h-64 items-center justify-center text-gray-500 dark:text-gray-400">
        API not configured
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">Query Builder</h1>

      {/* Table selector */}
      <div className="flex flex-wrap items-center gap-4">
        <span className="text-sm font-medium text-gray-500 dark:text-gray-400">Tables:</span>
        <div className="flex items-center rounded-sm border border-gray-300 dark:border-gray-600">
          <button
            onClick={() => dispatch({ type: 'SET_ENDPOINT', endpoint: 'runs' })}
            className={`px-3 py-1.5 text-sm font-medium ${
              state.endpoint === 'runs'
                ? 'bg-blue-600 text-white'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700'
            }`}
          >
            runs
          </button>
          <button
            onClick={() => dispatch({ type: 'SET_ENDPOINT', endpoint: 'test_stats' })}
            className={`px-3 py-1.5 text-sm font-medium ${
              state.endpoint === 'test_stats'
                ? 'bg-blue-600 text-white'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700'
            }`}
          >
            test_stats
          </button>
          <button
            onClick={() => dispatch({ type: 'SET_ENDPOINT', endpoint: 'test_stats_block_logs' })}
            className={`px-3 py-1.5 text-sm font-medium ${
              state.endpoint === 'test_stats_block_logs'
                ? 'bg-blue-600 text-white'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700'
            }`}
          >
            test_stats_block_logs
          </button>
        </div>
      </div>

      {/* Example queries */}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium text-gray-500 dark:text-gray-400">Examples:</span>
        {PRESETS.map((preset) => (
          <button
            key={preset.label}
            onClick={() => dispatch({ type: 'LOAD_PRESET', preset: preset.state })}
            className="rounded-sm border border-gray-300 bg-white px-3 py-1 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
          >
            {preset.label}
          </button>
        ))}
      </div>

      {/* Filters section */}
      <div className="rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
          <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Filters</span>
          <button
            onClick={() => dispatch({ type: 'ADD_FILTER' })}
            className="flex items-center gap-1 rounded-sm px-2 py-1 text-sm text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/20"
          >
            <Plus className="size-3.5" />
            Add filter
          </button>
        </div>
        {state.filters.length === 0 ? (
          <div className="px-4 py-4 text-sm text-gray-500 dark:text-gray-400">
            No filters. Click "Add filter" to add one.
          </div>
        ) : (
          <div className="flex flex-col gap-2 p-4">
            {state.filters.map((f) => (
              <div key={f.id} className="flex flex-wrap items-center gap-2">
                <select
                  value={f.column}
                  onChange={(e) =>
                    dispatch({ type: 'UPDATE_FILTER', id: f.id, field: 'column', value: e.target.value })
                  }
                  className="rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
                >
                  {columns.map((col) => (
                    <option key={col} value={col}>
                      {col}
                    </option>
                  ))}
                </select>
                <select
                  value={f.operator}
                  onChange={(e) =>
                    dispatch({ type: 'UPDATE_FILTER', id: f.id, field: 'operator', value: e.target.value })
                  }
                  className="rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
                >
                  {OPERATORS.map((op) => (
                    <option key={op.value} value={op.value}>
                      {op.label}
                    </option>
                  ))}
                </select>
                {f.operator === 'is' ? (
                  <select
                    value={f.value}
                    onChange={(e) =>
                      dispatch({ type: 'UPDATE_FILTER', id: f.id, field: 'value', value: e.target.value })
                    }
                    className="rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
                  >
                    <option value="null">null</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                ) : (
                  <input
                    type="text"
                    value={f.value}
                    onChange={(e) =>
                      dispatch({ type: 'UPDATE_FILTER', id: f.id, field: 'value', value: e.target.value })
                    }
                    placeholder="value"
                    className="rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
                  />
                )}
                <button
                  onClick={() => dispatch({ type: 'REMOVE_FILTER', id: f.id })}
                  className="rounded-sm p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-300"
                >
                  <X className="size-4" />
                </button>
                {f.operator === 'in' && (
                  <span className="text-xs text-gray-400">Comma-separated values</span>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Order & options row */}
      <div className="rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
          <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Order & Options</span>
          <button
            onClick={() => dispatch({ type: 'ADD_ORDER' })}
            className="flex items-center gap-1 rounded-sm px-2 py-1 text-sm text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/20"
          >
            <Plus className="size-3.5" />
            Add order
          </button>
        </div>
        <div className="flex flex-col gap-4 p-4">
          {state.orders.length > 0 && (
            <div className="flex flex-col gap-2">
              {state.orders.map((o) => (
                <div key={o.id} className="flex items-center gap-2">
                  <select
                    value={o.column}
                    onChange={(e) =>
                      dispatch({ type: 'UPDATE_ORDER', id: o.id, field: 'column', value: e.target.value })
                    }
                    className="rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
                  >
                    {columns.map((col) => (
                      <option key={col} value={col}>
                        {col}
                      </option>
                    ))}
                  </select>
                  <select
                    value={o.direction}
                    onChange={(e) =>
                      dispatch({ type: 'UPDATE_ORDER', id: o.id, field: 'direction', value: e.target.value })
                    }
                    className="rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
                  >
                    <option value="asc">Ascending</option>
                    <option value="desc">Descending</option>
                  </select>
                  <button
                    onClick={() => dispatch({ type: 'REMOVE_ORDER', id: o.id })}
                    className="rounded-sm p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-300"
                  >
                    <X className="size-4" />
                  </button>
                </div>
              ))}
            </div>
          )}
          <div className="flex flex-wrap items-end gap-4">
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-gray-500 dark:text-gray-400">Limit</label>
              <input
                type="number"
                min={1}
                max={1000}
                value={state.limit}
                onChange={(e) => dispatch({ type: 'SET_LIMIT', limit: Number(e.target.value) || 20 })}
                className="w-24 rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-gray-500 dark:text-gray-400">Offset</label>
              <input
                type="number"
                min={0}
                value={state.offset}
                onChange={(e) => dispatch({ type: 'SET_OFFSET', offset: Number(e.target.value) || 0 })}
                className="w-24 rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
              />
            </div>
            <span className="pb-2 text-xs text-gray-400">Max: 1000</span>
          </div>
        </div>
      </div>

      {/* Column selector */}
      <div className="rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <div className="border-b border-gray-200 px-4 py-3 dark:border-gray-700">
          <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
            Columns
          </span>
          <span className="ml-2 text-xs text-gray-400">
            {state.selectedColumns.length === 0 ? 'All columns (default)' : `${state.selectedColumns.length} selected`}
          </span>
        </div>
        <div className="flex flex-col gap-4 p-4">
          {columnGroups.map((group) => {
            const allSelected = group.columns.every((c) => state.selectedColumns.includes(c))
            return (
              <div key={group.label}>
                <div className="mb-1.5 flex items-center gap-2">
                  <span className="text-xs font-medium text-gray-500 dark:text-gray-400">{group.label}</span>
                  <button
                    onClick={() => {
                      if (allSelected) {
                        const groupSet = new Set(group.columns)
                        dispatch({ type: 'SET_COLUMNS', columns: state.selectedColumns.filter((c) => !groupSet.has(c)) })
                      } else {
                        const next = new Set([...state.selectedColumns, ...group.columns])
                        dispatch({ type: 'SET_COLUMNS', columns: [...next] })
                      }
                    }}
                    className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                  >
                    {allSelected ? 'Clear' : 'Select all'}
                  </button>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {group.columns.map((col) => {
                    const selected = state.selectedColumns.includes(col)
                    return (
                      <button
                        key={col}
                        onClick={() => {
                          const next = selected
                            ? state.selectedColumns.filter((c) => c !== col)
                            : [...state.selectedColumns, col]
                          dispatch({ type: 'SET_COLUMNS', columns: next })
                        }}
                        className={`rounded-sm border px-2 py-0.5 text-xs font-medium ${
                          selected
                            ? 'border-blue-300 bg-blue-100 text-blue-700 dark:border-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
                            : 'border-gray-300 bg-white text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-400 dark:hover:bg-gray-700'
                        }`}
                      >
                        {col}
                      </button>
                    )
                  })}
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* URL preview */}
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1 overflow-x-auto rounded-sm border border-gray-300 bg-gray-50 p-3 font-mono text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-gray-300">
          {queryUrl || 'Configure your query above...'}
        </div>
        <button
          onClick={handleCopy}
          disabled={!queryUrl}
          className="flex shrink-0 items-center gap-1.5 rounded-sm border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
        >
          {copied ? <Check className="size-4 text-green-500" /> : <Copy className="size-4" />}
          {copied ? 'Copied!' : 'Copy'}
        </button>
      </div>

      {/* API key usage hint */}
      {queryUrl && (
        <details className="rounded-sm border border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-900">
          <summary className="cursor-pointer px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300">
            Using this query with curl
          </summary>
          <div className="flex flex-col gap-3 border-t border-gray-200 px-4 py-3 dark:border-gray-700">
            <p className="text-sm text-gray-600 dark:text-gray-400">
              Authenticated endpoints require a <code className="rounded-sm bg-gray-200 px-1 py-0.5 text-xs dark:bg-gray-800">Bearer</code> API
              key (prefixed with <code className="rounded-sm bg-gray-200 px-1 py-0.5 text-xs dark:bg-gray-800">bmk_</code>).
              You can generate one from your profile settings. API keys are read-only.
            </p>
            <pre className="overflow-x-auto rounded-sm bg-gray-900 p-3 text-xs text-gray-300">
{`export BENCHMARKOOR_API_KEY="bmk_..."
curl -s -H "Authorization: Bearer $BENCHMARKOOR_API_KEY" \\
  '${queryUrl}' | jq .`}
            </pre>
          </div>
        </details>
      )}

      {/* Execute button */}
      <div>
        <button
          onClick={executeQuery}
          disabled={isFetching || !queryUrl}
          className="flex items-center gap-2 rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
        >
          {isFetching ? <Loader2 className="size-4 animate-spin" /> : null}
          {isFetching ? 'Executing...' : 'Execute Query'}
          <span className="text-xs text-blue-200">{navigator.platform.includes('Mac') ? '⌘' : 'Ctrl'}+Enter</span>
        </button>
      </div>

      {/* Error display */}
      {error && (
        <div className="rounded-sm border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
          {error instanceof Error ? error.message : 'Query failed'}
        </div>
      )}

      {/* Results */}
      {data && (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-3">
            <span className="rounded-sm bg-blue-100 px-2.5 py-0.5 text-sm font-medium text-blue-700 dark:bg-blue-900/50 dark:text-blue-300">
              {totalCount} total, showing {rows.length}
            </span>
            {/* Pagination */}
            <div className="flex items-center gap-1">
              <button
                onClick={() => dispatch({ type: 'SET_OFFSET', offset: state.offset - state.limit })}
                disabled={state.offset === 0}
                className="rounded-sm p-1 text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:text-gray-400 dark:hover:bg-gray-700"
              >
                <ChevronLeft className="size-4" />
              </button>
              <span className="text-xs text-gray-500 dark:text-gray-400">
                {state.offset + 1}&ndash;{state.offset + rows.length}
              </span>
              <button
                onClick={() => dispatch({ type: 'SET_OFFSET', offset: state.offset + state.limit })}
                disabled={rows.length < state.limit}
                className="rounded-sm p-1 text-gray-500 hover:bg-gray-100 disabled:opacity-30 dark:text-gray-400 dark:hover:bg-gray-700"
              >
                <ChevronRight className="size-4" />
              </button>
            </div>
          </div>

          {rows.length === 0 ? (
            <div className="rounded-sm bg-white py-8 text-center text-sm text-gray-500 shadow-xs dark:bg-gray-800 dark:text-gray-400">
              No results found.
            </div>
          ) : (
            <div className="overflow-x-auto rounded-sm bg-white shadow-xs dark:bg-gray-800">
              <table className="w-full divide-y divide-gray-200 dark:divide-gray-700">
                <thead>
                  <tr>
                    {tableColumns.map((col) => {
                      const currentOrder = state.orders.find((o) => o.column === col)
                      return (
                        <th
                          key={col}
                          onClick={() => dispatch({ type: 'TOGGLE_ORDER', column: col })}
                          className="cursor-pointer whitespace-nowrap px-4 py-2 text-left text-xs font-medium text-gray-500 select-none hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                        >
                          <span className="inline-flex items-center gap-1">
                            {col}
                            {currentOrder?.direction === 'asc' && <ChevronUp className="size-3" />}
                            {currentOrder?.direction === 'desc' && <ChevronDown className="size-3" />}
                          </span>
                        </th>
                      )
                    })}
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                  {rows.map((row, i) => (
                    <tr key={i} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                      {tableColumns.map((col) => {
                        const display = formatCellValue(col, row[col])
                        return (
                          <td
                            key={col}
                            title={display}
                            className="max-w-xs truncate whitespace-nowrap px-4 py-2 text-sm text-gray-900 dark:text-gray-200"
                          >
                            {display}
                          </td>
                        )
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
