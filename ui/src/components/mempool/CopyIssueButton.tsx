import { useEffect, useState } from 'react'
import clsx from 'clsx'
import { Check, ClipboardCopy } from 'lucide-react'

// CopyIssueButton copies a Markdown issue body to the clipboard and shows
// "Copied" for a moment. markdown runs only on click.
export function CopyIssueButton({ markdown, compact = false, className }: { markdown: () => string; compact?: boolean; className?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')

  useEffect(() => {
    if (state === 'idle') return
    const t = window.setTimeout(() => setState('idle'), 1500)
    return () => window.clearTimeout(t)
  }, [state])

  const copy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      await navigator.clipboard.writeText(markdown())
      setState('copied')
    } catch {
      setState('failed')
    }
  }

  const text = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : 'Copy as issue'
  const Icon = state === 'copied' ? Check : ClipboardCopy
  return (
    <button
      type="button"
      onClick={copy}
      title="Copy this result as a Markdown issue body"
      aria-label={text}
      className={clsx(
        'inline-flex shrink-0 items-center gap-1 self-start rounded-xs px-1 text-xs/5 hover:bg-gray-100 dark:hover:bg-gray-700/60',
        state === 'copied' ? 'text-green-700 dark:text-green-400' : state === 'failed' ? 'text-red-700 dark:text-red-400' : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-100',
        className,
      )}
    >
      <Icon className="size-3.5" />
      {(!compact || state !== 'idle') && <span className="whitespace-nowrap">{text}</span>}
    </button>
  )
}
