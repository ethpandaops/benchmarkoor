import { useState } from 'react'
import clsx from 'clsx'
import { Check, Copy } from 'lucide-react'
import { parseEESTName } from '@/utils/eestName'
import { searchQueryContains } from '@/utils/eestNameFilter'
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
  /**
   * If provided, chips become buttons. Called with the search-query term
   * to toggle (e.g. `opcode:ORIGIN`, `gas:90M`, `mem_size:1024`).
   */
  onChipClick?: (term: string) => void
  /**
   * Current search query. Chips whose term is present here render with
   * an "active" style so users can see what's pinned.
   */
  activeQuery?: string
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
const chipActive =
  'bg-blue-500 text-white ring-blue-500 dark:bg-blue-500 dark:text-white dark:ring-blue-500'

function Chip({
  variant,
  label,
  term,
  onClick,
  active,
}: {
  variant: 'gas' | 'accent' | 'neutral'
  label: string
  term: string
  onClick?: (term: string) => void
  active?: boolean
}) {
  const variantClasses =
    active ? chipActive
      : variant === 'gas' ? chipGas
        : variant === 'accent' ? chipAccent
          : chipNeutral

  if (!onClick) {
    return <span className={clsx(chipBase, variantClasses)}>{label}</span>
  }
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        e.preventDefault()
        onClick(term)
      }}
      className={clsx(
        chipBase,
        variantClasses,
        'cursor-pointer transition-colors',
        !active && 'hover:bg-blue-100 hover:text-blue-700 hover:ring-blue-300 dark:hover:bg-blue-950/60 dark:hover:text-blue-300 dark:hover:ring-blue-700',
      )}
      title={active ? `Click to remove ${term} from filter` : `Click to filter by ${term}`}
    >
      {label}
    </button>
  )
}

export function TestName({ name, variant = 'full', showCopy = false, showRawBelow = false, onChipClick, activeQuery, className }: TestNameProps) {
  const { mode } = useNameDisplayMode()
  const parsed = mode === 'decomposed' ? parseEESTName(name) : null

  if (mode === 'raw' || !parsed?.isEEST) {
    return (
      <span className={clsx('flex min-w-0 items-baseline gap-1.5', className)} title={name}>
        <code className={clsx('min-w-0 flex-1 truncate font-mono text-xs/5 text-gray-700 dark:text-gray-300')}>
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
        {parsed.path && (
          <>
            <span className="truncate font-mono text-xs/5 text-gray-400 dark:text-gray-500">{parsed.path}</span>
            <span className="text-xs/5 text-gray-300 dark:text-gray-600">/</span>
          </>
        )}
        {parsed.file && (
          <span className="truncate font-mono text-xs/5 text-gray-500 dark:text-gray-400">{parsed.file}</span>
        )}
        {parsed.file && <span className="text-xs/5 text-gray-400 dark:text-gray-500">›</span>}
        <span className="truncate text-sm/5 font-medium text-gray-900 dark:text-gray-100">
          {parsed.fn}
        </span>
        {showCopy && !showRawBelow && <CopyButton value={name} />}
      </span>
      {hasChips && (
        <span className="inline-flex flex-wrap items-baseline gap-1">
          {parsed.benchmark && (
            <Chip
              variant="gas"
              label={parsed.benchmark}
              term={`gas=${parsed.benchmark}`}
              onClick={onChipClick}
              active={!!activeQuery && searchQueryContains(activeQuery, `gas=${parsed.benchmark}`)}
            />
          )}
          {parsed.opcode && (
            <Chip
              variant="accent"
              label={parsed.opcode}
              term={`opcode=${parsed.opcode}`}
              onClick={onChipClick}
              active={!!activeQuery && searchQueryContains(activeQuery, `opcode=${parsed.opcode}`)}
            />
          )}
          {parsed.fork && (
            <Chip
              variant="neutral"
              label={`fork:${parsed.fork}`}
              term={`fork=${parsed.fork}`}
              onClick={onChipClick}
              active={!!activeQuery && searchQueryContains(activeQuery, `fork=${parsed.fork}`)}
            />
          )}
          {parsed.params.map(({ key, value }) => {
            const term = `${key}=${value}`
            return (
              <Chip
                key={term}
                variant="neutral"
                label={`${key}:${value}`}
                term={term}
                onClick={onChipClick}
                active={!!activeQuery && searchQueryContains(activeQuery, term)}
              />
            )
          })}
          {parsed.labels.map((label) => {
            const term = `label=${label}`
            return (
              <Chip
                key={label}
                variant="neutral"
                label={label}
                term={term}
                onClick={onChipClick}
                active={!!activeQuery && searchQueryContains(activeQuery, term)}
              />
            )
          })}
        </span>
      )}
      {showRawBelow && (
        <span className="mt-3 flex flex-col gap-0.5">
          <span className="text-[10px]/4 font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">Test name:</span>
          <span className="flex items-start gap-1.5">
            <code className="min-w-0 break-all font-mono text-[11px]/4 text-gray-400 dark:text-gray-500">{name}</code>
            {showCopy && <CopyButton value={name} />}
          </span>
        </span>
      )}
    </span>
  )
}
