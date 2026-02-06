import { Tab, TabGroup, TabList, TabPanel, TabPanels } from '@headlessui/react'
import clsx from 'clsx'
import { BarChart3, Database, BarChart2 } from 'lucide-react'
import type { DashboardTab } from '../types'

interface DashboardTabsProps {
  activeTab: DashboardTab
  onTabChange: (tab: DashboardTab) => void
  children: React.ReactNode
}

const TABS: { value: DashboardTab; label: string; icon: React.ReactNode }[] = [
  {
    value: 'overview',
    label: 'Overview',
    icon: <BarChart3 className="size-4" />,
  },
  {
    value: 'cache',
    label: 'Cache',
    icon: <Database className="size-4" />,
  },
  {
    value: 'distribution',
    label: 'Distribution',
    icon: <BarChart2 className="size-4" />,
  },
]

export function DashboardTabs({ activeTab, onTabChange, children }: DashboardTabsProps) {
  const tabIndex = Math.max(0, TABS.findIndex((t) => t.value === activeTab))

  const handleTabChange = (index: number) => {
    onTabChange(TABS[index].value)
  }

  return (
    <TabGroup key={activeTab} selectedIndex={tabIndex} onChange={handleTabChange}>
      <TabList className="flex border-b border-gray-200 dark:border-gray-700">
        {TABS.map((tab) => (
          <Tab
            key={tab.value}
            className={({ selected }) =>
              clsx(
                'flex cursor-pointer items-center gap-1.5 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors focus:outline-hidden',
                selected
                  ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                  : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'
              )
            }
          >
            {tab.icon}
            {tab.label}
          </Tab>
        ))}
      </TabList>
      <TabPanels className="p-4">
        {children}
      </TabPanels>
    </TabGroup>
  )
}

export { TabPanel }
