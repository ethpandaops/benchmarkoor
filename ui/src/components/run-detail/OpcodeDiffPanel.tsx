/**
 * OpcodeDiffPanel renders a per-opcode diff table comparing the
 * suite-defined opcode counts to the ones extracted at run-time
 * (test-opcodes.json). Only opcodes where the counts disagree are
 * rendered, sorted by absolute delta. Callers compute the rows.
 */

export interface OpcodeDiffRow {
  opcode: string
  suite: number
  run: number
  delta: number
}

interface OpcodeDiffPanelProps {
  rows: OpcodeDiffRow[]
  /** Optional caption shown above the table (e.g. test name). */
  caption?: string
  className?: string
}

export function OpcodeDiffPanel({ rows, caption, className }: OpcodeDiffPanelProps) {
  if (rows.length === 0) return null

  return (
    <div className={className}>
      {caption && (
        <div className="mb-1 break-all font-mono text-xs/5 font-medium text-gray-900 dark:text-gray-100">
          {caption}
        </div>
      )}
      <table className="min-w-full font-mono text-xs/5">
        <thead>
          <tr className="border-b border-gray-200 text-left text-gray-500 dark:border-gray-700 dark:text-gray-400">
            <th className="px-2 py-1 font-medium">Opcode</th>
            <th className="px-2 py-1 text-right font-medium">Suite</th>
            <th className="px-2 py-1 text-right font-medium">Run</th>
            <th className="px-2 py-1 text-right font-medium">Δ</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {rows.map((r) => (
            <tr key={r.opcode}>
              <td className="px-2 py-1 text-gray-900 dark:text-gray-100">{r.opcode}</td>
              <td className="px-2 py-1 text-right text-gray-700 dark:text-gray-300">{r.suite.toLocaleString()}</td>
              <td className="px-2 py-1 text-right text-gray-700 dark:text-gray-300">{r.run.toLocaleString()}</td>
              <td className={`px-2 py-1 text-right font-medium ${r.delta > 0 ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>
                {r.delta > 0 ? '+' : ''}{r.delta.toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
