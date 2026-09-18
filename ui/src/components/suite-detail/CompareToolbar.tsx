import clsx from 'clsx'
import { SquareStack, GitCompareArrows, Layers } from 'lucide-react'
import { MIN_COMPARE_RUNS } from '@/components/compare/constants'

interface CompareToolbarProps {
  /** Whether the manual run selection mode is active. */
  compareMode: boolean
  onToggleCompareMode: () => void
  /** Compare page URL for the latest successful run per client, or undefined when too few runs exist. */
  latestCompareUrl?: string
  /** Compare page URL for the averaged groups, or undefined when the suite has no group-by. */
  groupCompareUrl?: string
}

const BUTTON_BASE = 'flex items-center gap-1.5 rounded-xs px-2 py-1 text-xs/5 font-medium shadow-xs ring-1 ring-inset transition-colors'
const BUTTON_IDLE = 'cursor-pointer bg-white text-gray-600 ring-gray-300 hover:bg-gray-50 hover:text-gray-900 dark:bg-gray-800 dark:text-gray-300 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-100'
const BUTTON_ACTIVE = 'cursor-pointer bg-blue-600 text-white ring-blue-600 hover:bg-blue-700 hover:ring-blue-700'
const BUTTON_DISABLED = 'cursor-not-allowed bg-white text-gray-500 opacity-50 ring-gray-300 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600'

/**
 * The compare actions shared by the runs heatmap header and the run list toolbar.
 * Labels hide below the sm breakpoint; the title attributes keep the tooltip.
 */
export function CompareToolbar({ compareMode, onToggleCompareMode, latestCompareUrl, groupCompareUrl }: CompareToolbarProps) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <button
        onClick={onToggleCompareMode}
        className={clsx(BUTTON_BASE, compareMode ? BUTTON_ACTIVE : BUTTON_IDLE)}
        title="Pick runs from the list to compare"
      >
        <SquareStack className="size-3.5" />
        <span className="hidden sm:inline">{compareMode ? 'Done selecting' : 'Select runs'}</span>
      </button>
      <span className="ml-1.5 text-xs/5 text-gray-500 dark:text-gray-400">Compare:</span>
      {latestCompareUrl ? (
        <a href={latestCompareUrl} className={clsx(BUTTON_BASE, BUTTON_IDLE)} title="Compare the latest successful run of each client">
          <GitCompareArrows className="size-3.5" />
          <span className="hidden sm:inline">Latest per client</span>
        </a>
      ) : (
        <button
          disabled
          className={clsx(BUTTON_BASE, BUTTON_DISABLED)}
          title={`Needs successful runs from at least ${MIN_COMPARE_RUNS} clients`}
        >
          <GitCompareArrows className="size-3.5" />
          <span className="hidden sm:inline">Latest per client</span>
        </button>
      )}
      {groupCompareUrl && (
        <a href={groupCompareUrl} className={clsx(BUTTON_BASE, BUTTON_IDLE)} title="Compare the averaged groups, one group per client">
          <Layers className="size-3.5" />
          <span className="hidden sm:inline">Averaged groups</span>
        </a>
      )}
    </div>
  )
}
