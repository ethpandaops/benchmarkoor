import { useState, useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import type { PostTestRPCCallConfig } from '@/api/types'
import { fetchHead, type HeadResult } from '@/api/client'
import { formatBytes } from '@/utils/format'

interface PostTestDumpsPanelProps {
  runId: string
  testNames: string[]
  postTestRPCCalls: PostTestRPCCallConfig[]
}

interface DumpFileEntry {
  testName: string
  filename: string
  path: string
}

export function PostTestDumpsPanel({ runId, testNames, postTestRPCCalls }: PostTestDumpsPanelProps) {
  const [expanded, setExpanded] = useState(false)

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

  const isLoading = fileQueries.some((q) => q.isLoading)

  const { fileCount, totalSize, existingFiles } = useMemo(() => {
    let count = 0
    let size = 0
    const existing: { entry: DumpFileEntry; info: HeadResult }[] = []

    for (let i = 0; i < entries.length; i++) {
      const data = fileQueries[i]?.data
      if (data?.exists) {
        count++
        if (data.size != null) size += data.size
        existing.push({ entry: entries[i], info: data })
      }
    }

    return { fileCount: count, totalSize: size, existingFiles: existing }
  }, [entries, fileQueries])

  if (dumpCalls.length === 0) return null
  if (!isLoading && fileCount === 0) return null

  const summary = isLoading
    ? 'Loading...'
    : `${fileCount} file${fileCount !== 1 ? 's' : ''}, ${formatBytes(totalSize)} total`

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 text-left hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="shrink-0 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Post-Test RPC Dumps</h3>
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
        <div className="overflow-x-auto p-4">
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
        </div>
      )}
    </div>
  )
}
