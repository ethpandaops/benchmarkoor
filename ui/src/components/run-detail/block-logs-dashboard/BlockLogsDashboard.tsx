import { useEffect, useMemo, useState } from 'react'
import { TabPanel } from '@headlessui/react'
import type { BlockLogs, SuiteTest } from '@/api/types'
import { useDashboardState } from './hooks/useDashboardState'
import { useProcessedData } from './hooks/useProcessedData'
import { DashboardFilters } from './components/DashboardFilters'
import { DashboardTabs } from './components/DashboardTabs'
import { BlockLogsTable } from './components/BlockLogsTable'
import { OverviewTab } from './components/OverviewTab'
import { CompareTab } from './components/CompareTab'
import { CacheTab } from './components/CacheTab'
import { DistributionTab } from './components/DistributionTab'

interface BlockLogsDashboardProps {
  blockLogs: BlockLogs | null | undefined
  runId: string
  suiteTests?: SuiteTest[]
  onTestClick?: (testName: string) => void
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

export function BlockLogsDashboard({ blockLogs, runId, suiteTests, onTestClick }: BlockLogsDashboardProps) {
  const isDark = useDarkMode()
  const { state, updateState, toggleTestSelection, clearSelection } = useDashboardState(runId)

  // Build execution order map from suite tests
  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((test, index) => [test.name, index + 1]))
  }, [suiteTests])

  const { data, stats } = useProcessedData(blockLogs, state, executionOrder)

  // Don't render if no block logs data
  if (!blockLogs || Object.keys(blockLogs).length === 0) {
    return null
  }

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <div className="flex items-center gap-3">
          <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Block Logs Analysis</h3>
          <span className="rounded-full bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/50 dark:text-purple-300">
            {Object.keys(blockLogs).length} tests
          </span>
        </div>
        {stats && (
          <div className="text-xs text-gray-500 dark:text-gray-400">
            {stats.minThroughput.toFixed(1)} - {stats.maxThroughput.toFixed(1)} MGas/s
          </div>
        )}
      </div>

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
          <CompareTab
            data={data}
            selectedTests={state.selectedTests}
            isDark={isDark}
            onRemoveTest={toggleTestSelection}
            onClearSelection={clearSelection}
          />
        </TabPanel>
        <TabPanel>
          <CacheTab
            data={data}
            isDark={isDark}
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
        onToggleSelection={toggleTestSelection}
      />
    </div>
  )
}
