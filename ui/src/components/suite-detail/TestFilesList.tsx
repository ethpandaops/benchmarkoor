import { Fragment, useState } from 'react'
import clsx from 'clsx'
import { Copy, Check, Search, ArrowDown, ArrowUp } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import type { SuiteFile, SuiteTest } from '@/api/types'
import { fetchText } from '@/api/client'
import { Pagination } from '@/components/shared/Pagination'
import { Spinner } from '@/components/shared/Spinner'
import { Badge } from '@/components/shared/Badge'
import { Modal } from '@/components/shared/Modal'
import { TestName } from '@/components/shared/TestName'
import { getOpcodeCategory, getCategoryColor } from '@/utils/opcodeCategories'
import { testNameMatches, toggleSearchTerm, TEST_FILTER_HINT } from '@/utils/eestNameFilter'
import { formatBytes } from '@/utils/format'

export type OpcodeSortMode = 'name' | 'count'

import type { PayloadSort, PayloadSortCol } from './payloadSort'

// Per-test totals + ratios for payload-sizes sort + display. null fields
// mean "no payload_sizes.test data for this test"; the sort comparator
// pushes those rows to the bottom regardless of direction.
interface PayloadTotals {
  sszFull: number | null
  sszBal: number | null
  sszSnappy: number | null
  sszBalPct: number | null
  sszSnappyPct: number | null
  jsonFull: number | null
  jsonBal: number | null
  jsonBalPct: number | null
}

const EMPTY_PAYLOAD_TOTALS: PayloadTotals = {
  sszFull: null,
  sszBal: null,
  sszSnappy: null,
  sszBalPct: null,
  sszSnappyPct: null,
  jsonFull: null,
  jsonBal: null,
  jsonBalPct: null,
}

function payloadTotals(test: SuiteTest): PayloadTotals {
  const t = test.payload_sizes?.test
  if (!t) return EMPTY_PAYLOAD_TOTALS
  const sum = (xs: number[] | undefined) => xs ? xs.reduce((a, b) => a + b, 0) : 0
  const sszFull = sum(t.ssz_full)
  const sszBal = sum(t.ssz_bal)
  const sszSnappy = sum(t.ssz_full_snappy)
  const jsonFull = sum(t.json_full)
  const jsonBal = sum(t.json_bal)
  return {
    sszFull,
    sszBal,
    sszSnappy,
    sszBalPct: sszFull > 0 ? (sszBal / sszFull) * 100 : null,
    sszSnappyPct: sszFull > 0 ? (sszSnappy / sszFull) * 100 : null,
    jsonFull,
    jsonBal,
    jsonBalPct: jsonFull > 0 ? (jsonBal / jsonFull) * 100 : null,
  }
}

function compareNullsLast(a: number | null, b: number | null, dir: 'asc' | 'desc'): number {
  if (a === null && b === null) return 0
  if (a === null) return 1
  if (b === null) return -1
  return dir === 'asc' ? a - b : b - a
}

interface TestFilesListProps {
  // For pre_run_steps - simple file list
  files?: SuiteFile[]
  // For tests - tests with steps
  tests?: SuiteTest[]
  suiteHash: string
  type: 'tests' | 'pre_run_steps'
  currentPage?: number
  onPageChange?: (page: number) => void
  searchQuery?: string
  onSearchChange?: (query: string | undefined) => void
  detailIndex?: number
  onDetailChange?: (index: number | undefined) => void
  opcodeSort?: OpcodeSortMode
  onOpcodeSortChange?: (sort: OpcodeSortMode) => void
  testView?: 'general' | 'payload-sizes' | 'payload-sizes-json'
  onTestViewChange?: (mode: 'general' | 'payload-sizes' | 'payload-sizes-json') => void
  payloadSort?: PayloadSort | null
  onPayloadSortChange?: (sort: PayloadSort | null) => void
  /** Hide the in-table search input (the caller drives the search via the global ?q= bar). */
  hideSearchInput?: boolean
}

const PAGE_SIZE_OPTIONS = [50, 100, 200] as const
const DEFAULT_PAGE_SIZE = 100

function CopyIcon({ className }: { className?: string }) {
  return <Copy className={className} />
}

function CheckIcon({ className }: { className?: string }) {
  return <Check className={className} />
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="inline-flex items-center gap-1 rounded-sm px-2 py-1 text-xs font-medium text-gray-500 hover:bg-gray-200 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
      title={`Copy ${label}`}
    >
      {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
      {copied ? 'Copied' : `Copy ${label}`}
    </button>
  )
}

// For pre-run steps: path is suites/${suiteHash}/${file.og_path}/pre_run.request
// For test steps: path is suites/${suiteHash}/${testName}/${stepType}.request
function FileContent({
  suiteHash,
  stepType,
  file,
  testName,
  hidePath,
}: {
  suiteHash: string
  stepType: string
  file: SuiteFile
  testName?: string
  hidePath?: boolean
}) {
  // Build path based on whether this is a test step or pre-run step
  const path = testName
    ? `suites/${suiteHash}/${testName}/${stepType}.request`
    : `suites/${suiteHash}/${file.og_path}/pre_run.request`

  const { data, isLoading, error } = useQuery({
    queryKey: ['suite', suiteHash, stepType, testName, file.og_path],
    queryFn: async () => {
      const { data, status } = await fetchText(path)
      if (!data) {
        throw new Error(`Failed to fetch file: ${status}`)
      }
      return data
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 p-4">
        <Spinner size="sm" />
        <span className="text-sm/6 text-gray-500">Loading file content...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-4 text-sm/6 text-red-600 dark:text-red-400">
        Failed to load file: {error.message}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {!hidePath && (
        <div className="flex flex-col gap-1">
          <div className="flex items-center justify-between">
            <span className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Original Path
            </span>
            <CopyButton text={file.og_path} label="path" />
          </div>
          <div className="break-all font-mono text-sm/6 text-gray-700 dark:text-gray-300">{file.og_path}</div>
        </div>
      )}
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between">
          <span className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
            Content
          </span>
          <button
            onClick={async (e) => {
              e.stopPropagation()
              await navigator.clipboard.writeText(data || '')
              const btn = e.currentTarget
              btn.textContent = 'Copied!'
              setTimeout(() => (btn.textContent = 'Copy'), 2000)
            }}
            className="rounded-sm px-2 py-1 text-xs font-medium text-gray-500 hover:bg-gray-200 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
          >
            Copy
          </button>
        </div>
        <div className="overflow-x-auto">
          <pre className="max-h-96 overflow-y-auto rounded-sm bg-gray-900 p-4 font-mono text-xs/5 text-gray-100">
            {data}
          </pre>
        </div>
      </div>
    </div>
  )
}

function SearchIcon({ className }: { className?: string }) {
  return <Search className={className} />
}

// Component for displaying EEST fixture info and opcode counts
export function EESTInfoContent({ test, opcodeSort, onOpcodeSortChange }: { test: SuiteTest; opcodeSort: OpcodeSortMode; onOpcodeSortChange: (sort: OpcodeSortMode) => void }) {
  const info = test.eest?.info
  const opcodes = test.opcode_count ?? info?.opcode_count

  const fields = info ? [
    { label: 'Description', value: info.description },
    { label: 'Comment', value: info.comment },
    { label: 'Fixture Format', value: info['fixture-format'] },
    { label: 'Filling Tool', value: info['filling-transition-tool'] },
    { label: 'Hash', value: info.hash },
    { label: 'URL', value: info.url },
  ].filter((f) => f.value) : []

  const hasOpcodes = opcodes && Object.keys(opcodes).length > 0

  if (fields.length === 0 && !hasOpcodes) return null

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <Badge variant="default">{info ? 'EEST Info' : 'Opcode Info'}</Badge>
      </div>
      <div className="rounded-sm border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm/6">
          {fields.map(({ label, value }) => (
            <Fragment key={label}>
              <dt className="font-medium text-gray-500 dark:text-gray-400">{label}</dt>
              <dd className="break-all text-gray-900 dark:text-gray-100">
                {label === 'URL' && value ? (
                  <a
                    href={value}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-blue-600 hover:underline dark:text-blue-400"
                    onClick={(e) => e.stopPropagation()}
                  >
                    {value}
                  </a>
                ) : (
                  <span className="font-mono">{value}</span>
                )}
              </dd>
            </Fragment>
          ))}
        </dl>
        {hasOpcodes && (
          <div className="mt-3 flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <span className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Opcode Count</span>
              <button
                onClick={() => onOpcodeSortChange(opcodeSort === 'name' ? 'count' : 'name')}
                className="rounded-sm px-2 py-0.5 text-xs font-medium text-gray-500 hover:bg-gray-200 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-600 dark:hover:text-gray-200"
              >
                Sort by {opcodeSort === 'name' ? 'count' : 'name'}
              </button>
            </div>
            <div className="flex flex-wrap gap-1">
              {Object.entries(opcodes!)
                .sort(opcodeSort === 'name'
                  ? ([a], [b]) => a.localeCompare(b)
                  : ([, a], [, b]) => b - a
                )
                .map(([opcode, count]) => {
                  const category = getOpcodeCategory(opcode)
                  return (
                    <span
                      key={opcode}
                      title={category}
                      className="inline-flex items-center gap-1 rounded-xs bg-gray-100 px-2 py-0.5 font-mono text-xs/5 dark:bg-gray-700"
                      style={{ color: getCategoryColor(category, document.documentElement.classList.contains('dark')) }}
                    >
                      {opcode}
                      <span className="opacity-60">{count}</span>
                    </span>
                  )
                })}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// Payload-size panel for the test-details modal. Renders one table per
// populated step (setup/test/cleanup), with one row per engine_newPayload
// in that step plus a totals row. The three columns are the SSZ raw,
// BAL, and snappy byte counts, each shown alongside its % of the SSZ
// raw size for BAL/snappy rows.
function PayloadSizesContent({ test }: { test: SuiteTest }) {
  const ps = test.payload_sizes
  if (!ps) return null
  const steps: { label: string; buckets: NonNullable<typeof ps.test> }[] = []
  if (ps.setup) steps.push({ label: 'Setup', buckets: ps.setup })
  if (ps.test) steps.push({ label: 'Test', buckets: ps.test })
  if (ps.cleanup) steps.push({ label: 'Cleanup', buckets: ps.cleanup })
  if (steps.length === 0) return null

  const pctCell = (numerator: number, denom: number) => {
    if (denom <= 0) return null
    return (
      <span className="ml-1 text-xs/5 text-gray-500 dark:text-gray-400">
        ({((numerator / denom) * 100).toFixed(1)}%)
      </span>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <Badge variant="default">Payload Sizes</Badge>
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">per engine_newPayload</span>
      </div>
      <div className="flex flex-col gap-4">
        {steps.map(({ label, buckets }) => {
          const n = Math.max(
            buckets.ssz_full.length,
            buckets.ssz_bal.length,
            buckets.ssz_full_snappy.length,
            buckets.json_full.length,
            buckets.json_bal.length,
          )
          const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0)
          const sszFullTotal = sum(buckets.ssz_full)
          const sszBalTotal = sum(buckets.ssz_bal)
          const sszSnappyTotal = sum(buckets.ssz_full_snappy)
          const jsonFullTotal = sum(buckets.json_full)
          const jsonBalTotal = sum(buckets.json_bal)
          return (
            <div key={label} className="overflow-x-auto rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
              <div className="border-b border-gray-200 px-4 py-2 text-sm/6 font-medium text-gray-700 dark:border-gray-700 dark:text-gray-200">
                {label} step
                <span className="ml-2 text-xs/5 font-normal text-gray-500 dark:text-gray-400">
                  {n} newPayload{n === 1 ? '' : 's'}
                </span>
              </div>
              <table className="min-w-full">
                <thead className="bg-gray-50/50 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:bg-gray-800/50 dark:text-gray-400">
                  <tr>
                    <th className="w-12 px-3 py-2">#</th>
                    <th className="px-3 py-2 text-right" title="Full SSZ-encoded ExecutionPayload (BAL inline)">SSZ</th>
                    <th className="px-3 py-2 text-right" title="SSZ-encoded BlockAccessList, decoded from the wire hex">SSZ BAL</th>
                    <th className="px-3 py-2 text-right" title="snappy(SSZ)">SSZ Snappy</th>
                    <th className="border-l border-gray-200 px-3 py-2 text-right dark:border-gray-700" title="Canonical JSON of the same ExecutionPayload">JSON</th>
                    <th className="px-3 py-2 text-right" title="BAL hex string length as it appears in JSON (no quotes)">JSON BAL</th>
                  </tr>
                </thead>
                <tbody>
                  {Array.from({ length: n }).map((_, i) => {
                    const sszFull = buckets.ssz_full[i] ?? 0
                    const sszBal = buckets.ssz_bal[i] ?? 0
                    const sszSnap = buckets.ssz_full_snappy[i] ?? 0
                    const jsonFull = buckets.json_full[i] ?? 0
                    const jsonBal = buckets.json_bal[i] ?? 0
                    return (
                      <tr key={i} className="border-t border-gray-200 dark:border-gray-700">
                        <td className="px-3 py-1.5 font-mono text-xs/5 text-gray-500 dark:text-gray-400">#{i + 1}</td>
                        <td className="px-3 py-1.5 text-right font-mono text-sm/6 text-gray-900 dark:text-gray-100">{formatBytes(sszFull)}</td>
                        <td className="px-3 py-1.5 text-right font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                          {formatBytes(sszBal)}{pctCell(sszBal, sszFull)}
                        </td>
                        <td className="px-3 py-1.5 text-right font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                          {formatBytes(sszSnap)}{pctCell(sszSnap, sszFull)}
                        </td>
                        <td className="border-l border-gray-200 px-3 py-1.5 text-right font-mono text-sm/6 text-gray-900 dark:border-gray-700 dark:text-gray-100">{formatBytes(jsonFull)}</td>
                        <td className="px-3 py-1.5 text-right font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                          {formatBytes(jsonBal)}{pctCell(jsonBal, jsonFull)}
                        </td>
                      </tr>
                    )
                  })}
                  {n > 1 && (
                    <tr className="border-t-2 border-gray-300 bg-gray-50/50 dark:border-gray-600 dark:bg-gray-800/50">
                      <td className="px-3 py-1.5 text-xs/5 font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">Total</td>
                      <td className="px-3 py-1.5 text-right font-mono text-sm/6 font-semibold text-gray-900 dark:text-gray-100">{formatBytes(sszFullTotal)}</td>
                      <td className="px-3 py-1.5 text-right font-mono text-sm/6 font-semibold text-gray-900 dark:text-gray-100">
                        {formatBytes(sszBalTotal)}{pctCell(sszBalTotal, sszFullTotal)}
                      </td>
                      <td className="px-3 py-1.5 text-right font-mono text-sm/6 font-semibold text-gray-900 dark:text-gray-100">
                        {formatBytes(sszSnappyTotal)}{pctCell(sszSnappyTotal, sszFullTotal)}
                      </td>
                      <td className="border-l border-gray-200 px-3 py-1.5 text-right font-mono text-sm/6 font-semibold text-gray-900 dark:border-gray-700 dark:text-gray-100">{formatBytes(jsonFullTotal)}</td>
                      <td className="px-3 py-1.5 text-right font-mono text-sm/6 font-semibold text-gray-900 dark:text-gray-100">
                        {formatBytes(jsonBalTotal)}{pctCell(jsonBalTotal, jsonFullTotal)}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// Component for displaying test steps content (for tests with setup/test/cleanup)
function TestStepsContent({ suiteHash, test, opcodeSort, onOpcodeSortChange }: { suiteHash: string; test: SuiteTest; opcodeSort: OpcodeSortMode; onOpcodeSortChange: (sort: OpcodeSortMode) => void }) {
  const steps = [
    { key: 'setup', label: 'Setup step', file: test.setup },
    { key: 'test', label: 'Test step', file: test.test },
    { key: 'cleanup', label: 'Cleanup step', file: test.cleanup },
  ].filter((s) => s.file) as { key: string; label: string; file: SuiteFile }[]

  const hasPayloadSizes = !!test.payload_sizes
  const hasInfo =
    !!test.eest?.info ||
    (test.opcode_count && Object.keys(test.opcode_count).length > 0) ||
    hasPayloadSizes
  if (steps.length === 0 && !hasInfo) {
    return <div className="p-4 text-sm/6 text-gray-500">No step files available</div>
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <div className="text-sm/6 text-gray-900 dark:text-gray-100">
          <TestName name={test.name} showRawBelow showCopy />
        </div>
      </div>
      <EESTInfoContent test={test} opcodeSort={opcodeSort} onOpcodeSortChange={onOpcodeSortChange} />
      <PayloadSizesContent test={test} />
      {steps.map(({ key, label, file }) => (
        <div key={key} className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <Badge variant="default">{label}</Badge>
          </div>
          <FileContent suiteHash={suiteHash} stepType={key} file={file} testName={test.name} hidePath={hasInfo} />
        </div>
      ))}
    </div>
  )
}

export function TestFilesList({
  files,
  tests,
  suiteHash,
  type,
  currentPage: controlledPage,
  onPageChange,
  searchQuery,
  onSearchChange,
  detailIndex,
  onDetailChange,
  opcodeSort = 'name',
  onOpcodeSortChange,
  testView,
  onTestViewChange,
  payloadSort: payloadSortProp,
  onPayloadSortChange,
  hideSearchInput = false,
}: TestFilesListProps) {
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  // Switches which columns the tests table renders. "general" shows the
  // step badges; "payload-sizes" hides Steps and shows the three SSZ totals.
  // URL-driven via the parent (defaults to "general"), with a fallback to
  // local state when no callback is provided so this component can be used
  // standalone.
  const [localViewMode, setLocalViewMode] = useState<'general' | 'payload-sizes' | 'payload-sizes-json'>('general')
  const viewMode = testView ?? localViewMode
  const setViewMode = (mode: 'general' | 'payload-sizes' | 'payload-sizes-json') => {
    if (onTestViewChange) onTestViewChange(mode)
    else setLocalViewMode(mode)
  }
  // Payload-sizes table sort. URL-driven via the parent; falls back to
  // local state so the component still works standalone. null = original
  // index order (no sort active).
  const [localPayloadSort, setLocalPayloadSort] = useState<PayloadSort | null>(null)
  const payloadSort = payloadSortProp !== undefined ? payloadSortProp : localPayloadSort
  const setPayloadSort = (next: PayloadSort | null) => {
    if (onPayloadSortChange) onPayloadSortChange(next)
    else setLocalPayloadSort(next)
  }
  const togglePayloadSort = (col: PayloadSortCol) => {
    if (!payloadSort || payloadSort.col !== col) setPayloadSort({ col, dir: 'desc' })
    else if (payloadSort.dir === 'desc') setPayloadSort({ col, dir: 'asc' })
    else setPayloadSort(null)
  }
  const currentPage = controlledPage ?? 1
  const search = searchQuery ?? ''

  const handleOpcodeSortChange = (sort: OpcodeSortMode) => {
    onOpcodeSortChange?.(sort)
  }

  // For pre_run_steps, use files; for tests, use tests
  const isPreRunSteps = type === 'pre_run_steps'
  const hasGenesis = !isPreRunSteps && (tests ?? []).some((t) => !!t.genesis)
  const hasPayloadSizes = !isPreRunSteps && (tests ?? []).some((t) => !!t.payload_sizes?.test)
  const itemCount = isPreRunSteps ? (files?.length ?? 0) : (tests?.length ?? 0)

  // Filter and index items
  const filteredItems = isPreRunSteps
    ? (files ?? [])
        .map((file, index) => ({ file, originalIndex: index + 1 }))
        .filter(({ file }) => {
          const searchLower = search.toLowerCase()
          return file.og_path.toLowerCase().includes(searchLower)
        })
    : (tests ?? [])
        .map((test, index) => ({ test, originalIndex: index + 1 }))
        .filter(({ test }) => testNameMatches(test.name, search))

  // Apply payload-sizes sort only when in that view and a sort is active.
  // Tests without payload data are pushed to the bottom regardless of dir.
  const sortedItems = (!isPreRunSteps && viewMode !== 'general' && payloadSort)
    ? [...(filteredItems as { test: SuiteTest; originalIndex: number }[])].sort((a, b) => {
        const ps = payloadSort
        if (ps.col === 'index') {
          return compareNullsLast(a.originalIndex, b.originalIndex, ps.dir)
        }
        const ta = payloadTotals(a.test)
        const tb = payloadTotals(b.test)
        const pick = (t: PayloadTotals): number | null => {
          switch (ps.col) {
            case 'ssz_full': return t.sszFull
            case 'ssz_bal': return t.sszBal
            case 'ssz_bal_pct': return t.sszBalPct
            case 'ssz_snappy': return t.sszSnappy
            case 'ssz_snappy_pct': return t.sszSnappyPct
            case 'json_full': return t.jsonFull
            case 'json_bal': return t.jsonBal
            case 'json_bal_pct': return t.jsonBalPct
            // 'index' is handled by the early return above; this is unreachable.
            default: return null
          }
        }
        return compareNullsLast(pick(ta), pick(tb), ps.dir)
      })
    : filteredItems

  const totalPages = Math.ceil(sortedItems.length / pageSize)
  const paginatedItems = sortedItems.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  const setCurrentPage = (page: number) => {
    if (onPageChange) {
      onPageChange(page)
    }
  }

  const handlePageSizeChange = (newSize: number) => {
    setPageSize(newSize)
    setCurrentPage(1)
  }

  const handleSearchChange = (value: string) => {
    if (onSearchChange) {
      onSearchChange(value || undefined)
    }
  }

  const paginationControls = (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <span className="text-sm/6 text-gray-500 dark:text-gray-400">
          {search ? `${filteredItems.length} of ${itemCount}` : itemCount} {isPreRunSteps ? 'files' : 'tests'}
        </span>
        <span className="text-gray-300 dark:text-gray-600">|</span>
        <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
        <select
          value={pageSize}
          onChange={(e) => handlePageSizeChange(Number(e.target.value))}
          className="rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm/6 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
        >
          {PAGE_SIZE_OPTIONS.map((size) => (
            <option key={size} value={size}>
              {size}
            </option>
          ))}
        </select>
        <span className="text-sm/6 text-gray-500 dark:text-gray-400">per page</span>
      </div>

      {totalPages > 1 && (
        <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setCurrentPage} />
      )}
    </div>
  )

  // Render for pre_run_steps (simple file list)
  if (isPreRunSteps) {
    const fileItems = paginatedItems as { file: SuiteFile; originalIndex: number }[]

    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-4">
          <div className="relative flex-1">
            <SearchIcon className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-gray-400" />
            <input
              type="text"
              value={search}
              onChange={(e) => handleSearchChange(e.target.value)}
              placeholder="Search by path..."
              className="w-full rounded-sm border border-gray-300 bg-white py-2 pl-10 pr-4 text-sm/6 text-gray-900 placeholder:text-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 dark:placeholder:text-gray-500"
            />
          </div>
        </div>
        {filteredItems.length > 0 && paginationControls}
        <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
          <table className="w-full table-fixed divide-y divide-gray-200 dark:divide-gray-700">
            <thead className="bg-gray-50 dark:bg-gray-900">
              <tr>
                <th className="w-16 px-2 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  #
                </th>
                <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Path
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
              {fileItems.map(({ file, originalIndex }) => (
                <tr
                  key={originalIndex}
                  onClick={() => onDetailChange?.(originalIndex)}
                  className="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
                >
                  <td className="px-2 py-2 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400">
                    {originalIndex}
                  </td>
                  <td
                    className="truncate px-4 py-2 font-mono text-xs/5 text-gray-900 dark:text-gray-100"
                    title={file.og_path}
                  >
                    {file.og_path}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {filteredItems.length === 0 && (
            <div className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
              {search ? `No files matching "${search}"` : 'No files found'}
            </div>
          )}
        </div>

        {filteredItems.length > 0 && paginationControls}

        {(() => {
          const selectedFileItem = detailIndex != null ? (files ?? [])[detailIndex - 1] : undefined
          return (
            <Modal
              isOpen={!!selectedFileItem}
              onClose={() => onDetailChange?.(undefined)}
              title={detailIndex != null ? `Test #${detailIndex}` : undefined}
            >
              {selectedFileItem && (
                <FileContent suiteHash={suiteHash} stepType="pre_run_steps" file={selectedFileItem} />
              )}
            </Modal>
          )
        })()}
      </div>
    )
  }

  // Render for tests (tests with steps)
  const testItems = paginatedItems as { test: SuiteTest; originalIndex: number }[]

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-4">
        {!hideSearchInput && (
          <div className="relative flex-1">
            <SearchIcon className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-gray-400" />
            <input
              type="text"
              value={search}
              onChange={(e) => handleSearchChange(e.target.value)}
              placeholder="Search… or e.g. opcode:ORIGIN gas:90M"
              title={TEST_FILTER_HINT}
              className="w-full rounded-sm border border-gray-300 bg-white py-2 pl-10 pr-4 text-sm/6 text-gray-900 placeholder:text-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 dark:placeholder:text-gray-500"
            />
          </div>
        )}
        {hasPayloadSizes && (
          <div className="flex shrink-0 items-center gap-1 rounded-sm border border-gray-300 bg-white p-0.5 text-xs/5 dark:border-gray-600 dark:bg-gray-800">
            {([
              { value: 'general', label: 'General' },
              { value: 'payload-sizes', label: 'Payload sizes (SSZ)' },
              { value: 'payload-sizes-json', label: 'Payload sizes (JSON)' },
            ] as const).map((opt) => (
              <button
                key={opt.value}
                type="button"
                onClick={() => setViewMode(opt.value)}
                className={clsx(
                  'rounded-xs px-2 py-1 font-medium transition-colors',
                  viewMode === opt.value
                    ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                    : 'text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-700',
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
        )}
      </div>
      {filteredItems.length > 0 && paginationControls}
      <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <table className="w-full table-fixed divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <th className="w-16 px-2 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                {viewMode !== 'general' && hasPayloadSizes ? (() => {
                  const active = payloadSort?.col === 'index'
                  const Icon = active ? (payloadSort.dir === 'desc' ? ArrowDown : ArrowUp) : null
                  return (
                    <button
                      type="button"
                      onClick={() => togglePayloadSort('index')}
                      className={clsx(
                        'inline-flex w-full cursor-pointer items-center justify-end gap-1 hover:text-gray-700 dark:hover:text-gray-200',
                        active && 'text-gray-700 dark:text-gray-200',
                      )}
                      title="Original position in the suite"
                    >
                      #
                      {Icon ? <Icon className="size-3" /> : null}
                    </button>
                  )
                })() : '#'}
              </th>
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Test Name
              </th>
              {hasGenesis && (
                <th className="w-40 px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Genesis
                </th>
              )}
              {viewMode === 'general' && (
                <th className="w-48 px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Steps
                </th>
              )}
              {viewMode !== 'general' && hasPayloadSizes && (() => {
                const sortHeader = (col: PayloadSortCol, label: string, tooltip: string) => {
                  const active = payloadSort?.col === col
                  const Icon = active ? (payloadSort.dir === 'desc' ? ArrowDown : ArrowUp) : null
                  return (
                    <th
                      key={col}
                      className="w-24 px-3 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400"
                      title={tooltip}
                    >
                      <button
                        type="button"
                        onClick={() => togglePayloadSort(col)}
                        className={clsx(
                          'inline-flex w-full cursor-pointer items-center justify-end gap-1 hover:text-gray-700 dark:hover:text-gray-200',
                          active && 'text-gray-700 dark:text-gray-200',
                        )}
                      >
                        {label}
                        {Icon ? <Icon className="size-3" /> : null}
                      </button>
                    </th>
                  )
                }
                if (viewMode === 'payload-sizes') {
                  return (
                    <>
                      {sortHeader('ssz_full', 'SSZ', 'Full SSZ-encoded ExecutionPayload (BAL inline) — sum across all newPayloads in the test step')}
                      {sortHeader('ssz_bal', 'SSZ BAL', 'SSZ-encoded BlockAccessList — sum across all newPayloads in the test step')}
                      {sortHeader('ssz_bal_pct', 'BAL %', 'SSZ BAL as percentage of SSZ Full')}
                      {sortHeader('ssz_snappy', 'SSZ Snappy', 'snappy(SSZ) — sum across all newPayloads in the test step')}
                      {sortHeader('ssz_snappy_pct', 'Snappy %', 'SSZ Snappy as percentage of SSZ Full')}
                    </>
                  )
                }
                // payload-sizes-json
                return (
                  <>
                    {sortHeader('json_full', 'JSON', 'Canonical JSON-encoded ExecutionPayload — sum across all newPayloads in the test step')}
                    {sortHeader('json_bal', 'JSON BAL', 'BAL hex string as it appears in JSON (no quotes) — sum across all newPayloads in the test step')}
                    {sortHeader('json_bal_pct', 'BAL %', 'JSON BAL as percentage of JSON Full')}
                  </>
                )
              })()}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {testItems.map(({ test, originalIndex }) => (
              <tr
                key={originalIndex}
                onClick={() => onDetailChange?.(originalIndex)}
                className="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
              >
                <td className="px-2 py-2 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400">
                  {originalIndex}
                </td>
                <td className="max-w-md px-4 py-2">
                  <TestName
                    name={test.name}
                    onChipClick={onSearchChange ? (term) => onSearchChange(toggleSearchTerm(search, term) || undefined) : undefined}
                    activeQuery={search}
                  />
                </td>
                {hasGenesis && (
                  <td className="truncate px-4 py-2 font-mono text-xs/5 text-gray-500 dark:text-gray-400" title={test.genesis}>
                    {test.genesis ?? '—'}
                  </td>
                )}
                {viewMode === 'general' && (
                  <td className="px-4 py-2">
                    <div className="flex gap-1">
                      {test.setup && <Badge variant="default">Setup</Badge>}
                      {test.test && <Badge variant="default">Test</Badge>}
                      {test.cleanup && <Badge variant="default">Cleanup</Badge>}
                    </div>
                  </td>
                )}
                {viewMode !== 'general' && hasPayloadSizes && (() => {
                  const totals = payloadTotals(test)
                  const placeholder = <span className="text-gray-300 dark:text-gray-600">—</span>
                  const bytesCell = (key: string, value: number | null) => (
                    <td key={key} className="px-3 py-2 text-right font-mono text-xs/5 text-gray-700 dark:text-gray-300">
                      {value === null ? placeholder : formatBytes(value)}
                    </td>
                  )
                  const pctCell = (key: string, value: number | null) => (
                    <td key={key} className="px-3 py-2 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400">
                      {value === null ? placeholder : `${value.toFixed(1)}%`}
                    </td>
                  )
                  if (viewMode === 'payload-sizes') {
                    return (
                      <>
                        {bytesCell('ssz_full', totals.sszFull)}
                        {bytesCell('ssz_bal', totals.sszBal)}
                        {pctCell('ssz_bal_pct', totals.sszBalPct)}
                        {bytesCell('ssz_snappy', totals.sszSnappy)}
                        {pctCell('ssz_snappy_pct', totals.sszSnappyPct)}
                      </>
                    )
                  }
                  // payload-sizes-json
                  return (
                    <>
                      {bytesCell('json_full', totals.jsonFull)}
                      {bytesCell('json_bal', totals.jsonBal)}
                      {pctCell('json_bal_pct', totals.jsonBalPct)}
                    </>
                  )
                })()}
              </tr>
            ))}
          </tbody>
        </table>
        {filteredItems.length === 0 && (
          <div className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
            {search ? `No tests matching "${search}"` : 'No tests found'}
          </div>
        )}
      </div>

      {filteredItems.length > 0 && paginationControls}

      {(() => {
        const selectedTestItem = detailIndex != null ? (tests ?? [])[detailIndex - 1] : undefined
        return (
          <Modal
            isOpen={!!selectedTestItem}
            onClose={() => onDetailChange?.(undefined)}
            title={detailIndex != null ? `Test #${detailIndex}` : undefined}
          >
            {selectedTestItem && (
              <TestStepsContent suiteHash={suiteHash} test={selectedTestItem} opcodeSort={opcodeSort} onOpcodeSortChange={handleOpcodeSortChange} />
            )}
          </Modal>
        )
      })()}
    </div>
  )
}
