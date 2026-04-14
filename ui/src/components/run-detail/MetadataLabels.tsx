interface MetadataLabelsProps {
  labels?: Record<string, string>
}

/**
 * Renders user-defined metadata labels (everything except `name` and
 * `github.*` reserved keys) as pill-shaped chips. Used on both the normal
 * and live run detail pages.
 */
export function MetadataLabels({ labels }: MetadataLabelsProps) {
  if (!labels) return null

  const userLabels = Object.entries(labels).filter(
    ([k]) => !k.startsWith('github.') && k !== 'name',
  )

  if (userLabels.length === 0) return null

  return (
    <div className="flex flex-wrap items-center gap-2">
      {userLabels.map(([key, value]) => (
        <span
          key={key}
          className="inline-flex items-center gap-1.5 rounded-xs border border-blue-200 bg-blue-50 px-2 py-1 text-xs/5 font-medium text-blue-700 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300"
        >
          <span className="font-semibold">{key}</span>
          <span>=</span>
          <span>{value}</span>
        </span>
      ))}
    </div>
  )
}
