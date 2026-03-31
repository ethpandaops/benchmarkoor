/** label filters: key → set of selected values (OR within key, AND across keys) */
export type LabelFilters = Map<string, Set<string>>

/** Parse URL param `key:val1|val2,key2:val3` into a Map */
export function parseLabelFilters(raw: string | undefined): LabelFilters {
  const filters: LabelFilters = new Map()
  if (!raw) return filters
  for (const segment of raw.split(',')) {
    const idx = segment.indexOf(':')
    if (idx < 1) continue
    const key = decodeURIComponent(segment.slice(0, idx))
    const values = segment
      .slice(idx + 1)
      .split('|')
      .map(decodeURIComponent)
      .filter(Boolean)
    if (values.length > 0) filters.set(key, new Set(values))
  }
  return filters
}

export function serializeLabelFilters(filters: LabelFilters): string | undefined {
  if (filters.size === 0) return undefined
  const parts: string[] = []
  for (const [key, values] of filters) {
    if (values.size === 0) continue
    parts.push(
      `${encodeURIComponent(key)}:${Array.from(values).map(encodeURIComponent).join('|')}`,
    )
  }
  return parts.length > 0 ? parts.join(',') : undefined
}
