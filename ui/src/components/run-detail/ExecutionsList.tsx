import { useState, useCallback, useEffect } from 'react'
import clsx from 'clsx'
import { ChevronRight } from 'lucide-react'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useTestRequests, useTestResponses, useTestResultDetails, useTestRequestSummaries, type StepType } from '@/api/hooks/useTestDetails'
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
  stepType: StepType
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

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

interface ExecutionRowProps {
  index: number
  request?: string
  requestSize?: number
  /** Method name from partial fetch (used when full request is unavailable). */
  methodName?: string
  response?: string
  time?: number
  status?: number // 0=success, 1=fail
  mgasPerSec?: number
  gasUsed?: number
}

function StatusIndicator({ status }: { status?: number }) {
  if (status === undefined) return null

  const isSuccess = status === 0

  return (
    <span
      className={clsx(
        'shrink-0 rounded-full px-2 py-0.5 text-xs/5 font-medium',
        isSuccess
          ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
          : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400',
      )}
    >
      {isSuccess ? 'OK' : 'FAIL'}
    </span>
  )
}

function ExecutionRow({ index, request, requestSize, methodName, response, time, status, mgasPerSec, gasUsed }: ExecutionRowProps) {
  const [expanded, setExpanded] = useState(false)
  const method = request ? parseMethod(request) : methodName
  const canExpand = !!request || !!response

  return (
    <div className="max-w-full overflow-hidden border-b border-gray-200 last:border-b-0 dark:border-gray-700">
      <button
        onClick={() => canExpand && setExpanded(!expanded)}
        className={clsx(
          'flex w-full items-center gap-3 px-3 py-2 text-left transition-colors',
          canExpand ? 'cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-800' : 'cursor-default',
          expanded && 'bg-gray-100 dark:bg-gray-800',
        )}
      >
        {canExpand ? (
          <ChevronRight className={clsx('size-4 shrink-0 text-gray-400 transition-transform', expanded && 'rotate-90')} />
        ) : (
          <span className="size-4 shrink-0" />
        )}
        <span className="w-10 shrink-0 font-mono text-sm/6 text-gray-500 dark:text-gray-400">#{index}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-sm/6 text-gray-900 dark:text-gray-100">{method ?? '-'}</span>
        {mgasPerSec !== undefined && (
          <span className="shrink-0 text-sm/6 font-medium text-blue-600 dark:text-blue-400">
            {mgasPerSec.toFixed(2)} MGas/s
            {gasUsed !== undefined && (
              <span className="ml-1 font-normal text-gray-500 dark:text-gray-400">
                ({(gasUsed / 1e6).toFixed(2)}M gas)
              </span>
            )}
          </span>
        )}
        {(requestSize !== undefined || request || response) && (
          <span className="shrink-0 text-xs text-gray-400 dark:text-gray-500">
            {(requestSize !== undefined || request) && (
              <span title="Request size">
                {formatBytes(requestSize ?? new Blob([request!]).size)}
              </span>
            )}
            {(requestSize !== undefined || request) && response && ' / '}
            {response && <span title="Response size">{formatBytes(new Blob([response]).size)}</span>}
          </span>
        )}
        {time !== undefined && (
          <span className="shrink-0 text-sm/6 text-gray-500 dark:text-gray-400">
            <Duration nanoseconds={time} />
          </span>
        )}
        <StatusIndicator status={status} />
      </button>

      {expanded && (
        <div className="bg-gray-50 px-4 py-3 dark:bg-gray-900/50">
          <div className="flex flex-col gap-3">
            {request && (
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
            )}
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

const EXECUTIONS_PAGE_SIZE = 100

export function ExecutionsList({ runId, suiteHash, testName, stepType }: ExecutionsListProps) {
  const { data: requests, isLoading: requestsLoading, error: requestsError } = useTestRequests(suiteHash, testName, stepType)
  const { data: responses, error: responsesError } = useTestResponses(runId, testName, stepType)
  const { data: resultDetails, isLoading: detailsLoading, error: detailsError } = useTestResultDetails(runId, testName, stepType)
  const { data: requestSummaries } = useTestRequestSummaries(suiteHash, testName, stepType)
  const [page, setPage] = useState(1)

  // Treat response/detail fetch errors as missing data (not all steps have responses)
  const safeRequests = requestsError ? undefined : requests
  const safeResponses = responsesError ? undefined : responses
  const safeDetails = detailsError ? undefined : resultDetails

  // Wait for at least one data source (details or requests) to be ready
  const isLoading = (!requestsError && requestsLoading) && (!detailsError && detailsLoading)

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-4">
        <div className="size-5 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600" />
        <span className="ml-2 text-sm/6 text-gray-500 dark:text-gray-400">Loading executions...</span>
      </div>
    )
  }

  // Derive execution count from whichever source is available
  const executionCount = safeDetails?.duration_ns.length ?? safeRequests?.length ?? requestSummaries?.length ?? 0

  if (executionCount === 0) {
    return <p className="py-2 text-sm/6 text-gray-500 dark:text-gray-400">No execution data available</p>
  }

  const totalDurationNs = Array.isArray(safeDetails?.duration_ns)
    ? safeDetails.duration_ns.reduce((sum, ns) => sum + ns, 0)
    : 0
  const totalGasUsed = safeDetails?.gas_used
    ? Object.values(safeDetails.gas_used).reduce((sum, g) => sum + g, 0)
    : 0

  const totalPages = Math.ceil(executionCount / EXECUTIONS_PAGE_SIZE)
  const startIdx = (page - 1) * EXECUTIONS_PAGE_SIZE
  const endIdx = Math.min(startIdx + EXECUTIONS_PAGE_SIZE, executionCount)

  return (
    <div className="mt-4 max-w-full overflow-hidden">
      <div className="mb-2 flex items-center justify-between">
        <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          Executions ({executionCount})
        </h4>
        {totalDurationNs > 0 && (
          <span className="text-sm/6 text-gray-500 dark:text-gray-400">
            Total: <Duration nanoseconds={totalDurationNs} />
            {totalGasUsed > 0 && (
              <span className="ml-2 text-xs text-gray-400 dark:text-gray-500">
                ({(totalGasUsed / 1_000_000).toFixed(2)} MGas)
              </span>
            )}
          </span>
        )}
      </div>
      <div className="overflow-hidden rounded-xs border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
        {Array.from({ length: endIdx - startIdx }, (_, i) => {
          const index = startIdx + i
          return (
            <ExecutionRow
              key={index}
              index={index}
              request={safeRequests?.[index]}
              requestSize={requestSummaries?.[index]?.size}
              methodName={requestSummaries?.[index]?.head.match(/"method"\s*:\s*"([^"]+)"/)?.[1]}
              response={safeResponses?.[index]}
              time={safeDetails?.duration_ns[index]}
              status={safeDetails?.status[index]}
              mgasPerSec={safeDetails?.mgas_s[String(index)]}
              gasUsed={safeDetails?.gas_used[String(index)]}
            />
          )
        })}
      </div>
      {totalPages > 1 && (
        <div className="mt-2 flex items-center justify-between text-xs/5 text-gray-500 dark:text-gray-400">
          <span>Showing {startIdx + 1}–{endIdx} of {executionCount}</span>
          <div className="flex gap-1">
            <button
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
              className="rounded-xs px-2 py-0.5 transition-colors hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-gray-700"
            >
              Prev
            </button>
            <button
              disabled={page >= totalPages}
              onClick={() => setPage(page + 1)}
              className="rounded-xs px-2 py-0.5 transition-colors hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-gray-700"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
