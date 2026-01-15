import { Link } from '@tanstack/react-router'
import { useSuite } from '@/api/hooks/useSuite'
import { Badge } from '@/components/shared/Badge'
import { SourceBadge } from '@/components/shared/SourceBadge'
import { formatTimestamp, formatRelativeTime } from '@/utils/date'

interface SuiteEntry {
  hash: string
  runCount: number
  lastRun: number
}

interface SuitesTableProps {
  suites: SuiteEntry[]
}

function SuiteRow({ suite }: { suite: SuiteEntry }) {
  const { data: suiteInfo } = useSuite(suite.hash)

  return (
    <tr className="transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50">
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
        {suiteInfo?.source.tests ? (
          <div className="flex items-center gap-2">
            <SourceBadge source={suiteInfo.source.tests} />
            <Badge variant="default">{suiteInfo.tests.length}</Badge>
          </div>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        {suiteInfo?.source.warmup && suiteInfo.warmup ? (
          <div className="flex items-center gap-2">
            <SourceBadge source={suiteInfo.source.warmup} />
            <Badge variant="default">{suiteInfo.warmup.length}</Badge>
          </div>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        {suiteInfo?.filter ? (
          <span className="font-mono text-sm/6 text-gray-700 dark:text-gray-300">{suiteInfo.filter}</span>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="whitespace-nowrap px-6 py-4 text-sm/6 text-gray-500 dark:text-gray-400">
        <span title={formatRelativeTime(suite.lastRun)}>{formatTimestamp(suite.lastRun)}</span>
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        <Badge variant="info">{suite.runCount} runs</Badge>
      </td>
    </tr>
  )
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
              Tests Source
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Warmup Source
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Filter
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Last Run
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Runs
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {suites.map((suite) => (
            <SuiteRow key={suite.hash} suite={suite} />
          ))}
        </tbody>
      </table>
    </div>
  )
}
