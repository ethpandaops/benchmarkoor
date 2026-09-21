import { useState, useCallback, useEffect } from 'react'
import clsx from 'clsx'
import { useQuery } from '@tanstack/react-query'
import { ChevronRight } from 'lucide-react'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useTestRequests, useTestResponses, useTestResultDetails, useTestRequestSummaries, useTestResponseSummaries, type StepType } from '@/api/hooks/useTestDetails'
import { fetchPartialText } from '@/api/client'
import type { PayloadSizeBuckets } from '@/api/types'
import { Duration } from '@/components/shared/Duration'
import {
  DEFAULT_SLOW_MS,
  DEFAULT_THRESHOLD,
  formatSlowMs,
  getTextClassByDuration,
  getTextClassByThreshold,
} from '@/utils/perfThreshold'

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
  /**
   * Run that holds the responses, the timings and the statuses. Leave it
   * out for a suite-only list: the rows then show the request side alone,
   * which is the same for every run of the suite.
   */
  runId?: string
  suiteHash: string
  testName: string
  stepType: StepType
  expandedRows?: Set<number>
  onExpandedRowsChange?: (rows: Set<number>) => void
  /**
   * Per-newPayload transaction counts for the active step (typically
   * suiteTest.tx_counts[stepType]). Indexed by newPayload order, not by
   * row position — non-newPayload methods consume no slot.
   */
  txCounts?: number[]
  /**
   * Per-newPayload byte counts for the active step (typically
   * suiteTest.payload_sizes[stepType]). Indexed by newPayload order, the
   * same convention as `txCounts`.
   */
  payloadSizes?: PayloadSizeBuckets
  /** Slow-threshold MGas/s the per-call values colour against. */
  threshold?: number
  /** Slow-payload limit in milliseconds the payload times colour against. */
  slowMs?: number
}

/** Encoded sizes of one engine_newPayload, taken from the suite. */
interface PayloadSize {
  ssz: number
  bal: number
  snappy: number
  json: number
  jsonBal: number
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

/** Tooltip of the size badge: every encoding of the same payload. */
function payloadSizeTitle({ ssz, bal, snappy, json, jsonBal }: PayloadSize): string {
  const pct = (part: number, whole: number) => (whole > 0 ? ` (${((part / whole) * 100).toFixed(1)}%)` : '')
  const lines = [`SSZ ${formatBytes(ssz)}`]
  if (bal > 0) lines.push(`SSZ BAL ${formatBytes(bal)}${pct(bal, ssz)}`)
  if (snappy > 0) lines.push(`SSZ snappy ${formatBytes(snappy)}${pct(snappy, ssz)}`)
  if (json > 0) lines.push(`JSON ${formatBytes(json)}`)
  if (jsonBal > 0) lines.push(`JSON BAL ${formatBytes(jsonBal)}${pct(jsonBal, json)}`)
  return lines.join('\n')
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

interface LazyLineInfo {
  /** Full path to the file (e.g. "suites/{hash}/{test}/setup.request") */
  filePath: string
  /** Byte offset of this line within the file */
  byteOffset: number
  /** Byte size of this line */
  byteSize: number
}

interface ExecutionRowProps {
  index: number
  request?: string
  requestSize?: number
  /** Method name from partial fetch (used when full request is unavailable). */
  methodName?: string
  /** Info for lazy-loading request content on expand (when full file was too large). */
  requestLineInfo?: LazyLineInfo
  response?: string
  responseSize?: number
  time?: number
  status?: number // 0=success, 1=fail
  mgasPerSec?: number
  gasUsed?: number
  /** Transaction count for this row when the method is engine_newPayloadV*. */
  txCount?: number
  /** Encoded sizes of this row's payload, when the method is engine_newPayloadV*. */
  payloadSize?: PayloadSize
  /** File viewer link for the response file at this line. */
  responseViewerUrl?: string
  /** File viewer link for the request file at this line. */
  requestViewerUrl?: string
  /** Drop the columns that need a run: the response size, the timings and the status. */
  requestsOnly?: boolean
  /** Slow-threshold MGas/s the call's MGas/s colours against. */
  threshold: number
  /** Slow-payload limit in milliseconds the call's time colours against. */
  slowMs: number
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

const MAX_LAZY_LINE_SIZE = 1_000_000 // 1MB — lazy-load lines up to this size

function ExecutionRow({ index, request, requestSize, methodName, requestLineInfo, response, responseSize, time, status, mgasPerSec, gasUsed, txCount, payloadSize, responseViewerUrl, requestViewerUrl, requestsOnly = false, threshold, slowMs, expanded: expandedProp, onExpandedChange }: ExecutionRowProps & { expanded?: boolean; onExpandedChange?: (index: number, expanded: boolean) => void }) {
  const [expandedLocal, setExpandedLocal] = useState(false)
  const expanded = expandedProp ?? expandedLocal
  const setExpanded = (v: boolean) => {
    setExpandedLocal(v)
    onExpandedChange?.(index, v)
  }
  const method = request ? parseMethod(request) : methodName

  // Lazy-load request content for lines that are small enough but weren't in the full fetch
  const canLazyLoad = !request && !!requestLineInfo && requestLineInfo.byteSize <= MAX_LAZY_LINE_SIZE
  const { data: lazyRequest } = useQuery({
    queryKey: ['lazy-line', requestLineInfo?.filePath, requestLineInfo?.byteOffset, requestLineInfo?.byteSize],
    queryFn: async () => {
      if (!requestLineInfo) return null
      const { data } = await fetchPartialText(requestLineInfo.filePath, requestLineInfo.byteSize, requestLineInfo.byteOffset)
      return data
    },
    enabled: expanded && canLazyLoad,
  })

  const effectiveRequest = request ?? lazyRequest ?? undefined
  const effectiveRequestSize = requestSize ?? (request ? new Blob([request]).size : undefined)
  const effectiveResponseSize = responseSize ?? (response ? new Blob([response]).size : undefined)
  const canExpand = !!effectiveRequest || !!response || !!responseViewerUrl || !!requestViewerUrl || canLazyLoad
  const isFail = status !== undefined && status !== 0
  // A call that reports gas is a payload submission.
  const isPayload = mgasPerSec !== undefined

  return (
    <div className={clsx(
      'max-w-full overflow-hidden border-b last:border-b-0',
      isFail
        ? 'border-red-300 bg-red-100 dark:border-red-800/50 dark:bg-red-900/30'
        : 'border-gray-200 dark:border-gray-700',
    )}>
      <button
        onClick={() => canExpand && setExpanded(!expanded)}
        className={clsx(
          'flex w-full items-center gap-3 px-3 py-2 text-left transition-colors',
          canExpand
            ? isFail
              ? 'cursor-pointer hover:bg-red-200 dark:hover:bg-red-900/40'
              : 'cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-800'
            : 'cursor-default',
          expanded && (isFail ? 'bg-red-200 dark:bg-red-900/40' : 'bg-gray-100 dark:bg-gray-800'),
        )}
      >
        {canExpand ? (
          <ChevronRight className={clsx('size-4 shrink-0 text-gray-400 transition-transform', expanded && 'rotate-90')} />
        ) : (
          <span className="size-4 shrink-0" />
        )}
        <span className="w-10 shrink-0 font-mono text-sm/6 text-gray-500 dark:text-gray-400">#{index}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-sm/6 text-gray-900 dark:text-gray-100">
          {method ?? (requestSize === undefined
            ? <span className="inline-block size-3 animate-spin rounded-full border border-gray-300 border-t-gray-600" />
            : '-')}
          {txCount !== undefined && (
            <span
              className="ml-2 inline-block rounded-xs bg-violet-100 px-1.5 py-0.5 font-sans text-xs/5 font-medium text-violet-700 dark:bg-violet-900/40 dark:text-violet-300"
              title={`${txCount.toLocaleString()} transaction${txCount === 1 ? '' : 's'} in this engine_newPayload`}
            >
              {txCount.toLocaleString()} tx{txCount === 1 ? '' : 's'}
            </span>
          )}
        </span>
        {!requestsOnly && (
          <span
            className={clsx(
              'w-44 shrink-0 text-right text-sm/6 font-medium',
              mgasPerSec !== undefined ? getTextClassByThreshold(mgasPerSec, threshold) : '',
            )}
            title={mgasPerSec !== undefined ? `Threshold ${threshold} MGas/s` : undefined}
          >
            {mgasPerSec !== undefined ? (
              <>
                {mgasPerSec.toFixed(2)} MGas/s
                {gasUsed !== undefined && (
                  <span className="ml-1 font-normal text-gray-500 dark:text-gray-400">
                    ({(gasUsed / 1e6).toFixed(2)}M)
                  </span>
                )}
              </>
            ) : ''}
          </span>
        )}
        {!requestsOnly && (
          <span
            className={clsx(
              'w-16 shrink-0 text-right text-sm/6',
              // Only a call that executed gas is a payload, so only it
              // colours against the slow-payload limit.
              time !== undefined && isPayload
                ? getTextClassByDuration(time, slowMs)
                : 'text-gray-500 dark:text-gray-400',
            )}
            title={time !== undefined && isPayload ? `Slow-payload limit ${formatSlowMs(slowMs)}` : undefined}
          >
            {time !== undefined ? <Duration nanoseconds={time} /> : ''}
          </span>
        )}
        <span className="w-24 shrink-0 text-right">
          {payloadSize !== undefined && payloadSize.ssz > 0 && (
            <span
              className="inline-block rounded-xs bg-sky-100 px-1.5 py-0.5 text-xs/5 font-medium text-sky-700 dark:bg-sky-900/40 dark:text-sky-300"
              title={payloadSizeTitle(payloadSize)}
            >
              {formatBytes(payloadSize.ssz)} SSZ
            </span>
          )}
        </span>
        <span className="w-40 shrink-0 text-right text-xs text-gray-400 dark:text-gray-500">
          {requestSize !== undefined || request ? (
            <span
              className="inline-block rounded-xs bg-emerald-100 px-1.5 py-0.5 text-xs/5 font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300"
              title="JSON-RPC request size, the whole line"
            >
              {formatBytes(requestSize ?? new Blob([request!]).size)} JSON
            </span>
          ) : (
            <span className="inline-block size-2.5 animate-spin rounded-full border border-gray-300 border-t-gray-500" />
          )}
          {!requestsOnly && (
            <>
              {' / '}
              {responseSize !== undefined || response ? (
                <span title="JSON-RPC response size, the whole line">
                  {formatBytes(responseSize ?? new Blob([response!]).size)}
                </span>
              ) : (
                <span className="inline-block size-2.5 animate-spin rounded-full border border-gray-300 border-t-gray-500" />
              )}
            </>
          )}
        </span>
        {!requestsOnly && (
          <span className="w-12 shrink-0 text-right">
            <StatusIndicator status={status} />
          </span>
        )}
      </button>

      {expanded && (
        <div className="bg-gray-50 px-4 py-3 dark:bg-gray-900/50">
          <div className="flex flex-col gap-3">
            {effectiveRequest && (
              <div>
                <div className="mb-1 flex items-center justify-between">
                  <h5 className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Request{effectiveRequestSize !== undefined && <span className="ml-1 normal-case tracking-normal">({formatBytes(effectiveRequestSize)})</span>}
                  </h5>
                  <CopyButton text={formatJson(effectiveRequest)} />
                </div>
                <div className="w-0 min-w-full overflow-x-auto rounded-xs bg-gray-100 dark:bg-gray-800">
                  <JsonBlock code={formatJson(effectiveRequest)} />
                </div>
              </div>
            )}
            {!effectiveRequest && canLazyLoad && expanded && (
              <div>
                <h5 className="mb-1 text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Request
                </h5>
                <div className="flex items-center gap-2 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                  <div className="size-4 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600" />
                  Loading...
                </div>
              </div>
            )}
            {!effectiveRequest && !canLazyLoad && requestViewerUrl && (
              <div>
                <h5 className="mb-1 text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Request{effectiveRequestSize !== undefined && <span className="ml-1 normal-case tracking-normal">({formatBytes(effectiveRequestSize)})</span>}
                </h5>
                <div className="rounded-xs bg-gray-100 px-3 py-2 text-sm/6 text-gray-500 dark:bg-gray-800 dark:text-gray-400">
                  This file is too large to display here.{' '}
                  <a
                    href={requestViewerUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium text-blue-600 hover:text-blue-700 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
                  >
                    View request in file viewer →
                  </a>
                </div>
              </div>
            )}
            {response && (
              <div>
                <div className="mb-1 flex items-center justify-between">
                  <h5 className="text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Response{effectiveResponseSize !== undefined && <span className="ml-1 normal-case tracking-normal">({formatBytes(effectiveResponseSize)})</span>}
                  </h5>
                  <CopyButton text={formatJson(response)} />
                </div>
                <div className="w-0 min-w-full overflow-x-auto rounded-xs bg-gray-100 dark:bg-gray-800">
                  <JsonBlock code={formatJson(response)} />
                </div>
              </div>
            )}
            {!response && responseViewerUrl && (
              <div>
                <h5 className="mb-1 text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Response{effectiveResponseSize !== undefined && <span className="ml-1 normal-case tracking-normal">({formatBytes(effectiveResponseSize)})</span>}
                </h5>
                <div className="rounded-xs bg-gray-100 px-3 py-2 text-sm/6 text-gray-500 dark:bg-gray-800 dark:text-gray-400">
                  This file is too large to display here.{' '}
                  <a
                    href={responseViewerUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium text-blue-600 hover:text-blue-700 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
                  >
                    View response in file viewer →
                  </a>
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

export function ExecutionsList({ runId = '', suiteHash, testName, stepType, expandedRows, onExpandedRowsChange, txCounts, payloadSizes, threshold = DEFAULT_THRESHOLD, slowMs = DEFAULT_SLOW_MS }: ExecutionsListProps) {
  // Without a run there are no responses, no timings and no statuses. The
  // hooks below stay disabled and the rows keep the request side only.
  const requestsOnly = !runId
  const { data: requests, isLoading: requestsLoading, error: requestsError } = useTestRequests(suiteHash, testName, stepType)
  const { data: responses, error: responsesError } = useTestResponses(runId, testName, stepType)
  const { data: resultDetails, isLoading: detailsLoading, error: detailsError } = useTestResultDetails(runId, testName, stepType)
  const { data: requestSummaries } = useTestRequestSummaries(suiteHash, testName, stepType)
  const { data: responseSummaries } = useTestResponseSummaries(runId, testName, stepType)
  const [page, setPage] = useState(1)

  // Compute byte offsets for each request line (for lazy Range fetches)
  const requestFilePath = `suites/${suiteHash}/${testName}/${stepType}.request`
  const requestByteOffsets = requestSummaries
    ? (() => {
        const offsets: number[] = []
        let offset = 0
        for (const s of requestSummaries) {
          offsets.push(offset)
          offset += s.size + 1 // +1 for the newline
        }
        return offsets
      })()
    : undefined

  // Treat response/detail fetch errors as missing data (not all steps have responses)
  const safeRequests = requestsError ? undefined : requests
  const safeResponses = responsesError ? undefined : responses
  const safeDetails = detailsError ? undefined : resultDetails

  // Wait for at least one data source (details or requests) to be ready.
  // The suite-only list has the requests as its one source.
  const isLoading = requestsOnly
    ? !requestsError && requestsLoading
    : (!requestsError && requestsLoading) && (!detailsError && detailsLoading)

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

  // Map row index → newPayload ordinal. The suite arrays (tx counts,
  // payload sizes) are indexed by newPayload order, not by row position,
  // so we walk every row once to assign each newPayload row its slot.
  // The Map is keyed by absolute row index (not page-relative).
  const npIndexByRow = (() => {
    const out = new Map<number, number>()
    if (!txCounts?.length && !payloadSizes) return out
    let nextNp = 0
    const rowMethod = (i: number): string | undefined => {
      const req = safeRequests?.[i]
      if (req) return parseMethod(req)
      return requestSummaries?.[i]?.head.match(/"method"\s*:\s*"([^"]+)"/)?.[1]
    }
    for (let i = 0; i < executionCount; i++) {
      const m = rowMethod(i)
      if (m && m.startsWith('engine_newPayloadV')) out.set(i, nextNp++)
    }
    return out
  })()

  const txCountOf = (row: number): number | undefined => {
    const np = npIndexByRow.get(row)
    return np !== undefined ? txCounts?.[np] : undefined
  }
  const payloadSizeOf = (row: number): PayloadSize | undefined => {
    const np = npIndexByRow.get(row)
    if (np === undefined || !payloadSizes) return undefined
    const ssz = payloadSizes.ssz_full[np] ?? 0
    if (ssz === 0) return undefined
    return {
      ssz,
      bal: payloadSizes.ssz_bal[np] ?? 0,
      snappy: payloadSizes.ssz_full_snappy[np] ?? 0,
      json: payloadSizes.json_full[np] ?? 0,
      jsonBal: payloadSizes.json_bal[np] ?? 0,
    }
  }

  return (
    <div className="mt-4 max-w-full overflow-hidden">
      <div className="mb-2 flex items-center justify-between">
        <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          {requestsOnly ? 'Requests' : 'Executions'} ({executionCount})
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
              requestLineInfo={!safeRequests?.[index] && requestSummaries?.[index] && requestByteOffsets
                ? { filePath: requestFilePath, byteOffset: requestByteOffsets[index], byteSize: requestSummaries[index].size }
                : undefined}
              response={safeResponses?.[index]}
              responseSize={responseSummaries?.[index]?.size}
              time={safeDetails?.duration_ns[index]}
              status={safeDetails?.status[index]}
              mgasPerSec={safeDetails?.mgas_s[String(index)]}
              gasUsed={safeDetails?.gas_used[String(index)]}
              txCount={txCountOf(index)}
              payloadSize={payloadSizeOf(index)}
              requestsOnly={requestsOnly}
              threshold={threshold}
              slowMs={slowMs}
              responseViewerUrl={!requestsOnly && !safeResponses?.[index] && responseSummaries?.[index] && responseSummaries[index].size > 1_000_000
                ? `/runs/${runId}/fileviewer?file=${encodeURIComponent(`${testName}/${stepType}.response`)}&lines=${index + 1}`
                : undefined}
              requestViewerUrl={!requestsOnly && !safeRequests?.[index] && requestSummaries?.[index] && requestSummaries[index].size > 1_000_000
                ? `/runs/${runId}/fileviewer?base=${encodeURIComponent(`suites/${suiteHash}`)}&file=${encodeURIComponent(`${testName}/${stepType}.request`)}&lines=${index + 1}`
                : undefined}
              expanded={expandedRows?.has(index)}
              onExpandedChange={(idx, exp) => {
                const next = new Set(expandedRows)
                if (exp) next.add(idx); else next.delete(idx)
                onExpandedRowsChange?.(next)
              }}
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
