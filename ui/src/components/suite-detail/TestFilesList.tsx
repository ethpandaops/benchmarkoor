import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import clsx from 'clsx'
import type { SuiteFile } from '@/api/types'
import { fetchText } from '@/api/client'
import { Pagination } from '@/components/shared/Pagination'
import { Spinner } from '@/components/shared/Spinner'

interface TestFilesListProps {
  files: SuiteFile[]
  suiteHash: string
  type: 'tests' | 'warmup'
}

const PAGE_SIZE_OPTIONS = [50, 100, 200] as const
const DEFAULT_PAGE_SIZE = 100

function FileContent({ suiteHash, type, file }: { suiteHash: string; type: 'tests' | 'warmup'; file: SuiteFile }) {
  const path = file.d
    ? `suites/${suiteHash}/${type}/${file.d}/${file.f}`
    : `suites/${suiteHash}/${type}/${file.f}`

  const { data, isLoading, error } = useQuery({
    queryKey: ['suite', suiteHash, type, file.d, file.f],
    queryFn: () => fetchText(path),
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
    <pre className="max-h-96 overflow-auto rounded-sm bg-gray-900 p-4 font-mono text-xs/5 text-gray-100">
      {data}
    </pre>
  )
}

export function TestFilesList({ files, suiteHash, type }: TestFilesListProps) {
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [expandedFile, setExpandedFile] = useState<string | null>(null)

  const totalPages = Math.ceil(files.length / pageSize)
  const paginatedFiles = files.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  const toggleExpand = (fileKey: string) => {
    setExpandedFile(expandedFile === fileKey ? null : fileKey)
  }

  const handlePageSizeChange = (newSize: number) => {
    setPageSize(newSize)
    setCurrentPage(1)
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <th className="w-8 px-2 py-3"></th>
              <th className="w-12 px-2 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                #
              </th>
              <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Filename
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedFiles.map((file, index) => {
              const fileKey = `${file.d ?? ''}-${file.f}-${index}`
              const isExpanded = expandedFile === fileKey

              return (
                <>
                  <tr
                    key={fileKey}
                    onClick={() => toggleExpand(fileKey)}
                    className={clsx(
                      'cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50',
                      isExpanded && 'bg-blue-50 dark:bg-blue-900/20',
                    )}
                  >
                    <td className="px-2 py-4">
                      <svg
                        className={clsx(
                          'size-4 text-gray-400 transition-transform',
                          isExpanded && 'rotate-90',
                        )}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                    </td>
                    <td className="px-2 py-4 text-right font-mono text-sm/6 text-gray-500 dark:text-gray-400">
                      {(currentPage - 1) * pageSize + index + 1}
                    </td>
                    <td className="max-w-md truncate px-6 py-4 font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                      <span title={file.f}>{file.f}</span>
                    </td>
                  </tr>
                  {isExpanded && (
                    <tr key={`${fileKey}-content`}>
                      <td colSpan={3} className="bg-gray-50 px-4 py-4 dark:bg-gray-900/50">
                        <FileContent suiteHash={suiteHash} type={type} file={file} />
                      </td>
                    </tr>
                  )}
                </>
              )
            })}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
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
    </div>
  )
}
