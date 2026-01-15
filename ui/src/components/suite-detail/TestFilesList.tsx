import { useState } from 'react'
import type { SuiteFile } from '@/api/types'
import { Card } from '@/components/shared/Card'
import { Pagination } from '@/components/shared/Pagination'
import { Badge } from '@/components/shared/Badge'

interface TestFilesListProps {
  title: string
  files: SuiteFile[]
  defaultCollapsed?: boolean
}

const PAGE_SIZE = 20

export function TestFilesList({ title, files, defaultCollapsed = false }: TestFilesListProps) {
  const [currentPage, setCurrentPage] = useState(1)

  const totalPages = Math.ceil(files.length / PAGE_SIZE)
  const paginatedFiles = files.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  return (
    <Card title={`${title} (${files.length})`} collapsible defaultCollapsed={defaultCollapsed}>
      <div className="flex flex-col gap-4">
        <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
          <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead className="bg-gray-50 dark:bg-gray-900">
              <tr>
                <th className="px-4 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Filename
                </th>
                <th className="px-4 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Directory
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
              {paginatedFiles.map((file, index) => (
                <tr key={`${file.f}-${index}`}>
                  <td className="max-w-md truncate px-4 py-2 font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                    <span title={file.f}>{file.f}</span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2">
                    {file.d ? (
                      <Badge variant="default">{file.d}</Badge>
                    ) : (
                      <span className="text-sm/6 text-gray-400">-</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {totalPages > 1 && (
          <div className="flex justify-center">
            <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setCurrentPage} />
          </div>
        )}
      </div>
    </Card>
  )
}
