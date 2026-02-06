import clsx from 'clsx'
import { Loader2 } from 'lucide-react'

interface SpinnerProps {
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

const sizeClasses = {
  sm: 'size-4',
  md: 'size-6',
  lg: 'size-8',
}

export function Spinner({ size = 'md', className }: SpinnerProps) {
  return (
    <Loader2 className={clsx('animate-spin text-blue-600 dark:text-blue-400', sizeClasses[size], className)} />
  )
}

export function LoadingState({ message = 'Loading...' }: { message?: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-12">
      <Spinner size="lg" />
      <p className="text-sm/6 text-gray-500 dark:text-gray-400">{message}</p>
    </div>
  )
}
