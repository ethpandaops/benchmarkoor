import { useState, useMemo, useEffect } from 'react'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import type { PostTestRPCCallConfig, TestEntry } from '@/api/types'
import { fetchHead } from '@/api/client'
import { formatBytes } from '@/utils/format'
import { getDataUrl, loadRuntimeConfig, toAbsoluteUrl } from '@/config/runtime'
import { Modal } from '@/components/shared/Modal'
import { Pagination } from '@/components/shared/Pagination'

type DownloadListFormat = 'urls' | 'curl'
type SortDirection = 'asc' | 'desc'

interface FilesPanelProps {
  runId: string
  tests: Record<string, TestEntry>
  postTestRPCCalls?: PostTestRPCCallConfig[]
  showDownloadList: boolean
  downloadFormat: DownloadListFormat
  onShowDownloadListChange: (open: boolean) => void
  onDownloadFormatChange: (format: string) => void
}

interface FileEntry {
  testName: string
  filename: string
  path: string
  displayPath: string
  outputPath: string
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? (
        <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      ) : (
        <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
          />
        </svg>
      )}
    </button>
  )
}

const PAGE_SIZE_OPTIONS = [20, 50, 100] as const

function SortIcon({ direction, active }: { direction: SortDirection; active: boolean }) {
  return (
    <svg
      className={clsx('ml-1 inline-block size-3', active ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400')}
      viewBox="0 0 12 12"
      fill="currentColor"
    >
      {direction === 'asc' ? <path d="M6 2L10 8H2L6 2Z" /> : <path d="M6 10L2 4H10L6 10Z" />}
    </svg>
  )
}

// --- Generic FileListTab component ---
// Sorts and paginates entries statically, then only fires HEAD requests
// for the visible page to resolve sizes and availability.

function FileListTab({
  entries,
  queryKeyPrefix,
  emptyMessage,
  showDownloadList,
  downloadFormat,
  onShowDownloadListChange,
  onDownloadFormatChange,
}: {
  entries: FileEntry[]
  queryKeyPrefix: string
  emptyMessage: string
  showDownloadList: boolean
  downloadFormat: DownloadListFormat
  onShowDownloadListChange: (open: boolean) => void
  onDownloadFormatChange: (format: string) => void
}) {
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  const sortedEntries = useMemo(() => {
    const sorted = [...entries]
    sorted.sort((a, b) => {
      const pathA = `${a.testName}/${a.displayPath}`
      const pathB = `${b.testName}/${b.displayPath}`
      const cmp = pathA.localeCompare(pathB)
      return sortDir === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [entries, sortDir])

  const totalPages = Math.ceil(sortedEntries.length / pageSize)
  const pageEntries = sortedEntries.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  // Only HEAD the entries visible on the current page
  const pageQueries = useQueries({
    queries: pageEntries.map((entry) => ({
      queryKey: [queryKeyPrefix, entry.path],
      queryFn: () => fetchHead(entry.path),
      staleTime: Infinity,
    })),
  })

  const [downloadListText, setDownloadListText] = useState('')
  useEffect(() => {
    loadRuntimeConfig().then((cfg) => {
      const lines = entries.map((e) => {
        const url = getDataUrl(e.path, cfg)
        return downloadFormat === 'urls'
          ? toAbsoluteUrl(url)
          : `curl -fsSL --create-dirs -o '${e.outputPath}' '${toAbsoluteUrl(url)}'`
      })
      setDownloadListText(lines.join('\n'))
    })
  }, [entries, downloadFormat])

  if (entries.length === 0) {
    return (
      <div className="py-4 text-center text-xs/5 text-gray-500 dark:text-gray-400">
        {emptyMessage}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <button
          onClick={() => onShowDownloadListChange(true)}
          className="ml-auto flex items-center gap-1.5 rounded-xs border border-gray-300 px-2 py-1 text-xs/5 font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
        >
          <svg className="size-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 10h16M4 14h16M4 18h16" />
          </svg>
          Generate download list
        </button>
      </div>
      <Modal isOpen={showDownloadList} onClose={() => onShowDownloadListChange(false)} title="Download List" className="max-w-3xl">
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-1 rounded-xs bg-gray-100 p-0.5 dark:bg-gray-700">
                {([{ key: 'urls', label: 'Plain URLs' }, { key: 'curl', label: 'curl' }] as const).map(({ key, label }) => (
                  <button
                    key={key}
                    onClick={() => onDownloadFormatChange(key)}
                    className={clsx(
                      'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                      downloadFormat === key
                        ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                        : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <span className="text-xs/5 text-gray-500 dark:text-gray-400">
                {entries.length} file{entries.length !== 1 ? 's' : ''}
              </span>
            </div>
            <CopyButton text={downloadListText} />
          </div>
          <pre className="max-h-96 overflow-auto rounded-xs bg-gray-100 p-3 font-mono text-xs/5 text-gray-900 select-all dark:bg-gray-900 dark:text-gray-100">
            {downloadListText}
          </pre>
        </div>
      </Modal>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
          <select
            value={pageSize}
            onChange={(e) => {
              setPageSize(Number(e.target.value))
              setCurrentPage(1)
            }}
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
        {totalPages > 1 && <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setCurrentPage} />}
      </div>
      <table className="w-full text-left text-xs/5">
        <thead>
          <tr className="border-b border-gray-200 dark:border-gray-700">
            <th
              onClick={() => setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))}
              className="cursor-pointer select-none pb-2 pr-3 font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300"
            >
              Path
              <SortIcon direction={sortDir} active />
            </th>
            <th className="pb-2 pr-3 text-right font-medium text-gray-500 dark:text-gray-400">Size</th>
            <th className="pb-2" />
          </tr>
        </thead>
        <tbody className="font-mono text-gray-900 dark:text-gray-100">
          {pageEntries.map((entry, i) => {
            const headResult = pageQueries[i]?.data
            const isAvailable = headResult?.exists ?? false
            const isChecked = !!headResult

            return (
              <tr
                key={entry.path}
                className={clsx(
                  'border-b border-gray-200 last:border-0 dark:border-gray-700',
                  isChecked && !isAvailable && 'opacity-50',
                )}
              >
                <td className="py-2 pr-3">
                  <span className="text-gray-500 dark:text-gray-400">{entry.testName}/</span>
                  {entry.displayPath}
                  {isChecked && !isAvailable && (
                    <span className="ml-2 rounded-full bg-yellow-100 px-1.5 py-0.5 font-sans text-xs font-medium text-yellow-700 dark:bg-yellow-900/50 dark:text-yellow-300">
                      Unavailable
                    </span>
                  )}
                </td>
                <td className="py-2 pr-3 text-right text-gray-500 dark:text-gray-400">
                  {!isChecked ? (
                    <span className="inline-block size-3 animate-pulse rounded-full bg-gray-200 dark:bg-gray-600" />
                  ) : isAvailable && headResult.size != null ? (
                    formatBytes(headResult.size)
                  ) : (
                    '-'
                  )}
                </td>
                <td className="py-2">
                  {isAvailable && headResult ? (
                    <a
                      href={headResult.url}
                      download={entry.filename}
                      className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                      title="Download"
                    >
                      <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
                      </svg>
                    </a>
                  ) : null}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      {totalPages > 1 && (
        <div className="flex justify-end">
          <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setCurrentPage} />
        </div>
      )}
    </div>
  )
}

// --- Entry generators ---

const ALL_STEPS = ['setup', 'test', 'cleanup'] as const
type StepName = typeof ALL_STEPS[number]

function getTestSteps(entry: TestEntry): StepName[] {
  if (!entry.steps) return []
  return ALL_STEPS.filter((step) => entry.steps![step] != null)
}

function buildTestStatsEntries(runId: string, tests: Record<string, TestEntry>): FileEntry[] {
  const entries: FileEntry[] = []
  for (const [testName, testEntry] of Object.entries(tests)) {
    for (const step of getTestSteps(testEntry)) {
      for (const suffix of ['result-aggregated.json', 'result-details.json']) {
        const filename = `${step}.${suffix}`
        entries.push({
          testName,
          filename,
          path: `runs/${runId}/${testName}/${filename}`,
          displayPath: filename,
          outputPath: `${runId}/${testName}/${filename}`,
        })
      }
    }
  }
  return entries
}

function buildTestResponsesEntries(runId: string, tests: Record<string, TestEntry>): FileEntry[] {
  const entries: FileEntry[] = []
  for (const [testName, testEntry] of Object.entries(tests)) {
    for (const step of getTestSteps(testEntry)) {
      const filename = `${step}.response`
      entries.push({
        testName,
        filename,
        path: `runs/${runId}/${testName}/${filename}`,
        displayPath: filename,
        outputPath: `${runId}/${testName}/${filename}`,
      })
    }
  }
  return entries
}

function buildPostTestDumpEntries(runId: string, testNames: string[], postTestRPCCalls: PostTestRPCCallConfig[]): FileEntry[] {
  const dumpCalls = postTestRPCCalls.filter((c) => c.dump?.enabled && c.dump.filename)
  const entries: FileEntry[] = []
  for (const testName of testNames) {
    for (const call of dumpCalls) {
      const filename = `${call.dump!.filename}.json`
      entries.push({
        testName,
        filename,
        path: `runs/${runId}/${testName}/post_test_rpc_calls/${filename}`,
        displayPath: `post_test_rpc_calls/${filename}`,
        outputPath: `${runId}/${testName}/post_test_rpc_calls/${filename}`,
      })
    }
  }
  return entries
}

// --- FilesPanel ---

export function FilesPanel({ runId, tests, postTestRPCCalls, showDownloadList, downloadFormat, onShowDownloadListChange, onDownloadFormatChange }: FilesPanelProps) {
  const [expanded, setExpanded] = useState(showDownloadList)
  const [activeTab, setActiveTab] = useState('test-stats')

  const testNames = useMemo(() => Object.keys(tests), [tests])

  const testStatsEntries = useMemo(() => buildTestStatsEntries(runId, tests), [runId, tests])
  const testResponsesEntries = useMemo(() => buildTestResponsesEntries(runId, tests), [runId, tests])
  const postTestDumpEntries = useMemo(
    () => buildPostTestDumpEntries(runId, testNames, postTestRPCCalls ?? []),
    [runId, testNames, postTestRPCCalls],
  )

  const hasPostTestDumps = postTestDumpEntries.length > 0

  const tabs = useMemo(() => {
    const result: { key: string; label: string; badge: string }[] = []
    result.push({ key: 'test-stats', label: 'Stats', badge: String(testStatsEntries.length) })
    result.push({ key: 'test-responses', label: 'Responses', badge: String(testResponsesEntries.length) })
    if (hasPostTestDumps) {
      result.push({ key: 'post-test-rpc-dumps', label: 'Post-Test RPC Dumps', badge: String(postTestDumpEntries.length) })
    }
    return result
  }, [testStatsEntries.length, testResponsesEntries.length, postTestDumpEntries.length, hasPostTestDumps])

  const totalEntries = testStatsEntries.length + testResponsesEntries.length + postTestDumpEntries.length
  const summary = `${totalEntries} file${totalEntries !== 1 ? 's' : ''}`

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 text-left hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="shrink-0 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Files</h3>
        <div className="flex min-w-0 items-center gap-3">
          <span className="truncate text-xs/5 text-gray-500 dark:text-gray-400">{summary}</span>
          <svg
            className={clsx('size-5 shrink-0 text-gray-500 transition-transform', expanded && 'rotate-180')}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        </div>
      </button>
      {expanded && (
        <div>
          <div className="flex gap-1 border-b border-gray-200 px-4 dark:border-gray-700">
            {tabs.map((tab) => (
              <button
                key={tab.key}
                onClick={() => setActiveTab(tab.key)}
                className={clsx(
                  'flex items-center gap-2 border-b-2 px-4 py-2 text-sm/6 font-medium transition-colors',
                  activeTab === tab.key
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300',
                )}
              >
                {tab.label}
                <span className="rounded-full bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-300">
                  {tab.badge}
                </span>
              </button>
            ))}
          </div>
          <div className="overflow-x-auto p-4">
            {activeTab === 'test-stats' && (
              <FileListTab
                entries={testStatsEntries}
                queryKeyPrefix="file-panel"
                emptyMessage="No test stats files found"
                showDownloadList={showDownloadList}
                downloadFormat={downloadFormat}
                onShowDownloadListChange={onShowDownloadListChange}
                onDownloadFormatChange={onDownloadFormatChange}
              />
            )}
            {activeTab === 'test-responses' && (
              <FileListTab
                entries={testResponsesEntries}
                queryKeyPrefix="file-panel"
                emptyMessage="No test response files found"
                showDownloadList={showDownloadList}
                downloadFormat={downloadFormat}
                onShowDownloadListChange={onShowDownloadListChange}
                onDownloadFormatChange={onDownloadFormatChange}
              />
            )}
            {activeTab === 'post-test-rpc-dumps' && hasPostTestDumps && (
              <FileListTab
                entries={postTestDumpEntries}
                queryKeyPrefix="file-panel"
                emptyMessage="No dump files found"
                showDownloadList={showDownloadList}
                downloadFormat={downloadFormat}
                onShowDownloadListChange={onShowDownloadListChange}
                onDownloadFormatChange={onDownloadFormatChange}
              />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
