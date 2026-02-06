import { useState, useMemo, useCallback, useRef } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useVirtualizer } from '@tanstack/react-virtual'
import clsx from 'clsx'
import { FolderOpen, Folder, Check, Copy, Download, List, ChevronDown, ChevronRight, File, FileText } from 'lucide-react'
import type { PostTestRPCCallConfig, TestEntry } from '@/api/types'
import { fetchHead, type HeadResult } from '@/api/client'
import { formatBytes } from '@/utils/format'
import { getDataUrl, loadRuntimeConfig, toAbsoluteUrl } from '@/config/runtime'
import { Modal } from '@/components/shared/Modal'

type DownloadListFormat = 'urls' | 'curl'

interface FilesPanelProps {
  runId: string
  tests: Record<string, TestEntry>
  postTestRPCCalls?: PostTestRPCCallConfig[]
  showDownloadList: boolean
  downloadFormat: DownloadListFormat
  onShowDownloadListChange: (open: boolean) => void
  onDownloadFormatChange: (format: DownloadListFormat) => void
}

interface FileEntry {
  testName: string
  filename: string
  path: string
  displayPath: string
  outputPath: string
}

interface TreeNode {
  id: string
  name: string
  type: 'file' | 'directory'
  entry?: FileEntry
  children?: TreeNode[]
  depth: number
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
    </button>
  )
}

// --- Entry generators ---

const GENERAL_FILES = ['benchmarkoor.log', 'container.log', 'config.json', 'result.json'] as const

function buildGeneralEntries(runId: string): FileEntry[] {
  return GENERAL_FILES.map((filename) => ({
    testName: '',
    filename,
    path: `runs/${runId}/${filename}`,
    displayPath: filename,
    outputPath: `${runId}/${filename}`,
  }))
}

const ALL_STEPS = ['setup', 'test', 'cleanup'] as const
type StepName = (typeof ALL_STEPS)[number]

function getTestSteps(entry: TestEntry): StepName[] {
  if (!entry.steps) return []
  return ALL_STEPS.filter((step) => entry.steps![step] != null)
}

function buildTestStatsEntries(runId: string, tests: Record<string, TestEntry>): FileEntry[] {
  const entries: FileEntry[] = []
  for (const [testName, testEntry] of Object.entries(tests)) {
    for (const step of getTestSteps(testEntry)) {
      for (const suffix of ['result-aggregated.json', 'result-details.json']) {
        const filename = `${step}.${suffix}`
        entries.push({
          testName,
          filename,
          path: `runs/${runId}/${testName}/${filename}`,
          displayPath: filename,
          outputPath: `${runId}/${testName}/${filename}`,
        })
      }
    }
  }
  return entries
}

function buildTestResponsesEntries(runId: string, tests: Record<string, TestEntry>): FileEntry[] {
  const entries: FileEntry[] = []
  for (const [testName, testEntry] of Object.entries(tests)) {
    for (const step of getTestSteps(testEntry)) {
      const filename = `${step}.response`
      entries.push({
        testName,
        filename,
        path: `runs/${runId}/${testName}/${filename}`,
        displayPath: filename,
        outputPath: `${runId}/${testName}/${filename}`,
      })
    }
  }
  return entries
}

function buildPostTestDumpEntries(runId: string, testNames: string[], postTestRPCCalls: PostTestRPCCallConfig[]): FileEntry[] {
  const dumpCalls = postTestRPCCalls.filter((c) => c.dump?.enabled && c.dump.filename)
  const entries: FileEntry[] = []
  for (const testName of testNames) {
    for (const call of dumpCalls) {
      const filename = `${call.dump!.filename}.json`
      entries.push({
        testName,
        filename,
        path: `runs/${runId}/${testName}/post_test_rpc_calls/${filename}`,
        displayPath: `post_test_rpc_calls/${filename}`,
        outputPath: `${runId}/${testName}/post_test_rpc_calls/${filename}`,
      })
    }
  }
  return entries
}

// --- Tree building ---

function countFiles(node: TreeNode): number {
  if (node.type === 'file') return 1
  if (!node.children) return 0
  return node.children.reduce((sum, child) => sum + countFiles(child), 0)
}

function buildFileTree(
  generalEntries: FileEntry[],
  tests: Record<string, TestEntry>,
  runId: string,
  postTestRPCCalls: PostTestRPCCallConfig[],
): TreeNode[] {
  const nodes: TreeNode[] = []

  // Root-level files
  for (const entry of generalEntries) {
    nodes.push({
      id: entry.path,
      name: entry.filename,
      type: 'file',
      entry,
      depth: 0,
    })
  }

  const dumpCalls = postTestRPCCalls.filter((c) => c.dump?.enabled && c.dump.filename)

  // Test directories
  for (const [testName, testEntry] of Object.entries(tests)) {
    const children: TreeNode[] = []
    const steps = getTestSteps(testEntry)

    // Stats and response files per step
    for (const step of steps) {
      for (const suffix of ['response', 'result-aggregated.json', 'result-details.json']) {
        const filename = `${step}.${suffix}`
        const path = `runs/${runId}/${testName}/${filename}`
        children.push({
          id: path,
          name: filename,
          type: 'file',
          entry: {
            testName,
            filename,
            path,
            displayPath: filename,
            outputPath: `${runId}/${testName}/${filename}`,
          },
          depth: 1,
        })
      }
    }

    // Post-test RPC calls subdirectory
    if (dumpCalls.length > 0) {
      const dumpChildren: TreeNode[] = []
      for (const call of dumpCalls) {
        const filename = `${call.dump!.filename}.json`
        const path = `runs/${runId}/${testName}/post_test_rpc_calls/${filename}`
        dumpChildren.push({
          id: path,
          name: filename,
          type: 'file',
          entry: {
            testName,
            filename,
            path,
            displayPath: `post_test_rpc_calls/${filename}`,
            outputPath: `${runId}/${testName}/post_test_rpc_calls/${filename}`,
          },
          depth: 2,
        })
      }
      children.push({
        id: `dir:${testName}/post_test_rpc_calls`,
        name: 'post_test_rpc_calls',
        type: 'directory',
        children: dumpChildren,
        depth: 1,
      })
    }

    nodes.push({
      id: `dir:${testName}`,
      name: testName,
      type: 'directory',
      children,
      depth: 0,
    })
  }

  return nodes
}

function buildDumpsOnlyTree(
  tests: Record<string, TestEntry>,
  runId: string,
  postTestRPCCalls: PostTestRPCCallConfig[],
): TreeNode[] {
  const dumpCalls = postTestRPCCalls.filter((c) => c.dump?.enabled && c.dump.filename)
  if (dumpCalls.length === 0) return []

  const nodes: TreeNode[] = []
  for (const testName of Object.keys(tests)) {
    const children: TreeNode[] = []
    for (const call of dumpCalls) {
      const filename = `${call.dump!.filename}.json`
      const path = `runs/${runId}/${testName}/post_test_rpc_calls/${filename}`
      children.push({
        id: path,
        name: filename,
        type: 'file',
        entry: {
          testName,
          filename,
          path,
          displayPath: `post_test_rpc_calls/${filename}`,
          outputPath: `${runId}/${testName}/post_test_rpc_calls/${filename}`,
        },
        depth: 1,
      })
    }
    nodes.push({
      id: `dir:${testName}`,
      name: testName,
      type: 'directory',
      children,
      depth: 0,
    })
  }
  return nodes
}

function collectVisibleFileEntries(nodes: TreeNode[], expandedDirs: Set<string>): FileEntry[] {
  const entries: FileEntry[] = []
  for (const node of nodes) {
    if (node.type === 'file' && node.entry) {
      entries.push(node.entry)
    } else if (node.type === 'directory' && expandedDirs.has(node.id) && node.children) {
      entries.push(...collectVisibleFileEntries(node.children, expandedDirs))
    }
  }
  return entries
}

function flattenTree(nodes: TreeNode[], expandedDirs: Set<string>): TreeNode[] {
  const flat: TreeNode[] = []
  for (const node of nodes) {
    flat.push(node)
    if (node.type === 'directory' && expandedDirs.has(node.id) && node.children) {
      flat.push(...flattenTree(node.children, expandedDirs))
    }
  }
  return flat
}

const ROW_HEIGHT = 32

// --- Tree components ---

function FileTreeRow({
  node,
  runId,
  isExpanded,
  onToggleDir,
  headResultMap,
}: {
  node: TreeNode
  runId: string
  isExpanded?: boolean
  onToggleDir: (id: string) => void
  headResultMap: Map<string, HeadResult>
}) {
  if (node.type === 'directory') {
    const fileCount = countFiles(node)

    return (
      <button
        onClick={() => onToggleDir(node.id)}
        className="flex w-full cursor-pointer items-center gap-2 text-left text-xs/5 hover:bg-gray-50 dark:hover:bg-gray-700/50"
        style={{ paddingLeft: node.depth * 20 + 8, height: ROW_HEIGHT }}
      >
        <ChevronRight
          className={clsx('size-3.5 shrink-0 text-gray-400 transition-transform', isExpanded && 'rotate-90')}
        />
        {isExpanded ? (
          <FolderOpen className="size-4 shrink-0 text-amber-500 dark:text-amber-400" />
        ) : (
          <Folder className="size-4 shrink-0 text-amber-500 dark:text-amber-400" />
        )}
        <span className="min-w-0 truncate font-medium text-gray-900 dark:text-gray-100">{node.name}</span>
        <span className="mr-3 ml-auto shrink-0 rounded-full bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-500 dark:bg-gray-700 dark:text-gray-400">
          {fileCount}
        </span>
      </button>
    )
  }

  // File node
  const entry = node.entry!
  const headResult = headResultMap.get(entry.path)
  const isChecked = !!headResult
  const isAvailable = headResult?.exists ?? false
  const isJson = entry.filename.endsWith('.json')
  const FileIcon = isJson ? FileText : File

  return (
    <div
      className={clsx(
        'flex items-center gap-2 text-xs/5',
        isChecked && !isAvailable && 'opacity-50',
      )}
      style={{ paddingLeft: node.depth * 20 + 8, height: ROW_HEIGHT }}
    >
      {/* Spacer to align with directory chevron */}
      <span className="size-3.5 shrink-0" />
      <FileIcon className="size-4 shrink-0 text-gray-400 dark:text-gray-500" />
      <Link
        to="/runs/$runId/fileviewer"
        params={{ runId }}
        search={{ file: entry.path.replace(`runs/${runId}/`, '') }}
        target="_blank"
        className="min-w-0 truncate font-mono text-gray-900 hover:text-blue-600 dark:text-gray-100 dark:hover:text-blue-400"
      >
        {node.name}
        {isChecked && !isAvailable && (
          <span className="ml-2 rounded-full bg-yellow-100 px-1.5 py-0.5 font-sans text-xs font-medium text-yellow-700 dark:bg-yellow-900/50 dark:text-yellow-300">
            Unavailable
          </span>
        )}
      </Link>
      <span className="mr-3 ml-auto flex shrink-0 items-center gap-2">
        <span className="w-16 text-right text-gray-500 dark:text-gray-400">
          {!isChecked ? (
            <span className="inline-block size-3 animate-pulse rounded-full bg-gray-200 dark:bg-gray-600" />
          ) : isAvailable && headResult.size != null ? (
            formatBytes(headResult.size)
          ) : (
            '-'
          )}
        </span>
        {isAvailable && headResult ? (
          <a
            href={headResult.url}
            download={entry.filename}
            className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
            title="Download"
          >
            <Download className="size-4" />
          </a>
        ) : (
          <span className="size-4" />
        )}
      </span>
    </div>
  )
}

function FileTree({
  flatRows,
  runId,
  expandedDirs,
  onToggleDir,
  headResultMap,
}: {
  flatRows: TreeNode[]
  runId: string
  expandedDirs: Set<string>
  onToggleDir: (id: string) => void
  headResultMap: Map<string, HeadResult>
}) {
  const scrollRef = useRef<HTMLDivElement>(null)

  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: flatRows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 10,
  })

  // Shrink to content when rows fit, otherwise cap at 600px
  const totalHeight = flatRows.length * ROW_HEIGHT
  const maxHeight = 600
  const containerHeight = Math.min(totalHeight, maxHeight)

  return (
    <div className="flex flex-col gap-2">
      <div
        ref={scrollRef}
        className="overflow-auto"
        style={{ height: containerHeight }}
      >
        <div
          className="relative w-full"
          style={{ height: virtualizer.getTotalSize() }}
        >
          {virtualizer.getVirtualItems().map((virtualRow) => {
            const node = flatRows[virtualRow.index]
            return (
              <div
                key={node.id}
                className="absolute left-0 top-0 w-full border-b border-gray-100 last:border-0 dark:border-gray-700/50"
                style={{ height: virtualRow.size, transform: `translateY(${virtualRow.start}px)` }}
              >
                <FileTreeRow
                  node={node}
                  runId={runId}
                  isExpanded={node.type === 'directory' ? expandedDirs.has(node.id) : undefined}
                  onToggleDir={onToggleDir}
                  headResultMap={headResultMap}
                />
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// --- Category entry map for download modal ---

interface CategoryInfo {
  key: string
  label: string
  entries: FileEntry[]
}

// --- FilesPanel ---

export function FilesPanel({ runId, tests, postTestRPCCalls, showDownloadList, downloadFormat, onShowDownloadListChange, onDownloadFormatChange }: FilesPanelProps) {
  const [expanded, setExpanded] = useState(showDownloadList)
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(() => new Set())
  const [fileFilter, setFileFilter] = useState<'all' | 'dumps'>('all')
  const [excludedCategories, setExcludedCategories] = useState<Set<string>>(() => new Set())

  const testNames = useMemo(() => Object.keys(tests), [tests])

  const generalEntries = useMemo(() => buildGeneralEntries(runId), [runId])
  const testStatsEntries = useMemo(() => buildTestStatsEntries(runId, tests), [runId, tests])
  const testResponsesEntries = useMemo(() => buildTestResponsesEntries(runId, tests), [runId, tests])
  const postTestDumpEntries = useMemo(
    () => buildPostTestDumpEntries(runId, testNames, postTestRPCCalls ?? []),
    [runId, testNames, postTestRPCCalls],
  )

  const hasPostTestDumps = postTestDumpEntries.length > 0

  // Build file trees
  const allTree = useMemo(
    () => buildFileTree(generalEntries, tests, runId, postTestRPCCalls ?? []),
    [generalEntries, tests, runId, postTestRPCCalls],
  )
  const dumpsTree = useMemo(
    () => buildDumpsOnlyTree(tests, runId, postTestRPCCalls ?? []),
    [tests, runId, postTestRPCCalls],
  )

  const tree = fileFilter === 'dumps' ? dumpsTree : allTree

  // Flatten tree into a flat row list for virtualization
  const flatRows = useMemo(
    () => flattenTree(tree, expandedDirs),
    [tree, expandedDirs],
  )

  // Collect file entries that are currently visible (in expanded directories)
  const visibleFileEntries = useMemo(
    () => collectVisibleFileEntries(tree, expandedDirs),
    [tree, expandedDirs],
  )

  // HEAD requests only for visible files
  const headQueries = useQueries({
    queries: visibleFileEntries.map((entry) => ({
      queryKey: ['file-panel', entry.path],
      queryFn: () => fetchHead(entry.path),
      staleTime: Infinity,
    })),
  })

  const headResultMap = useMemo(() => {
    const map = new Map<string, HeadResult>()
    visibleFileEntries.forEach((entry, i) => {
      const data = headQueries[i]?.data
      if (data) {
        map.set(entry.path, data)
      }
    })
    return map
  }, [visibleFileEntries, headQueries])

  const toggleDir = useCallback((id: string) => {
    setExpandedDirs((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }, [])

  // Categories for download modal
  const categories: CategoryInfo[] = useMemo(() => {
    const result: CategoryInfo[] = [
      { key: 'general', label: 'General', entries: generalEntries },
      { key: 'test-stats', label: 'Stats', entries: testStatsEntries },
      { key: 'test-responses', label: 'Responses', entries: testResponsesEntries },
    ]
    if (hasPostTestDumps) {
      result.push({ key: 'post-test-rpc-dumps', label: 'Post-Test RPC Dumps', entries: postTestDumpEntries })
    }
    return result
  }, [generalEntries, testStatsEntries, testResponsesEntries, postTestDumpEntries, hasPostTestDumps])

  const toggleCategory = useCallback((key: string) => {
    setExcludedCategories((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }, [])

  const downloadEntries = useMemo(() => {
    const entries: FileEntry[] = []
    for (const cat of categories) {
      if (!excludedCategories.has(cat.key)) {
        entries.push(...cat.entries)
      }
    }
    return entries
  }, [categories, excludedCategories])

  const { data: runtimeConfig } = useQuery({
    queryKey: ['runtime-config'],
    queryFn: loadRuntimeConfig,
    staleTime: Infinity,
  })

  const downloadListText = useMemo(() => {
    if (!runtimeConfig || downloadEntries.length === 0) return ''
    return downloadEntries.map((e) => {
      const url = getDataUrl(e.path, runtimeConfig)
      return downloadFormat === 'urls'
        ? toAbsoluteUrl(url)
        : `curl -fsSL --create-dirs -o '${e.outputPath}' '${toAbsoluteUrl(url)}'`
    }).join('\n')
  }, [downloadEntries, downloadFormat, runtimeConfig])

  const handleDownloadFile = useCallback(() => {
    if (!downloadListText) return
    const isCurl = downloadFormat === 'curl'
    const content = isCurl ? `#!/bin/sh\n${downloadListText}` : downloadListText
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = isCurl ? `${runId}.sh` : `${runId}.txt`
    a.click()
    URL.revokeObjectURL(url)
  }, [downloadListText, downloadFormat, runId])

  const totalEntries = generalEntries.length + testStatsEntries.length + testResponsesEntries.length + postTestDumpEntries.length
  const summary = `${totalEntries} file${totalEntries !== 1 ? 's' : ''}`

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <div
        onClick={() => setExpanded(!expanded)}
        className="flex cursor-pointer items-center gap-3 border-b border-gray-200 px-4 py-3 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="flex shrink-0 items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          <FolderOpen className="size-4 text-gray-400 dark:text-gray-500" />
          Generated Files
        </h3>
        <div className="ml-auto flex min-w-0 items-center gap-3">
          <span className="truncate text-xs/5 text-gray-500 dark:text-gray-400">{summary}</span>
          <button
            onClick={(e) => { e.stopPropagation(); onShowDownloadListChange(true) }}
            className="flex shrink-0 items-center gap-1.5 rounded-xs border border-gray-300 px-2 py-1 text-xs/5 font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
          >
            <List className="size-3.5" />
            Download list
          </button>
          <ChevronDown className={clsx('size-5 shrink-0 text-gray-500 transition-transform', expanded && 'rotate-180')} />
        </div>
      </div>
      {expanded && (
        <div className="p-4">
          {hasPostTestDumps && (
            <div className="mb-3 flex items-center gap-1 rounded-xs bg-gray-100 p-0.5 dark:bg-gray-700" style={{ width: 'fit-content' }}>
              {([{ key: 'all', label: 'All' }, { key: 'dumps', label: 'Post-Test RPC Dumps' }] as const).map(({ key, label }) => (
                <button
                  key={key}
                  onClick={() => setFileFilter(key)}
                  className={clsx(
                    'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                    fileFilter === key
                      ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                      : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
          <FileTree
            flatRows={flatRows}
            runId={runId}
            expandedDirs={expandedDirs}
            onToggleDir={toggleDir}
            headResultMap={headResultMap}
          />
        </div>
      )}
      <Modal isOpen={showDownloadList} onClose={() => onShowDownloadListChange(false)} title="Download List" className="max-w-3xl">
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-1 rounded-xs bg-gray-100 p-0.5 dark:bg-gray-700">
                {([{ key: 'curl', label: 'curl' }, { key: 'urls', label: 'Plain URLs' }] as const).map(({ key, label }) => (
                  <button
                    key={key}
                    onClick={() => onDownloadFormatChange(key)}
                    className={clsx(
                      'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                      downloadFormat === key
                        ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                        : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <span className="text-xs/5 text-gray-500 dark:text-gray-400">
                {downloadEntries.length} file{downloadEntries.length !== 1 ? 's' : ''}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <CopyButton text={downloadListText} />
              <button
                onClick={handleDownloadFile}
                disabled={!downloadListText}
                className="shrink-0 text-gray-400 hover:text-gray-600 disabled:opacity-50 dark:hover:text-gray-200"
                title="Download as file"
              >
                <Download className="size-4" />
              </button>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Include:</span>
            {categories.map((cat) => (
              <button
                key={cat.key}
                onClick={() => toggleCategory(cat.key)}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  !excludedCategories.has(cat.key)
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'bg-gray-100 text-gray-600 hover:text-gray-900 dark:bg-gray-700 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                {cat.label} ({cat.entries.length})
              </button>
            ))}
          </div>
          <pre className="max-h-96 overflow-auto rounded-xs bg-gray-100 p-3 font-mono text-xs/5 text-gray-900 select-all dark:bg-gray-900 dark:text-gray-100">
            {downloadListText || 'No files selected'}
          </pre>
          {downloadFormat === 'curl' && downloadListText && (
            <p className="text-xs/5 text-gray-500 dark:text-gray-400">
              Download{' '}
              <button onClick={handleDownloadFile} className="text-blue-600 underline hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300">
                {runId}.sh
              </button>
              . Run with: <code className="rounded-xs bg-gray-100 px-1 py-0.5 font-mono dark:bg-gray-900">chmod +x {runId}.sh && ./{runId}.sh</code>{' '}<CopyButton text={`chmod +x ${runId}.sh && ./${runId}.sh`} />
            </p>
          )}
        </div>
      </Modal>
    </div>
  )
}
