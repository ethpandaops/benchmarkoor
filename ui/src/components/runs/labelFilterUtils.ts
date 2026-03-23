export interface LabelFilter {
  key: string
  value: string
}

export function parseLabelFilters(param: string | undefined): LabelFilter[] {
  if (!param) return []
  return param
    .split(',')
    .map((pair) => {
      const idx = pair.indexOf(':')
      if (idx < 1) return null
      return { key: pair.slice(0, idx), value: pair.slice(idx + 1) }
    })
    .filter((f): f is LabelFilter => f !== null)
}

export function serializeLabelFilters(filters: LabelFilter[]): string | undefined {
  if (filters.length === 0) return undefined
  return filters.map((f) => `${f.key}:${f.value}`).join(',')
}
