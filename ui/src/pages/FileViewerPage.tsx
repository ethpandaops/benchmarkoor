import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { Check, Copy, Download } from 'lucide-react'
import { Link, useParams, useSearch, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
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
          <Check className="size-4" />
          Copied!
        </>
      ) : (
        <>
          <Copy className="size-4" />
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
      title="Download file"
    >
      <Download className="size-4" />
      Download
    </button>
  )
}

// ANSI escape code pattern
// eslint-disable-next-line no-control-regex
const ANSI_REGEX = /\x1b\[[0-9;]*m/g

function hasAnsiCodes(content: string): boolean {
  // Check first 10 lines for ANSI escape codes
  const first10Lines = content.split('\n').slice(0, 10).join('\n')
  return ANSI_REGEX.test(first10Lines)
}

function detectLanguage(content: string): string {
  // Check first few lines to detect content type
  const sample = content.slice(0, 2000)

  // JSON detection
  if (sample.trim().startsWith('{') || sample.trim().startsWith('[')) {
    return 'json'
  }

  // YAML detection
  if (/^[\w-]+:\s/m.test(sample)) {
    return 'yaml'
  }

  // Default to bash for general log output (handles timestamps, log levels, etc.)
  return 'bash'
}

// ANSI color code to CSS color mapping
const ANSI_COLORS: Record<number, string> = {
  30: '#1e1e1e', // black
  31: '#e06c75', // red
  32: '#98c379', // green
  33: '#e5c07b', // yellow
  34: '#61afef', // blue
  35: '#c678dd', // magenta
  36: '#56b6c2', // cyan
  37: '#abb2bf', // white
  90: '#5c6370', // bright black (gray)
  91: '#e06c75', // bright red
  92: '#98c379', // bright green
  93: '#e5c07b', // bright yellow
  94: '#61afef', // bright blue
  95: '#c678dd', // bright magenta
  96: '#56b6c2', // bright cyan
  97: '#ffffff', // bright white
}

const ANSI_BG_COLORS: Record<number, string> = {
  40: '#1e1e1e',
  41: '#e06c75',
  42: '#98c379',
  43: '#e5c07b',
  44: '#61afef',
  45: '#c678dd',
  46: '#56b6c2',
  47: '#abb2bf',
  100: '#5c6370',
  101: '#e06c75',
  102: '#98c379',
  103: '#e5c07b',
  104: '#61afef',
  105: '#c678dd',
  106: '#56b6c2',
  107: '#ffffff',
}

interface AnsiStyle {
  color?: string
  backgroundColor?: string
  fontWeight?: string
  fontStyle?: string
  textDecoration?: string
}

function parseAnsiCodes(codes: number[]): AnsiStyle {
  const style: AnsiStyle = {}

  for (const code of codes) {
    if (code === 0) {
      // Reset
      return {}
    } else if (code === 1) {
      style.fontWeight = 'bold'
    } else if (code === 3) {
      style.fontStyle = 'italic'
    } else if (code === 4) {
      style.textDecoration = 'underline'
    } else if (code >= 30 && code <= 37) {
      style.color = ANSI_COLORS[code]
    } else if (code >= 40 && code <= 47) {
      style.backgroundColor = ANSI_BG_COLORS[code]
    } else if (code >= 90 && code <= 97) {
      style.color = ANSI_COLORS[code]
    } else if (code >= 100 && code <= 107) {
      style.backgroundColor = ANSI_BG_COLORS[code]
    }
  }

  return style
}

function AnsiLine({ content }: { content: string }) {
  if (!content) {
    return <span>&nbsp;</span>
  }

  const parts: React.ReactNode[] = []
  let lastIndex = 0
  let currentStyle: AnsiStyle = {}
  let partKey = 0

  // Reset regex lastIndex
  // eslint-disable-next-line no-control-regex
  const regex = /\x1b\[([0-9;]*)m/g
  let match

  while ((match = regex.exec(content)) !== null) {
    // Add text before this escape code
    if (match.index > lastIndex) {
      const text = content.slice(lastIndex, match.index)
      if (text) {
        parts.push(
          <span key={partKey++} style={currentStyle}>
            {text}
          </span>
        )
      }
    }

    // Parse the ANSI codes
    const codesStr = match[1]
    if (codesStr === '' || codesStr === '0') {
      currentStyle = {}
    } else {
      const codes = codesStr.split(';').map(Number)
      const newStyle = parseAnsiCodes(codes)
      currentStyle = { ...currentStyle, ...newStyle }
      // Reset if 0 is in the codes
      if (codes.includes(0)) {
        currentStyle = parseAnsiCodes(codes.filter((c) => c !== 0))
      }
    }

    lastIndex = regex.lastIndex
  }

  // Add remaining text
  if (lastIndex < content.length) {
    const text = content.slice(lastIndex)
    if (text) {
      parts.push(
        <span key={partKey++} style={currentStyle}>
          {text}
        </span>
      )
    }
  }

  return <>{parts.length > 0 ? parts : content}</>
}

function HighlightedLine({ content, language }: { content: string; language: string }) {
  if (!content) {
    return <span>&nbsp;</span>
  }

  return (
    <SyntaxHighlighter
      language={language}
      style={oneDark}
      customStyle={{
        margin: 0,
        padding: 0,
        background: 'transparent',
        fontSize: 'inherit',
        lineHeight: 'inherit',
      }}
      codeTagProps={{
        style: {
          fontSize: 'inherit',
          lineHeight: 'inherit',
        },
      }}
      PreTag="span"
      CodeTag="span"
    >
      {content}
    </SyntaxHighlighter>
  )
}

function LogLine({ content, language, useAnsi }: { content: string; language: string; useAnsi: boolean }) {
  if (useAnsi) {
    return <AnsiLine content={content} />
  }
  return <HighlightedLine content={content} language={language} />
}

export function FileViewerPage() {
  const { runId } = useParams({ from: '/runs/$runId/fileviewer' })
  const navigate = useNavigate()
  const search = useSearch({ from: '/runs/$runId/fileviewer' }) as { file?: string; lines?: string }
  const filename = search.file
  const selectedLines = parseLineSelection(search.lines)

  const [lastClickedLine, setLastClickedLine] = useState<number | null>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const hasScrolledRef = useRef(false)

  const { data: config } = useRunConfig(runId)

  const {
    data: fileContent,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['run', runId, 'file', filename],
    queryFn: async () => {
      const { data, status } = await fetchText(`runs/${runId}/${filename}`)
      if (!data) {
        throw new Error(`Failed to fetch log: ${status}`)
      }
      return data
    },
    enabled: !!runId && !!filename,
  })

  const updateSelectedLines = useCallback(
    (newSelected: Set<number>) => {
      navigate({
        to: '/runs/$runId/fileviewer',
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

  const useAnsi = useMemo(() => fileContent ? hasAnsiCodes(fileContent) : false, [fileContent])
  const language = useMemo(() => fileContent ? detectLanguage(fileContent) : 'bash', [fileContent])

  // Scroll to first selected line on initial load
  useEffect(() => {
    if (fileContent && selectedLines.size > 0 && !hasScrolledRef.current) {
      const firstLine = Math.min(...selectedLines)
      const row = document.getElementById(`log-line-${firstLine}`)
      if (row && scrollContainerRef.current) {
        row.scrollIntoView({ block: 'center' })
        hasScrolledRef.current = true
      }
    }
  }, [fileContent, selectedLines])

  if (!filename) {
    return <ErrorState message="No file specified" />
  }

  if (isLoading) {
    return <LoadingState message="Loading file..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (!fileContent) {
    return <ErrorState message="File not found" />
  }

  const lines = fileContent.split('\n')
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

      <div className="relative left-1/2 right-1/2 -ml-[50vw] -mr-[50vw] w-screen overflow-hidden bg-gray-900 shadow-xs">
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
            <CopyButton text={fileContent} label="Copy all" />
            <DownloadButton content={fileContent} filename={filename} />
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
                      <LogLine content={line} language={language} useAnsi={useAnsi} />
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
