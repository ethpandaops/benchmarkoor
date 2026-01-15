import clsx from 'clsx'
import { getClientColors } from '@/utils/client-colors'

interface ClientBadgeProps {
  client: string
  className?: string
}

export function ClientBadge({ client, className }: ClientBadgeProps) {
  const colors = getClientColors(client)

  return (
    <span
      className={clsx(
        'inline-flex items-center rounded-sm px-2.5 py-0.5 text-xs/5 font-medium',
        colors.bg,
        colors.text,
        colors.darkBg,
        colors.darkText,
        className,
      )}
    >
      {client}
    </span>
  )
}
