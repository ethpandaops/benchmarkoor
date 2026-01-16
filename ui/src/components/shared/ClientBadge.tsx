import clsx from 'clsx'
import { getClientColors } from '@/utils/client-colors'

interface ClientBadgeProps {
  client: string
  className?: string
}

function capitalizeFirst(str: string): string {
  if (!str) return str
  return str.charAt(0).toUpperCase() + str.slice(1)
}

export function ClientBadge({ client, className }: ClientBadgeProps) {
  const colors = getClientColors(client)
  const logoPath = `/img/clients/${client}.jpg`

  return (
    <span
      className={clsx(
        'inline-flex w-28 items-center gap-1.5 rounded-sm px-2.5 py-0.5 text-xs/5 font-medium',
        colors.bg,
        colors.text,
        colors.darkBg,
        colors.darkText,
        className,
      )}
    >
      <img src={logoPath} alt={`${client} logo`} className="size-4 rounded-full object-cover" />
      {capitalizeFirst(client)}
    </span>
  )
}
