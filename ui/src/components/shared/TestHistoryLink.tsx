import { Link } from '@tanstack/react-router'
import { History } from 'lucide-react'

interface TestHistoryLinkProps {
  suiteHash: string
  testName: string
}

// TestHistoryLink opens the history of a test across the runs of its suite.
export function TestHistoryLink({ suiteHash, testName }: TestHistoryLinkProps) {
  return (
    <Link
      to="/suites/$suiteHash/history"
      params={{ suiteHash }}
      search={{ test: testName }}
      className="flex items-center gap-1 text-xs/5 font-medium text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
      title="Show this test across the runs of the suite"
    >
      <History className="size-4" />
      History
    </Link>
  )
}
