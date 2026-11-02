import { CircleAlert } from 'lucide-react'
import type { MempoolScenarioResult } from '@/api/types'
import { collectProblems, openScenario, scenarioDomId } from './runOverview'

// ProblemsBanner lists the failed hard checks and scenario errors of a run.
// Each item links to its scenario card. It renders nothing for a clean run.
export function ProblemsBanner({ scenarios }: { scenarios: MempoolScenarioResult[] }) {
  const problems = collectProblems(scenarios)
  if (problems.length === 0) return null
  const hard = problems.filter((p) => p.kind === 'hard-fail').length
  const errors = problems.length - hard
  const heading = [hard > 0 && `${hard} hard failure${hard === 1 ? '' : 's'}`, errors > 0 && `${errors} scenario error${errors === 1 ? '' : 's'}`].filter(Boolean).join(', ')

  return (
    <div role="alert" className="flex gap-3 rounded-sm border border-red-200 bg-red-50 px-4 py-3 dark:border-red-800 dark:bg-red-900/20">
      <CircleAlert className="mt-0.5 size-5 shrink-0 text-red-600 dark:text-red-400" />
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <p className="text-sm/6 font-medium text-red-800 dark:text-red-200">{heading}</p>
        <ul className="flex flex-col gap-1 text-xs/5">
          {problems.map((p, i) => (
            <li key={`${p.scenario}-${p.name}-${i}`} className="flex min-w-0 flex-wrap items-baseline gap-x-2">
              <a
                href={`#${scenarioDomId(p.scenario)}`}
                onClick={(e) => {
                  e.preventDefault()
                  openScenario(p.scenario)
                }}
                className="shrink-0 font-mono font-medium text-red-800 underline decoration-red-300 underline-offset-2 hover:text-red-950 dark:text-red-200 dark:decoration-red-700 dark:hover:text-white"
              >
                {p.scenario}
              </a>
              <span className="min-w-0 break-words text-red-900 dark:text-red-100">{p.name}</span>
              {p.actual && (
                <span className="min-w-0 truncate font-mono text-red-700 dark:text-red-300" title={p.actual}>
                  {p.kind === 'hard-fail' ? `actual ${p.actual}` : p.actual}
                </span>
              )}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
