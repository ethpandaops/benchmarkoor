import { useState, useEffect } from 'react'
import { WifiOff } from 'lucide-react'
import { useAuth } from '@/hooks/useAuth'
import { onApiStatusChange } from '@/api/api-status-events'

export function ApiDownBanner() {
  const { isApiEnabled } = useAuth()
  const [isDown, setIsDown] = useState(false)

  useEffect(() => onApiStatusChange(setIsDown), [])

  if (!isApiEnabled || !isDown) return null

  return (
    <div className="border-b border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-950">
      <div className="mx-auto flex max-w-7xl items-center justify-center gap-3 px-4 py-2 text-sm/5 text-amber-800 dark:text-amber-200">
        <WifiOff className="size-4 shrink-0" />
        <span>
          Unable to reach the API server. Data may be stale. The banner will
          dismiss automatically once connectivity is restored.
        </span>
      </div>
    </div>
  )
}
