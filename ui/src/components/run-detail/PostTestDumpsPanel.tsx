import { useState, useMemo, useEffect, useCallback } from 'react'
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
  onDownloadFormatChange: (format: DownloadListFormat) => void
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
}: {
  entries: FileEntry[]
  queryKeyPrefix: string
  emptyMessage: string
}) {
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  const sortedEntries = useMemo(() => {
    const sorted = [...entries]
    sorted.sort((a, b) => {
      const pathA = a.testName ? `${a.testName}/${a.displayPath}` : a.displayPath
      const pathB = b.testName ? `${b.testName}/${b.displayPath}` : b.displayPath
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

  if (entries.length === 0) {
    return (
      <div className="py-4 text-center text-xs/5 text-gray-500 dark:text-gray-400">
        {emptyMessage}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
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
                  {entry.testName && <span className="text-gray-500 dark:text-gray-400">{entry.testName}/</span>}
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

const GENERAL_FILES = ['benchmarkoor.log', 'container.log', 'config.json', 'result.json'] as const

function buildGeneralEntries(runId: string): FileEntry[] {
  return GENERAL_FILES.map((filename) => ({
    testName: '',
    filename,
    path: `runs/${runId}/${filename}`,
    displayPath: filename,
    outputPath: `${runId}/${filename}`,
  }))
}

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

// --- Category entry map keyed by tab key ---

interface CategoryInfo {
  key: string
  label: string
  entries: FileEntry[]
}

// --- FilesPanel ---

export function FilesPanel({ runId, tests, postTestRPCCalls, showDownloadList, downloadFormat, onShowDownloadListChange, onDownloadFormatChange }: FilesPanelProps) {
  const [expanded, setExpanded] = useState(showDownloadList)
  const [activeTab, setActiveTab] = useState('general')
  const [downloadCategories, setDownloadCategories] = useState<Set<string>>(() => new Set())

  const testNames = useMemo(() => Object.keys(tests), [tests])

  const generalEntries = useMemo(() => buildGeneralEntries(runId), [runId])
  const testStatsEntries = useMemo(() => buildTestStatsEntries(runId, tests), [runId, tests])
  const testResponsesEntries = useMemo(() => buildTestResponsesEntries(runId, tests), [runId, tests])
  const postTestDumpEntries = useMemo(
    () => buildPostTestDumpEntries(runId, testNames, postTestRPCCalls ?? []),
    [runId, testNames, postTestRPCCalls],
  )

  const hasPostTestDumps = postTestDumpEntries.length > 0

  const categories: CategoryInfo[] = useMemo(() => {
    const result: CategoryInfo[] = [
      { key: 'general', label: 'General', entries: generalEntries },
      { key: 'test-stats', label: 'Stats', entries: testStatsEntries },
      { key: 'test-responses', label: 'Responses', entries: testResponsesEntries },
    ]
    if (hasPostTestDumps) {
      result.push({ key: 'post-test-rpc-dumps', label: 'Post-Test RPC Dumps', entries: postTestDumpEntries })
    }
    return result
  }, [generalEntries, testStatsEntries, testResponsesEntries, postTestDumpEntries, hasPostTestDumps])

  const tabs = useMemo(
    () => categories.map((c) => ({ key: c.key, label: c.label, badge: String(c.entries.length) })),
    [categories],
  )

  // Auto-select all categories (including newly appearing ones like post-test dumps)
  useEffect(() => {
    const allKeys = categories.map((c) => c.key)
    setDownloadCategories((prev) => {
      const next = new Set(prev)
      let changed = false
      for (const key of allKeys) {
        if (!next.has(key)) {
          next.add(key)
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [categories])

  const toggleCategory = useCallback((key: string) => {
    setDownloadCategories((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }, [])

  const downloadEntries = useMemo(() => {
    const entries: FileEntry[] = []
    for (const cat of categories) {
      if (downloadCategories.has(cat.key)) {
        entries.push(...cat.entries)
      }
    }
    return entries
  }, [categories, downloadCategories])

  const [downloadListText, setDownloadListText] = useState('')
  useEffect(() => {
    if (downloadEntries.length === 0) {
      setDownloadListText('')
      return
    }
    loadRuntimeConfig().then((cfg) => {
      const lines = downloadEntries.map((e) => {
        const url = getDataUrl(e.path, cfg)
        return downloadFormat === 'urls'
          ? toAbsoluteUrl(url)
          : `curl -fsSL --create-dirs -o '${e.outputPath}' '${toAbsoluteUrl(url)}'`
      })
      setDownloadListText(lines.join('\n'))
    })
  }, [downloadEntries, downloadFormat])

  const handleDownloadFile = useCallback(() => {
    if (!downloadListText) return
    const isCurl = downloadFormat === 'curl'
    const content = isCurl ? `#!/bin/sh\n${downloadListText}` : downloadListText
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = isCurl ? `${runId}.sh` : `${runId}.txt`
    a.click()
    URL.revokeObjectURL(url)
  }, [downloadListText, downloadFormat, runId])

  const totalEntries = generalEntries.length + testStatsEntries.length + testResponsesEntries.length + postTestDumpEntries.length
  const summary = `${totalEntries} file${totalEntries !== 1 ? 's' : ''}`

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <div className="flex items-center gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <h3 className="shrink-0 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Files</h3>
        <div className="flex min-w-0 items-center gap-3 ml-auto">
          <span className="truncate text-xs/5 text-gray-500 dark:text-gray-400">{summary}</span>
          <button
            onClick={(e) => { e.stopPropagation(); onShowDownloadListChange(true) }}
            className="flex shrink-0 items-center gap-1.5 rounded-xs border border-gray-300 px-2 py-1 text-xs/5 font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
          >
            <svg className="size-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 10h16M4 14h16M4 18h16" />
            </svg>
            Download list
          </button>
          <button
            onClick={() => setExpanded(!expanded)}
            className="shrink-0 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"
          >
            <svg
              className={clsx('size-5 transition-transform', expanded && 'rotate-180')}
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          </button>
        </div>
      </div>
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
            {activeTab === 'general' && (
              <FileListTab
                entries={generalEntries}
                queryKeyPrefix="file-panel"
                emptyMessage="No general files found"
              />
            )}
            {activeTab === 'test-stats' && (
              <FileListTab
                entries={testStatsEntries}
                queryKeyPrefix="file-panel"
                emptyMessage="No test stats files found"
              />
            )}
            {activeTab === 'test-responses' && (
              <FileListTab
                entries={testResponsesEntries}
                queryKeyPrefix="file-panel"
                emptyMessage="No test response files found"
              />
            )}
            {activeTab === 'post-test-rpc-dumps' && hasPostTestDumps && (
              <FileListTab
                entries={postTestDumpEntries}
                queryKeyPrefix="file-panel"
                emptyMessage="No dump files found"
              />
            )}
          </div>
        </div>
      )}
      <Modal isOpen={showDownloadList} onClose={() => onShowDownloadListChange(false)} title="Download List" className="max-w-3xl">
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-1 rounded-xs bg-gray-100 p-0.5 dark:bg-gray-700">
                {([{ key: 'curl', label: 'curl' }, { key: 'urls', label: 'Plain URLs' }] as const).map(({ key, label }) => (
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
                {downloadEntries.length} file{downloadEntries.length !== 1 ? 's' : ''}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <CopyButton text={downloadListText} />
              <button
                onClick={handleDownloadFile}
                disabled={!downloadListText}
                className="shrink-0 text-gray-400 hover:text-gray-600 disabled:opacity-50 dark:hover:text-gray-200"
                title="Download as file"
              >
                <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
                </svg>
              </button>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Include:</span>
            {categories.map((cat) => (
              <button
                key={cat.key}
                onClick={() => toggleCategory(cat.key)}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  downloadCategories.has(cat.key)
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'bg-gray-100 text-gray-600 hover:text-gray-900 dark:bg-gray-700 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                {cat.label} ({cat.entries.length})
              </button>
            ))}
          </div>
          <pre className="max-h-96 overflow-auto rounded-xs bg-gray-100 p-3 font-mono text-xs/5 text-gray-900 select-all dark:bg-gray-900 dark:text-gray-100">
            {downloadListText || 'No files selected'}
          </pre>
          {downloadFormat === 'curl' && downloadListText && (
            <p className="text-xs/5 text-gray-500 dark:text-gray-400">
              Download{' '}
              <button onClick={handleDownloadFile} className="text-blue-600 underline hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300">
                {runId}.sh
              </button>
              . Run with: <code className="rounded-xs bg-gray-100 px-1 py-0.5 font-mono dark:bg-gray-900">chmod +x {runId}.sh && ./{runId}.sh</code>{' '}<CopyButton text={`chmod +x ${runId}.sh && ./${runId}.sh`} />
            </p>
          )}
        </div>
      </Modal>
    </div>
  )
}
