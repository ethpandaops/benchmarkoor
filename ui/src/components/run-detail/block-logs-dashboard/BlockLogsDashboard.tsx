import { useEffect, useMemo, useState } from 'react'
import { TabPanel } from '@headlessui/react'
import type { BlockLogs, SuiteTest } from '@/api/types'
import { useDashboardState } from './hooks/useDashboardState'
import { useProcessedData } from './hooks/useProcessedData'
import { DashboardFilters } from './components/DashboardFilters'
import { DashboardTabs } from './components/DashboardTabs'
import { BlockLogsTable } from './components/BlockLogsTable'
import { OverviewTab } from './components/OverviewTab'
import { CacheTab } from './components/CacheTab'
import { DistributionTab } from './components/DistributionTab'

interface BlockLogsDashboardProps {
  blockLogs: BlockLogs | null | undefined
  runId: string
  suiteTests?: SuiteTest[]
  onTestClick?: (testName: string) => void
  searchQuery?: string
  onSearchChange?: (query: string) => void
  fullscreen?: boolean
  onFullscreenChange?: (fullscreen: boolean) => void
}

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

function useFullscreen(
  externalFullscreen?: boolean,
  onFullscreenChange?: (fullscreen: boolean) => void
) {
  const [internalFullscreen, setInternalFullscreen] = useState(false)

  // Use external state if provided, otherwise use internal state
  const fullscreen = externalFullscreen ?? internalFullscreen
  const setFullscreen = onFullscreenChange ?? setInternalFullscreen

  useEffect(() => {
    if (!fullscreen) return
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false)
    }
    document.addEventListener('keydown', handleKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', handleKey)
      document.body.style.overflow = ''
    }
  }, [fullscreen, setFullscreen])

  return { fullscreen, setFullscreen }
}

export function BlockLogsDashboard({ blockLogs, runId, suiteTests, onTestClick, searchQuery = '', onSearchChange, fullscreen: externalFullscreen, onFullscreenChange }: BlockLogsDashboardProps) {
  const isDark = useDarkMode()
  const { fullscreen, setFullscreen } = useFullscreen(externalFullscreen, onFullscreenChange)
  const { state, updateState } = useDashboardState(runId)

  // Build execution order map from suite tests
  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((test, index) => [test.name, index + 1]))
  }, [suiteTests])

  const { data, stats } = useProcessedData(blockLogs, state, executionOrder, searchQuery)

  // Don't render if no block logs data
  if (!blockLogs || Object.keys(blockLogs).length === 0) {
    return null
  }

  const header = (
    <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
      <div className="flex items-center gap-3">
        <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Block Logs Analysis</h3>
        <span className="rounded-full bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/50 dark:text-purple-300">
          {Object.keys(blockLogs).length} tests
        </span>
      </div>
      <div className="flex items-center gap-2">
        {onSearchChange && (
          <input
            type="text"
            placeholder="Filter tests..."
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            className="rounded-xs border border-gray-300 bg-white px-3 py-1 text-sm placeholder-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500"
          />
        )}
        <button
          onClick={() => setFullscreen(!fullscreen)}
          className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
          title={fullscreen ? 'Exit fullscreen' : 'Fullscreen'}
        >
          {fullscreen ? (
            <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          ) : (
            <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5v-4m0 4h-4m4 0l-5-5" />
            </svg>
          )}
        </button>
      </div>
    </div>
  )

  const content = (
    <>
      {/* Filters */}
      <DashboardFilters
        state={state}
        stats={stats}
        onUpdate={updateState}
      />

      {/* Tabs */}
      <DashboardTabs
        activeTab={state.activeTab}
        onTabChange={(tab) => updateState({ activeTab: tab })}
      >
        <TabPanel>
          <OverviewTab
            data={data}
            stats={stats}
            isDark={isDark}
            useLogScale={state.useLogScale}
            onTestClick={onTestClick}
          />
        </TabPanel>
        <TabPanel>
          <CacheTab
            data={data}
            isDark={isDark}
            useLogScale={state.useLogScale}
          />
        </TabPanel>
        <TabPanel>
          <DistributionTab
            data={data}
            stats={stats}
            isDark={isDark}
            useLogScale={state.useLogScale}
          />
        </TabPanel>
      </DashboardTabs>

      {/* Data Table */}
      <BlockLogsTable
        data={data}
        state={state}
        onUpdate={updateState}
      />
    </>
  )

  return (
    <div className={
      fullscreen
        ? 'fixed inset-0 z-40 flex flex-col overflow-auto bg-white dark:bg-gray-900'
        : 'overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800'
    }>
      <div className={fullscreen ? 'sticky top-0 z-10 bg-white dark:bg-gray-900' : ''}>
        {header}
      </div>
      <div className={fullscreen ? 'flex-1' : ''}>
        {content}
      </div>
    </div>
  )
}
