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
  expandedIndex?: number
  onExpandedChange?: (index: number | undefined) => void
  currentPage?: number
  onPageChange?: (page: number) => void
  searchQuery?: string
  onSearchChange?: (query: string | undefined) => void
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
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between">
          <span className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
            Filename
          </span>
          <CopyButton text={file.f} label="filename" />
        </div>
        <div className="break-all font-mono text-sm/6 text-gray-700 dark:text-gray-300">{file.f}</div>
      </div>
      {file.d && (
        <div className="flex flex-col gap-1">
          <div className="flex items-center justify-between">
            <span className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Directory
            </span>
            <CopyButton text={file.d} label="directory" />
          </div>
          <div className="break-all font-mono text-sm/6 text-gray-700 dark:text-gray-300">{file.d}</div>
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

export function TestFilesList({
  files,
  suiteHash,
  type,
  expandedIndex,
  onExpandedChange,
  currentPage: controlledPage,
  onPageChange,
  searchQuery,
  onSearchChange,
}: TestFilesListProps) {
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const currentPage = controlledPage ?? 1
  const search = searchQuery ?? ''

  // Add original index to each file for stable numbering
  const indexedFiles = files.map((file, index) => ({ file, originalIndex: index + 1 }))
  const filteredFiles = indexedFiles.filter(({ file }) => {
    const searchLower = search.toLowerCase()
    return file.f.toLowerCase().includes(searchLower) || (file.d?.toLowerCase().includes(searchLower) ?? false)
  })

  const totalPages = Math.ceil(filteredFiles.length / pageSize)
  const paginatedFiles = filteredFiles.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  const setCurrentPage = (page: number) => {
    if (onPageChange) {
      onPageChange(page)
    }
  }

  const toggleExpand = (index: number) => {
    if (onExpandedChange) {
      onExpandedChange(expandedIndex === index ? undefined : index)
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

  const PaginationControls = () => (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <span className="text-sm/6 text-gray-500 dark:text-gray-400">
          {search ? `${filteredFiles.length} of ${files.length}` : files.length} files
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

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-4">
        <div className="relative flex-1">
          <SearchIcon className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-gray-400" />
          <input
            type="text"
            value={search}
            onChange={(e) => handleSearchChange(e.target.value)}
            placeholder="Search by filename or directory..."
            className="w-full rounded-sm border border-gray-300 bg-white py-2 pl-10 pr-4 text-sm/6 text-gray-900 placeholder:text-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 dark:placeholder:text-gray-500"
          />
        </div>
      </div>
      {filteredFiles.length > 0 && <PaginationControls />}
      <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <table className="w-full table-fixed divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <th className="w-10 px-2 py-3"></th>
              <th className="w-16 px-2 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                #
              </th>
              <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Filename
              </th>
              <th className="w-48 px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Directory
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedFiles.map(({ file, originalIndex }) => {
              const isExpanded = expandedIndex === originalIndex

              return (
                <>
                  <tr
                    key={originalIndex}
                    onClick={() => toggleExpand(originalIndex)}
                    className={clsx(
                      'cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50',
                      isExpanded && 'bg-blue-50 dark:bg-blue-900/20',
                    )}
                  >
                    <td className="px-2 py-2">
                      <svg
                        className={clsx(
                          'size-3.5 text-gray-400 transition-transform',
                          isExpanded && 'rotate-90',
                        )}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                    </td>
                    <td className="px-2 py-2 text-right font-mono text-xs/5 text-gray-500 dark:text-gray-400">
                      {originalIndex}
                    </td>
                    <td className="truncate px-6 py-2 font-mono text-xs/5 text-gray-900 dark:text-gray-100" title={file.f}>
                      {file.f}
                    </td>
                    <td className="truncate px-4 py-2 font-mono text-xs/5 text-gray-500 dark:text-gray-400" title={file.d}>
                      {file.d || '-'}
                    </td>
                  </tr>
                  {isExpanded && (
                    <tr key={`${originalIndex}-content`}>
                      <td colSpan={4} className="max-w-0 bg-gray-50 px-4 py-4 dark:bg-gray-900/50">
                        <FileContent suiteHash={suiteHash} type={type} file={file} />
                      </td>
                    </tr>
                  )}
                </>
              )
            })}
          </tbody>
        </table>
        {filteredFiles.length === 0 && (
          <div className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
            {search ? `No files matching "${search}"` : 'No files found'}
          </div>
        )}
      </div>

      {filteredFiles.length > 0 && <PaginationControls />}
    </div>
  )
}
