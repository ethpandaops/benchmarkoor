import { ChevronLeft, ChevronRight } from 'lucide-react'
import type { TestNavigation } from '@/hooks/useTestNavigation'

const buttonClass =
  'rounded-xs p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-30 disabled:hover:bg-transparent dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200'

/** Previous/next buttons and the position of the open test, for a modal header. */
export function TestNavControls({ navigation }: { navigation?: TestNavigation }) {
  if (!navigation || navigation.total < 2) return null
  return (
    <div className="flex items-center gap-1 text-xs/5 text-gray-500 dark:text-gray-400">
      <button type="button" onClick={navigation.onPrev} disabled={!navigation.onPrev} className={buttonClass} title="Previous test (←)">
        <ChevronLeft className="size-4" />
      </button>
      <span className="tabular-nums">
        {navigation.position} / {navigation.total}
      </span>
      <button type="button" onClick={navigation.onNext} disabled={!navigation.onNext} className={buttonClass} title="Next test (→)">
        <ChevronRight className="size-4" />
      </button>
    </div>
  )
}
