import { Badge } from '@/components/shared/Badge'

// MempoolBadge marks a run or suite of the mempool executor.
export function MempoolBadge({ className }: { className?: string }) {
  return (
    <span title="Mempool run: txpool scenarios, no MGas/s">
      <Badge variant="info" className={className}>
        mempool
      </Badge>
    </span>
  )
}
