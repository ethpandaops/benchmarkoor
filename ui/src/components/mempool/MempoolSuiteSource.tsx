import type { MempoolSource } from '@/api/types'
import { Card } from '@/components/shared/Card'
import { formatNumber } from '@/utils/format'
import { MempoolBadge } from './MempoolBadge'
import { formatParam } from './mempoolUtils'

// MempoolSuiteSource describes the generated scenarios of a mempool suite.
// It takes the place of the git / EEST source of a replay suite.
export function MempoolSuiteSource({ source }: { source?: MempoolSource }) {
  if (!source) {
    return (
      <Card title="Source" headerExtra={<MempoolBadge />}>
        <p className="text-sm/6 text-gray-500 dark:text-gray-400">This mempool suite has no source description.</p>
      </Card>
    )
  }

  const fields: [string, string][] = [
    ['Fork', source.fork],
    ['Chain ID', String(source.chain_id)],
    ['Seed', source.seed],
    ['Accounts', formatNumber(source.accounts)],
    ['Gas limit', formatNumber(source.gas_limit)],
  ]

  return (
    <Card title="Source" headerExtra={<MempoolBadge />}>
      <div className="flex flex-col gap-4">
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-5">
          {fields.map(([label, value]) => (
            <div key={label}>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">{label}</dt>
              <dd className="mt-1 break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">{value}</dd>
            </div>
          ))}
        </dl>
        <div className="flex flex-col gap-2 border-t border-gray-200 pt-4 dark:border-gray-700">
          <span className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Scenarios ({source.scenarios?.length ?? 0})</span>
          <ul className="flex flex-col gap-1.5">
            {(source.scenarios ?? []).map((s) => (
              <li key={s.name} className="flex flex-wrap items-center gap-1.5">
                <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">{s.name}</span>
                {Object.entries(s.params ?? {}).map(([k, v]) => (
                  <code
                    key={k}
                    className="rounded-xs bg-gray-100 px-1.5 py-0.5 font-mono text-xs/5 text-gray-700 dark:bg-gray-700 dark:text-gray-300"
                  >
                    {k}={formatParam(v)}
                  </code>
                ))}
              </li>
            ))}
          </ul>
        </div>
      </div>
    </Card>
  )
}
