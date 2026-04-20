// Group definition type and URL encoding helpers for the group
// comparison page. Separated from the component file so the React
// fast-refresh lint rule doesn't complain about mixed exports.

export interface GroupDef {
  client: string
  metadata: Record<string, string>
}

/**
 * Parse the groups search param. Format:
 * "client1:key1=val1,key2=val2;client2:"
 */
export function parseGroupsParam(param: string | undefined): GroupDef[] {
  if (!param) return []

  return param
    .split(';')
    .filter(Boolean)
    .map((seg) => {
      const [client, rest] = seg.split(':', 2)
      const metadata: Record<string, string> = {}
      if (rest) {
        for (const pair of rest.split(',').filter(Boolean)) {
          const eq = pair.indexOf('=')
          if (eq > 0) {
            metadata[pair.slice(0, eq)] = pair.slice(eq + 1)
          }
        }
      }
      return { client: client || '', metadata }
    })
}

export function encodeGroupsParam(groups: GroupDef[]): string {
  return groups
    .map((g) => {
      const meta = Object.entries(g.metadata)
        .map(([k, v]) => `${k}=${v}`)
        .join(',')
      return `${g.client}:${meta}`
    })
    .join(';')
}
