import clsx from 'clsx'
import { getBaseClient, getClientColors } from '@/utils/client-colors'

interface ClientBadgeProps {
  client: string
  className?: string
  hideLabel?: boolean
}

function capitalizeFirst(str: string): string {
  if (!str) return str
  return str.charAt(0).toUpperCase() + str.slice(1)
}

export function ClientBadge({ client, className, hideLabel = false }: ClientBadgeProps) {
  const base = getBaseClient(client)
  const colors = getClientColors(client)
  const logoPath = `/img/clients/${base}.jpg`

  return (
    <span
      className={clsx(
        'inline-flex items-center rounded-xs text-xs/5 font-medium',
        hideLabel ? 'p-0.5' : 'w-28 gap-1.5 px-2.5 py-0.5',
        colors.bg,
        colors.text,
        colors.darkBg,
        colors.darkText,
        className,
      )}
    >
      <img src={logoPath} alt={`${client} logo`} className="size-4 rounded-full object-cover" />
      {!hideLabel && capitalizeFirst(client)}
    </span>
  )
}
