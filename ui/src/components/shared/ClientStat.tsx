import clsx from 'clsx'
import { getClientColors } from '@/utils/client-colors'

interface ClientStatProps {
  client: string
  runId: string
}

function capitalizeFirst(str: string): string {
  if (!str) return str
  return str.charAt(0).toUpperCase() + str.slice(1)
}

export function ClientStat({ client, runId }: ClientStatProps) {
  const colors = getClientColors(client)
  const logoPath = `/img/clients/${client}.jpg`

  return (
    <div
      className={clsx(
        'flex flex-col gap-2 rounded-sm p-4 shadow-xs',
        colors.bg,
        colors.darkBg,
      )}
    >
      <div className="flex items-center gap-3">
        <img
          src={logoPath}
          alt={`${client} logo`}
          className="size-12 rounded-sm object-cover"
        />
        <span className={clsx('text-2xl/8 font-semibold', colors.text, colors.darkText)}>
          {capitalizeFirst(client)}
        </span>
      </div>
      <div>
        <p className={clsx('text-xs/5', colors.text, colors.darkText, 'opacity-70')}>
          Run id
        </p>
        <p className={clsx('text-xs/5', colors.text, colors.darkText)}>
          {runId}
        </p>
      </div>
    </div>
  )
}
