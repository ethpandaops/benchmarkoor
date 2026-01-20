import { useState, useEffect, useRef, useCallback } from 'react'
import { Link, useParams, useSearch, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { fetchText } from '@/api/client'
import { useRunConfig } from '@/api/hooks/useRunConfig'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { JDenticon } from '@/components/shared/JDenticon'

function parseLineSelection(linesParam: string | undefined): Set<number> {
  if (!linesParam) return new Set()
  const selected = new Set<number>()
  const parts = linesParam.split(',')
  for (const part of parts) {
    if (part.includes('-')) {
      const [start, end] = part.split('-').map(Number)
      if (!isNaN(start) && !isNaN(end)) {
        for (let i = Math.min(start, end); i <= Math.max(start, end); i++) {
          selected.add(i)
        }
      }
    } else {
      const num = Number(part)
      if (!isNaN(num)) selected.add(num)
    }
  }
  return selected
}

function serializeLineSelection(selected: Set<number>): string | undefined {
  if (selected.size === 0) return undefined
  const sorted = Array.from(selected).sort((a, b) => a - b)
  const ranges: string[] = []
  let rangeStart = sorted[0]
  let rangeEnd = sorted[0]

  for (let i = 1; i <= sorted.length; i++) {
    if (i < sorted.length && sorted[i] === rangeEnd + 1) {
      rangeEnd = sorted[i]
    } else {
      if (rangeStart === rangeEnd) {
        ranges.push(String(rangeStart))
      } else {
        ranges.push(`${rangeStart}-${rangeEnd}`)
      }
      if (i < sorted.length) {
        rangeStart = sorted[i]
        rangeEnd = sorted[i]
      }
    }
  }
  return ranges.join(',')
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="flex items-center gap-1.5 rounded-sm bg-gray-700 px-2.5 py-1.5 text-xs/5 font-medium text-gray-200 hover:bg-gray-600"
      title="Copy to clipboard"
    >
      {copied ? (
        <>
          <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
          Copied!
        </>
      ) : (
        <>
          <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
            />
          </svg>
          {label}
        </>
      )}
    </button>
  )
}

function DownloadButton({ content, filename }: { content: string; filename: string }) {
  const handleDownload = () => {
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <button
      onClick={handleDownload}
      className="flex items-center gap-1.5 rounded-sm bg-gray-700 px-2.5 py-1.5 text-xs/5 font-medium text-gray-200 hover:bg-gray-600"
      title="Download log file"
    >
      <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
        />
      </svg>
      Download
    </button>
  )
}

export function LogViewerPage() {
  const { runId } = useParams({ from: '/runs/$runId/logs' })
  const navigate = useNavigate()
  const search = useSearch({ from: '/runs/$runId/logs' }) as { file?: string; lines?: string }
  const filename = search.file
  const selectedLines = parseLineSelection(search.lines)

  const [lastClickedLine, setLastClickedLine] = useState<number | null>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const hasScrolledRef = useRef(false)

  const { data: config } = useRunConfig(runId)

  const {
    data: logContent,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['run', runId, 'logs', filename],
    queryFn: () => fetchText(`runs/${runId}/${filename}`),
    enabled: !!runId && !!filename,
  })

  const updateSelectedLines = useCallback(
    (newSelected: Set<number>) => {
      navigate({
        to: '/runs/$runId/logs',
        params: { runId },
        search: {
          file: filename,
          lines: serializeLineSelection(newSelected),
        },
        replace: true,
      })
    },
    [navigate, runId, filename]
  )

  const handleLineClick = useCallback(
    (lineNum: number, event: React.MouseEvent) => {
      const newSelected = new Set(selectedLines)

      if (event.shiftKey && lastClickedLine !== null) {
        // Range selection
        const start = Math.min(lastClickedLine, lineNum)
        const end = Math.max(lastClickedLine, lineNum)
        for (let i = start; i <= end; i++) {
          newSelected.add(i)
        }
      } else if (event.metaKey || event.ctrlKey) {
        // Toggle selection
        if (newSelected.has(lineNum)) {
          newSelected.delete(lineNum)
        } else {
          newSelected.add(lineNum)
        }
        setLastClickedLine(lineNum)
      } else {
        // Single selection (replace)
        newSelected.clear()
        newSelected.add(lineNum)
        setLastClickedLine(lineNum)
      }

      updateSelectedLines(newSelected)
    },
    [selectedLines, lastClickedLine, updateSelectedLines]
  )

  // Scroll to first selected line on initial load
  useEffect(() => {
    if (logContent && selectedLines.size > 0 && !hasScrolledRef.current) {
      const firstLine = Math.min(...selectedLines)
      const row = document.getElementById(`log-line-${firstLine}`)
      if (row && scrollContainerRef.current) {
        row.scrollIntoView({ block: 'center' })
        hasScrolledRef.current = true
      }
    }
  }, [logContent, selectedLines])

  if (!filename) {
    return <ErrorState message="No file specified. Use ?file=filename.log" />
  }

  if (isLoading) {
    return <LoadingState message="Loading logs..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (!logContent) {
    return <ErrorState message="Log file not found" />
  }

  const lines = logContent.split('\n')
  const selectedContent = selectedLines.size > 0
    ? Array.from(selectedLines)
        .sort((a, b) => a - b)
        .map((lineNum) => lines[lineNum - 1] ?? '')
        .join('\n')
    : null

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/suites" className="hover:text-gray-700 dark:hover:text-gray-300">
          Suites
        </Link>
        <span>/</span>
        {config?.suite_hash && (
          <>
            <Link
              to="/suites/$suiteHash"
              params={{ suiteHash: config.suite_hash }}
              className="flex items-center gap-1.5 font-mono hover:text-gray-700 dark:hover:text-gray-300"
            >
              <JDenticon value={config.suite_hash} size={16} className="shrink-0 rounded-xs" />
              {config.suite_hash}
            </Link>
            <span>/</span>
          </>
        )}
        <Link
          to="/runs/$runId"
          params={{ runId }}
          className="hover:text-gray-700 dark:hover:text-gray-300"
        >
          {runId}
        </Link>
        <span>/</span>
        <span className="font-mono text-gray-900 dark:text-gray-100">{filename}</span>
      </div>

      <div className="overflow-hidden rounded-sm bg-gray-900 shadow-xs">
        <div className="flex items-center justify-between border-b border-gray-700 px-4 py-3">
          <div className="flex items-center gap-3">
            <h3 className="font-mono text-sm/6 font-medium text-gray-100">{filename}</h3>
            {selectedLines.size > 0 && (
              <span className="text-xs/5 text-gray-400">
                {selectedLines.size} line{selectedLines.size !== 1 ? 's' : ''} selected
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {selectedContent && <CopyButton text={selectedContent} label="Copy selected" />}
            <CopyButton text={logContent} label="Copy all" />
            <DownloadButton content={logContent} filename={filename} />
          </div>
        </div>
        <div ref={scrollContainerRef} className="max-h-[80vh] overflow-y-auto">
          <table className="w-full">
            <tbody>
              {lines.map((line, i) => {
                const lineNum = i + 1
                const isSelected = selectedLines.has(lineNum)
                return (
                  <tr
                    key={i}
                    id={`log-line-${lineNum}`}
                    className={isSelected ? 'bg-yellow-500/20' : 'hover:bg-gray-800/50'}
                  >
                    <td
                      onClick={(e) => handleLineClick(lineNum, e)}
                      className={`cursor-pointer select-none whitespace-nowrap py-0.5 pl-4 pr-4 text-right align-top font-mono text-xs/5 ${
                        isSelected
                          ? 'text-yellow-400 hover:text-yellow-300'
                          : 'text-gray-500 hover:text-gray-300'
                      }`}
                    >
                      {lineNum}
                    </td>
                    <td className="whitespace-pre-wrap break-all py-0.5 pr-4 font-mono text-xs/5 text-gray-100">
                      {line}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
