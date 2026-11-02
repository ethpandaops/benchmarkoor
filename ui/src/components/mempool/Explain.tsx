import { Info } from 'lucide-react'
import type { MempoolCheckExplanation } from '@/api/types'
import { EXPLAIN_LABELS, type ExplainKind } from './mempoolUtils'

const BUTTON_TITLE: Record<ExplainKind, string> = {
  check: 'What this checks, why, and which results are OK',
  observation: 'What this records, why, and how to read it',
  metric: 'What this measures, why, and how to read it',
}

// ExplainButton toggles an explanation.
export function ExplainButton({ open, onClick, kind = 'check', label = true }: { open: boolean; onClick: () => void; kind?: ExplainKind; label?: boolean }) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onClick()
      }}
      aria-expanded={open}
      aria-label={open ? 'Hide the explanation' : BUTTON_TITLE[kind]}
      title={open ? 'Hide the explanation' : BUTTON_TITLE[kind]}
      className="inline-flex shrink-0 items-center gap-1 self-start rounded-xs px-1 text-xs/5 text-blue-600 hover:bg-blue-50 hover:text-blue-700 dark:text-blue-400 dark:hover:bg-blue-900/30 dark:hover:text-blue-300"
    >
      <Info className="size-3.5" />
      {label && (open ? 'Hide' : 'Explain')}
    </button>
  )
}

// Explanation shows what a check tests (or a value records or measures),
// why it matters, and how to read the result.
export function Explanation({ explain, kind = 'check', title }: { explain: MempoolCheckExplanation; kind?: ExplainKind; title?: string }) {
  const [what, why, ok] = EXPLAIN_LABELS[kind]
  const rows: [string, string][] = [
    [what, explain.what],
    [why, explain.why],
    [ok, explain.ok],
  ]
  return (
    <div className="mt-1.5 flex max-w-4xl flex-col gap-1 rounded-xs bg-gray-50 px-3 py-2 text-xs/5 dark:bg-gray-900/40">
      {title && <span className="font-medium text-gray-900 dark:text-gray-100">{title}</span>}
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
        {rows.map(([label, text]) => (
          <div key={label} className="contents">
            <dt className="font-medium text-gray-500 dark:text-gray-400">{label}</dt>
            <dd className="text-gray-700 dark:text-gray-300">{text}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
