import { useState, useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import type { PostTestRPCCallConfig } from '@/api/types'
import { fetchHead, type HeadResult } from '@/api/client'
import { formatBytes } from '@/utils/format'

interface FilesPanelProps {
  runId: string
  testNames: string[]
  postTestRPCCalls?: PostTestRPCCallConfig[]
}

interface DumpFileEntry {
  testName: string
  filename: string
  path: string
}

function PostTestDumpsTab({ runId, testNames, postTestRPCCalls }: { runId: string; testNames: string[]; postTestRPCCalls: PostTestRPCCallConfig[] }) {
  const dumpCalls = useMemo(
    () => postTestRPCCalls.filter((c) => c.dump?.enabled && c.dump.filename),
    [postTestRPCCalls],
  )

  const entries = useMemo<DumpFileEntry[]>(() => {
    const result: DumpFileEntry[] = []
    for (const testName of testNames) {
      for (const call of dumpCalls) {
        const filename = `${call.dump!.filename}.json`
        result.push({
          testName,
          filename,
          path: `runs/${runId}/${testName}/post_test_rpc_calls/${filename}`,
        })
      }
    }
    return result
  }, [runId, testNames, dumpCalls])

  const fileQueries = useQueries({
    queries: entries.map((entry) => ({
      queryKey: ['post-test-dump-panel', runId, entry.testName, entry.filename],
      queryFn: () => fetchHead(entry.path),
      staleTime: Infinity,
    })),
  })

  const existingFiles = useMemo(() => {
    const existing: { entry: DumpFileEntry; info: HeadResult }[] = []
    for (let i = 0; i < entries.length; i++) {
      const data = fileQueries[i]?.data
      if (data?.exists) {
        existing.push({ entry: entries[i], info: data })
      }
    }
    return existing
  }, [entries, fileQueries])

  if (existingFiles.length === 0) {
    return (
      <div className="py-4 text-center text-xs/5 text-gray-500 dark:text-gray-400">
        No dump files found
      </div>
    )
  }

  return (
    <table className="w-full text-left text-xs/5">
      <thead>
        <tr className="border-b border-gray-200 text-gray-500 dark:border-gray-700 dark:text-gray-400">
          <th className="pb-2 pr-3 font-medium">Path</th>
          <th className="pb-2 pr-3 text-right font-medium">Size</th>
          <th className="pb-2" />
        </tr>
      </thead>
      <tbody className="font-mono text-gray-900 dark:text-gray-100">
        {existingFiles.map(({ entry, info }) => (
          <tr key={entry.path} className="border-b border-gray-200 last:border-0 dark:border-gray-700">
            <td className="py-2 pr-3">
              <span className="text-gray-500 dark:text-gray-400">{entry.testName}/</span>
              post_test_rpc_calls/{entry.filename}
            </td>
            <td className="py-2 pr-3 text-right text-gray-500 dark:text-gray-400">
              {info.size != null ? formatBytes(info.size) : '-'}
            </td>
            <td className="py-2">
              <a
                href={info.url}
                download={entry.filename}
                className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                title="Download"
              >
                <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
                </svg>
              </a>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function usePostTestDumpStats(runId: string, testNames: string[], postTestRPCCalls: PostTestRPCCallConfig[]) {
  const dumpCalls = useMemo(
    () => postTestRPCCalls.filter((c) => c.dump?.enabled && c.dump.filename),
    [postTestRPCCalls],
  )

  const entries = useMemo(() => {
    const result: { testName: string; filename: string; path: string }[] = []
    for (const testName of testNames) {
      for (const call of dumpCalls) {
        const filename = `${call.dump!.filename}.json`
        result.push({ testName, filename, path: `runs/${runId}/${testName}/post_test_rpc_calls/${filename}` })
      }
    }
    return result
  }, [runId, testNames, dumpCalls])

  const fileQueries = useQueries({
    queries: entries.map((entry) => ({
      queryKey: ['post-test-dump-panel', runId, entry.testName, entry.filename],
      queryFn: () => fetchHead(entry.path),
      staleTime: Infinity,
    })),
  })

  return useMemo(() => {
    const isLoading = fileQueries.some((q) => q.isLoading)
    let fileCount = 0
    let totalSize = 0
    for (const q of fileQueries) {
      if (q.data?.exists) {
        fileCount++
        if (q.data.size != null) totalSize += q.data.size
      }
    }
    return { isLoading, fileCount, totalSize, hasDumpCalls: dumpCalls.length > 0 }
  }, [fileQueries, dumpCalls.length])
}

export function FilesPanel({ runId, testNames, postTestRPCCalls }: FilesPanelProps) {
  const [expanded, setExpanded] = useState(false)
  const [activeTab, setActiveTab] = useState('post-test-rpc-dumps')

  const hasPostTestDumps = postTestRPCCalls && postTestRPCCalls.length > 0
  const dumpStats = usePostTestDumpStats(runId, testNames, postTestRPCCalls ?? [])

  const tabs = useMemo(() => {
    const result: { key: string; label: string; badge?: string }[] = []
    if (hasPostTestDumps) {
      result.push({
        key: 'post-test-rpc-dumps',
        label: 'Post-Test RPC Dumps',
        badge: dumpStats.isLoading ? '...' : dumpStats.fileCount > 0 ? String(dumpStats.fileCount) : undefined,
      })
    }
    return result
  }, [hasPostTestDumps, dumpStats])

  if (tabs.length === 0) return null
  if (!dumpStats.isLoading && dumpStats.fileCount === 0 && !dumpStats.hasDumpCalls) return null
  if (!dumpStats.isLoading && dumpStats.fileCount === 0) return null

  const summary = dumpStats.isLoading
    ? 'Loading...'
    : `${dumpStats.fileCount} file${dumpStats.fileCount !== 1 ? 's' : ''}, ${formatBytes(dumpStats.totalSize)} total`

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
          {tabs.length > 1 && (
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
                  {tab.badge && (
                    <span className="rounded-full bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-300">
                      {tab.badge}
                    </span>
                  )}
                </button>
              ))}
            </div>
          )}
          {tabs.length === 1 && (
            <div className="border-b border-gray-200 px-4 py-2 dark:border-gray-700">
              <span className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                {tabs[0].label}
                {tabs[0].badge && (
                  <span className="ml-2 rounded-full bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-300">
                    {tabs[0].badge}
                  </span>
                )}
              </span>
            </div>
          )}
          <div className="overflow-x-auto p-4">
            {activeTab === 'post-test-rpc-dumps' && hasPostTestDumps && (
              <PostTestDumpsTab runId={runId} testNames={testNames} postTestRPCCalls={postTestRPCCalls} />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
