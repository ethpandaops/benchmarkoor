import { useReducer, useEffect, useMemo, useCallback, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Copy, Check, Loader2, Plus, X, ChevronLeft, ChevronRight } from 'lucide-react'
import { loadRuntimeConfig } from '@/config/runtime'

// --- Column & operator metadata ---

const RUNS_COLUMNS = [
  'id', 'discovery_path', 'run_id', 'timestamp', 'timestamp_end',
  'suite_hash', 'status', 'termination_reason', 'has_result', 'instance_id',
  'client', 'image', 'rollback_strategy', 'tests_total', 'tests_passed',
  'tests_failed', 'indexed_at', 'reindexed_at',
]

const TEST_DURATION_COLUMNS = [
  'id', 'suite_hash', 'test_name', 'run_id', 'client',
  'total_gas_used', 'total_time_ns', 'total_mgas_s',
  'setup_gas_used', 'setup_time_ns', 'setup_mgas_s',
  'test_gas_used', 'test_time_ns', 'test_mgas_s',
  'run_start', 'run_end',
]

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

const TIMESTAMP_COLUMNS = new Set([
  'timestamp', 'timestamp_end', 'indexed_at', 'reindexed_at', 'run_start', 'run_end',
])

// --- Types ---

type Endpoint = 'runs' | 'test_durations'

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

// --- Reducer ---

type Action =
  | { type: 'SET_ENDPOINT'; endpoint: Endpoint }
  | { type: 'ADD_FILTER' }
  | { type: 'REMOVE_FILTER'; id: string }
  | { type: 'UPDATE_FILTER'; id: string; field: keyof FilterRow; value: string }
  | { type: 'ADD_ORDER' }
  | { type: 'REMOVE_ORDER'; id: string }
  | { type: 'UPDATE_ORDER'; id: string; field: 'column' | 'direction'; value: string }
  | { type: 'SET_LIMIT'; limit: number }
  | { type: 'SET_OFFSET'; offset: number }
  | { type: 'SET_COLUMNS'; columns: string[] }
  | { type: 'LOAD_PRESET'; preset: Omit<QueryBuilderState, 'selectedColumns'> & { selectedColumns?: string[] } }

let nextId = 1
function uid() {
  return String(nextId++)
}

function columnsForEndpoint(ep: Endpoint) {
  return ep === 'runs' ? RUNS_COLUMNS : TEST_DURATION_COLUMNS
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

// --- URL generation ---

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
  state: Omit<QueryBuilderState, 'selectedColumns'>
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
      orders: [{ id: uid(), column: 'tests_failed', direction: 'desc' }],
      limit: 100,
      offset: 0,
    },
  },
  {
    label: 'Slow tests',
    state: {
      endpoint: 'test_durations',
      filters: [],
      orders: [{ id: uid(), column: 'test_mgas_s', direction: 'asc' }],
      limit: 20,
      offset: 0,
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
      endpoint: 'test_durations',
      filters: [{ id: uid(), column: 'suite_hash', operator: 'eq', value: '<fill in>' }],
      orders: [{ id: uid(), column: 'total_time_ns', direction: 'desc' }],
      limit: 100,
      offset: 0,
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
  const [state, dispatch] = useReducer(reducer, undefined, makeInitialState)
  const [apiBaseUrl, setApiBaseUrl] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

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

  const { data, isFetching, refetch } = useQuery<QueryResponse>({
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
      {/* Header row */}
      <div className="flex flex-wrap items-center gap-4">
        <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">Query Builder</h1>
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
            onClick={() => dispatch({ type: 'SET_ENDPOINT', endpoint: 'test_durations' })}
            className={`px-3 py-1.5 text-sm font-medium ${
              state.endpoint === 'test_durations'
                ? 'bg-blue-600 text-white'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700'
            }`}
          >
            test_durations
          </button>
        </div>
      </div>

      {/* Example queries */}
      <div className="flex flex-wrap gap-2">
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
                  <div className="flex flex-col">
                    <input
                      type="text"
                      value={f.value}
                      onChange={(e) =>
                        dispatch({ type: 'UPDATE_FILTER', id: f.id, field: 'value', value: e.target.value })
                      }
                      placeholder="value"
                      className="rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200"
                    />
                    {f.operator === 'in' && (
                      <span className="mt-0.5 text-xs text-gray-400">Comma-separated values</span>
                    )}
                  </div>
                )}
                <button
                  onClick={() => dispatch({ type: 'REMOVE_FILTER', id: f.id })}
                  className="rounded-sm p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-300"
                >
                  <X className="size-4" />
                </button>
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
              <span className="text-xs text-gray-400">Max: 1000</span>
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
        <div className="flex flex-wrap gap-2 p-4">
          {columns.map((col) => {
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
                className={`rounded-sm border px-2.5 py-1 text-xs font-medium ${
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
                    {tableColumns.map((col) => (
                      <th
                        key={col}
                        className="whitespace-nowrap px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400"
                      >
                        {col}
                      </th>
                    ))}
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
