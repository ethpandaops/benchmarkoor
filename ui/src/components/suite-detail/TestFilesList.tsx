import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { SuiteFile, SuiteTest } from '@/api/types'
import { fetchText } from '@/api/client'
import { Pagination } from '@/components/shared/Pagination'
import { Spinner } from '@/components/shared/Spinner'
import { Badge } from '@/components/shared/Badge'
import { Modal } from '@/components/shared/Modal'
import { getOpcodeCategory, getCategoryColor } from '@/utils/opcodeCategories'

export type OpcodeSortMode = 'name' | 'count'

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
}

const PAGE_SIZE_OPTIONS = [50, 100, 200] as const
const DEFAULT_PAGE_SIZE = 100

function CopyIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
      />
    </svg>
  )
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
    </svg>
  )
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
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
    </svg>
  )
}

// Component for displaying EEST fixture info
export function EESTInfoContent({ test, opcodeSort, onOpcodeSortChange }: { test: SuiteTest; opcodeSort: OpcodeSortMode; onOpcodeSortChange: (sort: OpcodeSortMode) => void }) {
  const info = test.eest?.info
  if (!info) return null

  const fields = [
    { label: 'Description', value: info.description },
    { label: 'Comment', value: info.comment },
    { label: 'Fixture Format', value: info['fixture-format'] },
    { label: 'Filling Tool', value: info['filling-transition-tool'] },
    { label: 'Hash', value: info.hash },
    { label: 'URL', value: info.url },
  ].filter((f) => f.value)

  const hasOpcodes = info.opcode_count && Object.keys(info.opcode_count).length > 0

  if (fields.length === 0 && !hasOpcodes) return null

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <Badge variant="default">EEST Info</Badge>
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
              {Object.entries(info.opcode_count!)
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

// Component for displaying test steps content (for tests with setup/test/cleanup)
function TestStepsContent({ suiteHash, test, opcodeSort, onOpcodeSortChange }: { suiteHash: string; test: SuiteTest; opcodeSort: OpcodeSortMode; onOpcodeSortChange: (sort: OpcodeSortMode) => void }) {
  const steps = [
    { key: 'setup', label: 'Setup step', file: test.setup },
    { key: 'test', label: 'Test step', file: test.test },
    { key: 'cleanup', label: 'Cleanup step', file: test.cleanup },
  ].filter((s) => s.file) as { key: string; label: string; file: SuiteFile }[]

  const hasInfo = !!test.eest?.info
  if (steps.length === 0 && !hasInfo) {
    return <div className="p-4 text-sm/6 text-gray-500">No step files available</div>
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between">
          <span className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">Name</span>
          <CopyButton text={test.name} label="name" />
        </div>
        <div className="break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">{test.name}</div>
      </div>
      <EESTInfoContent test={test} opcodeSort={opcodeSort} onOpcodeSortChange={onOpcodeSortChange} />
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
}: TestFilesListProps) {
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const currentPage = controlledPage ?? 1
  const search = searchQuery ?? ''

  const handleOpcodeSortChange = (sort: OpcodeSortMode) => {
    onOpcodeSortChange?.(sort)
  }

  // For pre_run_steps, use files; for tests, use tests
  const isPreRunSteps = type === 'pre_run_steps'
  const hasGenesis = !isPreRunSteps && (tests ?? []).some((t) => !!t.genesis)
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
        .filter(({ test }) => {
          const searchLower = search.toLowerCase()
          return test.name.toLowerCase().includes(searchLower)
        })

  const totalPages = Math.ceil(filteredItems.length / pageSize)
  const paginatedItems = filteredItems.slice((currentPage - 1) * pageSize, currentPage * pageSize)

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
        <div className="relative flex-1">
          <SearchIcon className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-gray-400" />
          <input
            type="text"
            value={search}
            onChange={(e) => handleSearchChange(e.target.value)}
            placeholder="Search by test name..."
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
                Test Name
              </th>
              {hasGenesis && (
                <th className="w-40 px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Genesis
                </th>
              )}
              <th className="w-48 px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Steps
              </th>
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
                <td className="truncate px-4 py-2 font-mono text-xs/5 text-gray-900 dark:text-gray-100" title={test.name}>
                  {test.name}
                </td>
                {hasGenesis && (
                  <td className="truncate px-4 py-2 font-mono text-xs/5 text-gray-500 dark:text-gray-400" title={test.genesis}>
                    {test.genesis ?? '—'}
                  </td>
                )}
                <td className="px-4 py-2">
                  <div className="flex gap-1">
                    {test.setup && <Badge variant="default">Setup</Badge>}
                    {test.test && <Badge variant="default">Test</Badge>}
                    {test.cleanup && <Badge variant="default">Cleanup</Badge>}
                  </div>
                </td>
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
