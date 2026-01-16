import { formatDuration } from '@/utils/format'

interface DurationProps {
  nanoseconds: number
  className?: string
}

export function Duration({ nanoseconds, className }: DurationProps) {
  return <span className={className}>{formatDuration(nanoseconds)}</span>
}
