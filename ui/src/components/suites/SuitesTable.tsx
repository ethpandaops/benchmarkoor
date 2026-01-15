import { Link } from '@tanstack/react-router'
import { Badge } from '@/components/shared/Badge'

interface SuiteEntry {
  hash: string
  runCount: number
}

interface SuitesTableProps {
  suites: SuiteEntry[]
}

export function SuitesTable({ suites }: SuitesTableProps) {
  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Suite Hash
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Runs
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {suites.map((suite) => (
            <tr key={suite.hash} className="transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50">
              <td className="whitespace-nowrap px-6 py-4">
                <Link
                  to="/suites/$suiteHash"
                  params={{ suiteHash: suite.hash }}
                  className="font-mono text-sm/6 font-medium text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  {suite.hash}
                </Link>
              </td>
              <td className="whitespace-nowrap px-6 py-4">
                <Badge variant="info">{suite.runCount} runs</Badge>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
