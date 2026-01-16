import { useState, useCallback, useEffect } from 'react'
import clsx from 'clsx'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useTestRequests, useTestResponses, useTestTimes } from '@/api/hooks/useTestDetails'
import { Duration } from '@/components/shared/Duration'

function useDarkMode() {
  const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains('dark'))

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  return isDark
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [text])

  return (
    <button
      onClick={handleCopy}
      className="rounded-xs px-2 py-1 text-xs/5 font-medium text-gray-500 transition-colors hover:bg-gray-200 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
    >
      {copied ? 'Copied!' : 'Copy'}
    </button>
  )
}

function JsonBlock({ code }: { code: string }) {
  const isDark = useDarkMode()

  return (
    <SyntaxHighlighter
      language="json"
      style={isDark ? oneDark : oneLight}
      customStyle={{
        margin: 0,
        padding: '0.75rem',
        fontSize: '0.75rem',
        lineHeight: '1.25rem',
        background: 'transparent',
      }}
      wrapLongLines={false}
    >
      {code}
    </SyntaxHighlighter>
  )
}

interface ExecutionsListProps {
  runId: string
  suiteHash: string
  testName: string
  dir?: string
}

function parseMethod(request: string): string {
  try {
    const parsed = JSON.parse(request)
    return parsed.method || 'unknown'
  } catch {
    return 'unknown'
  }
}

function formatJson(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 2)
  } catch {
    return json
  }
}

interface ExecutionRowProps {
  index: number
  request: string
  response?: string
  time?: number
}

function ExecutionRow({ index, request, response, time }: ExecutionRowProps) {
  const [expanded, setExpanded] = useState(false)
  const method = parseMethod(request)

  return (
    <div className="max-w-full overflow-hidden border-b border-gray-200 last:border-b-0 dark:border-gray-700">
      <button
        onClick={() => setExpanded(!expanded)}
        className={clsx(
          'flex w-full items-center gap-3 px-3 py-2 text-left transition-colors hover:bg-gray-100 dark:hover:bg-gray-800',
          expanded && 'bg-gray-100 dark:bg-gray-800',
        )}
      >
        <svg
          className={clsx('size-4 shrink-0 text-gray-400 transition-transform', expanded && 'rotate-90')}
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span className="w-10 shrink-0 font-mono text-sm/6 text-gray-500 dark:text-gray-400">#{index}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-sm/6 text-gray-900 dark:text-gray-100">{method}</span>
        {time !== undefined && (
          <span className="shrink-0 text-sm/6 text-gray-500 dark:text-gray-400">
            <Duration nanoseconds={time} />
          </span>
        )}
      </button>

      {expanded && (
        <div className="bg-gray-50 px-4 py-3 dark:bg-gray-900/50">
          <div className="flex flex-col gap-3">
            <div>
              <div className="mb-1 flex items-center justify-between">
                <h5 className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Request
                </h5>
                <CopyButton text={formatJson(request)} />
              </div>
              <div className="w-0 min-w-full overflow-x-auto rounded-xs bg-gray-100 dark:bg-gray-800">
                <JsonBlock code={formatJson(request)} />
              </div>
            </div>
            {response && (
              <div>
                <div className="mb-1 flex items-center justify-between">
                  <h5 className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Response
                  </h5>
                  <CopyButton text={formatJson(response)} />
                </div>
                <div className="w-0 min-w-full overflow-x-auto rounded-xs bg-gray-100 dark:bg-gray-800">
                  <JsonBlock code={formatJson(response)} />
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export function ExecutionsList({ runId, suiteHash, testName, dir }: ExecutionsListProps) {
  const { data: requests, isLoading: requestsLoading, error: requestsError } = useTestRequests(suiteHash, testName, dir)
  const { data: responses, isLoading: responsesLoading } = useTestResponses(runId, testName, dir)
  const { data: times, isLoading: timesLoading } = useTestTimes(runId, testName, dir)

  const isLoading = requestsLoading || responsesLoading || timesLoading

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-4">
        <div className="size-5 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600" />
        <span className="ml-2 text-sm/6 text-gray-500 dark:text-gray-400">Loading executions...</span>
      </div>
    )
  }

  if (requestsError || !requests) {
    return <p className="py-2 text-sm/6 text-gray-500 dark:text-gray-400">No execution data available</p>
  }

  return (
    <div className="mt-4 max-w-full overflow-hidden">
      <h4 className="mb-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
        Executions ({requests.length})
      </h4>
      <div className="overflow-hidden rounded-xs border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
        {requests.map((request, index) => (
          <ExecutionRow
            key={index}
            index={index}
            request={request}
            response={responses?.[index]}
            time={times?.[index]}
          />
        ))}
      </div>
    </div>
  )
}
