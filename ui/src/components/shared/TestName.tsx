import { useState } from 'react'
import clsx from 'clsx'
import { Check, Copy } from 'lucide-react'
import { parseEESTName } from '@/utils/eestName'
import { useNameDisplayMode } from '@/hooks/useNameDisplayMode'

interface TestNameProps {
  /** Raw test name. Always preserved as the source of truth. */
  name: string
  /**
   * - `full`: file path prefix + function + all chips (tables, modals).
   * - `compact`: one-line `<fn> · <opcode> · <gas>` (heatmap row labels).
   */
  variant?: 'full' | 'compact'
  /** Show a copy-to-clipboard affordance (default: false). */
  showCopy?: boolean
  /**
   * Render the raw name on a muted second line. Only visible in decomposed
   * mode (in raw mode the primary line already shows the raw name).
   */
  showRawBelow?: boolean
  className?: string
}

function CopyButton({ value, className }: { value: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  const onCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    e.preventDefault()
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      // ignore
    }
  }
  return (
    <button
      type="button"
      onClick={onCopy}
      className={clsx(
        'inline-flex shrink-0 items-center justify-center rounded-xs p-0.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-700 dark:hover:text-gray-200',
        className,
      )}
      title={copied ? 'Copied!' : 'Copy raw name'}
      aria-label="Copy raw name"
    >
      {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
    </button>
  )
}

const chipBase =
  'inline-flex max-w-full items-center gap-1 break-all rounded-xs px-1.5 py-0 font-mono text-[11px]/5 font-medium ring-1 ring-inset'
const chipNeutral =
  'bg-gray-100 text-gray-700 ring-gray-200 dark:bg-gray-700 dark:text-gray-200 dark:ring-gray-600'
const chipAccent =
  'bg-indigo-50 text-indigo-700 ring-indigo-200 dark:bg-indigo-950/50 dark:text-indigo-300 dark:ring-indigo-800'
const chipGas =
  'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950/50 dark:text-emerald-300 dark:ring-emerald-800'

export function TestName({ name, variant = 'full', showCopy = false, showRawBelow = false, className }: TestNameProps) {
  const { mode } = useNameDisplayMode()
  const parsed = mode === 'decomposed' ? parseEESTName(name) : null

  if (mode === 'raw' || !parsed?.isEEST) {
    return (
      <span className={clsx('inline-flex min-w-0 items-baseline gap-1.5', className)} title={name}>
        <code className={clsx('truncate font-mono text-xs/5 text-gray-700 dark:text-gray-300')}>
          {name}
        </code>
        {showCopy && <CopyButton value={name} />}
      </span>
    )
  }

  if (variant === 'compact') {
    return (
      <span className={clsx('inline-flex min-w-0 items-baseline gap-1.5', className)} title={name}>
        <span className="truncate text-sm/5 text-gray-900 dark:text-gray-100">{parsed.short}</span>
        {showCopy && <CopyButton value={name} />}
      </span>
    )
  }

  const hasChips =
    parsed.opcode != null ||
    parsed.benchmark != null ||
    parsed.fork != null ||
    parsed.params.length > 0 ||
    parsed.labels.length > 0

  return (
    <span className={clsx('flex min-w-0 flex-col gap-1', className)} title={name}>
      <span className="inline-flex min-w-0 items-baseline gap-1">
        {parsed.file && (
          <span className="truncate font-mono text-xs/5 text-gray-500 dark:text-gray-400">{parsed.file}</span>
        )}
        {parsed.file && <span className="text-xs/5 text-gray-400 dark:text-gray-500">›</span>}
        <span className="truncate text-sm/5 font-medium text-gray-900 dark:text-gray-100">
          {parsed.fn}
        </span>
        {showCopy && <CopyButton value={name} />}
      </span>
      {hasChips && (
        <span className="inline-flex flex-wrap items-baseline gap-1">
          {parsed.benchmark && (
            <span className={clsx(chipBase, chipGas)}>{parsed.benchmark}</span>
          )}
          {parsed.opcode && (
            <span className={clsx(chipBase, chipAccent)}>{parsed.opcode}</span>
          )}
          {parsed.fork && (
            <span className={clsx(chipBase, chipNeutral)}>fork:{parsed.fork}</span>
          )}
          {parsed.params.map(({ key, value }) => (
            <span key={`${key}=${value}`} className={clsx(chipBase, chipNeutral)}>
              {key}:{value}
            </span>
          ))}
          {parsed.labels.map((label) => (
            <span key={label} className={clsx(chipBase, chipNeutral)}>
              {label}
            </span>
          ))}
        </span>
      )}
      {showRawBelow && (
        <code className="break-all font-mono text-[11px]/4 text-gray-400 dark:text-gray-500">{name}</code>
      )}
    </span>
  )
}
