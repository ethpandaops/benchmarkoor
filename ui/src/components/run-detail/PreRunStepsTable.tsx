import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { ListChecks, Check, Copy } from 'lucide-react'
import type { StepResult, SuiteFile } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { Modal } from '@/components/shared/Modal'
import { TimeBreakdown } from './TimeBreakdown'
import { MGasBreakdown } from './MGasBreakdown'
import { ExecutionsList } from './ExecutionsList'

export type PreRunSortColumn = 'order' | 'name' | 'time' | 'passed' | 'failed'
export type PreRunSortDirection = 'asc' | 'desc'

interface PreRunStepsTableProps {
  preRunSteps: Record<string, StepResult>
  suitePreRunSteps?: SuiteFile[]
  runId: string
  suiteHash?: string
}

function SortIcon({ direction, active }: { direction: PreRunSortDirection; active: boolean }) {
  return (
    <svg
      className={clsx('ml-1 inline-block size-3', active ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400')}
      viewBox="0 0 12 12"
      fill="currentColor"
    >
      {direction === 'asc' ? <path d="M6 2L10 8H2L6 2Z" /> : <path d="M6 10L2 4H10L6 10Z" />}
    </svg>
  )
}

function SortableHeader({
  label,
  column,
  currentSort,
  currentDirection,
  onSort,
  className,
}: {
  label: string
  column: PreRunSortColumn
  currentSort: PreRunSortColumn
  currentDirection: PreRunSortDirection
  onSort: (column: PreRunSortColumn) => void
  className?: string
}) {
  const isActive = currentSort === column
  return (
    <th
      onClick={() => onSort(column)}
      className={clsx(
        'cursor-pointer select-none px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300',
        className,
      )}
    >
      {label}
      <SortIcon direction={isActive ? currentDirection : 'asc'} active={isActive} />
    </th>
  )
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
    </button>
  )
}

export function PreRunStepsTable({
  preRunSteps,
  suitePreRunSteps,
  runId,
  suiteHash,
}: PreRunStepsTableProps) {
  const [sortBy, setSortBy] = useState<PreRunSortColumn>('order')
  const [sortDir, setSortDir] = useState<PreRunSortDirection>('asc')
  const [selectedStep, setSelectedStep] = useState<string | undefined>(undefined)

  const handleSort = (column: PreRunSortColumn) => {
    const newDirection = sortBy === column && sortDir === 'asc' ? 'desc' : 'asc'
    setSortBy(column)
    setSortDir(column === sortBy ? newDirection : 'asc')
  }

  const executionOrder = useMemo(() => {
    if (!suitePreRunSteps) return new Map<string, number>()
    return new Map(suitePreRunSteps.map((step, index) => [step.og_path, index + 1]))
  }, [suitePreRunSteps])

  const sortedSteps = useMemo(() => {
    const entries = Object.entries(preRunSteps)

    return entries.sort(([nameA, stepA], [nameB, stepB]) => {
      let comparison = 0
      const statsA = stepA.aggregated
      const statsB = stepB.aggregated

      if (sortBy === 'order') {
        const orderA = executionOrder.get(nameA) ?? Infinity
        const orderB = executionOrder.get(nameB) ?? Infinity
        comparison = orderA - orderB
      } else if (sortBy === 'time') {
        comparison = (statsA?.time_total ?? 0) - (statsB?.time_total ?? 0)
      } else if (sortBy === 'passed') {
        comparison = (statsA?.success ?? 0) - (statsB?.success ?? 0)
      } else if (sortBy === 'failed') {
        comparison = (statsA?.fail ?? 0) - (statsB?.fail ?? 0)
      } else {
        comparison = nameA.localeCompare(nameB)
      }

      return sortDir === 'asc' ? comparison : -comparison
    })
  }, [preRunSteps, sortBy, sortDir, executionOrder])

  const stepCount = sortedSteps.length

  if (stepCount === 0) {
    return null
  }

  const selectedStepData = selectedStep ? preRunSteps[selectedStep] : undefined

  return (
    <div className="flex flex-col gap-4">
      <h2 className="flex items-center gap-2 text-lg/7 font-semibold text-gray-900 dark:text-gray-100">
        <ListChecks className="size-5 text-gray-400 dark:text-gray-500" />
        Pre-Run Steps ({stepCount})
      </h2>

      <div className="overflow-x-auto rounded-xs bg-white shadow-xs dark:bg-gray-800">
        <table className="min-w-full table-fixed divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <SortableHeader
                label="#"
                column="order"
                currentSort={sortBy}
                currentDirection={sortDir}
                onSort={handleSort}
                className="w-12"
              />
              <SortableHeader label="Step" column="name" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
              <SortableHeader label="Total Time" column="time" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-28 text-right" />
              <SortableHeader label="Failed" column="failed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-16 text-center" />
              <SortableHeader label="Passed" column="passed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-16 text-center" />
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {sortedSteps.map(([stepName, step]) => {
              const stats = step.aggregated
              const order = executionOrder.get(stepName)

              return (
                <tr
                  key={stepName}
                  onClick={() => setSelectedStep(stepName)}
                  className="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
                >
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 font-medium text-gray-500 dark:text-gray-400">
                    {order ?? '-'}
                  </td>
                  <td className="max-w-md px-4 py-3">
                    <div className="truncate text-sm/6 font-medium text-gray-900 dark:text-gray-100" title={stepName}>
                      {stepName}
                    </div>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {stats ? <Duration nanoseconds={stats.time_total} /> : '-'}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-center">
                    {stats && stats.fail > 0 && <Badge variant="error">{stats.fail}</Badge>}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-center">
                    {stats && stats.success > 0 && <Badge variant="success">{stats.success}</Badge>}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Pre-Run Step Detail Modal */}
      {selectedStep && selectedStepData && (
        <Modal
          isOpen={!!selectedStep}
          onClose={() => setSelectedStep(undefined)}
          title={`Pre-Run Step #${executionOrder.get(selectedStep) ?? '?'}`}
        >
          <div className="flex flex-col gap-6">
            <div className="flex flex-col gap-2">
              <div>
                <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Step Name</div>
                <div className="flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
                  <span>{selectedStep}</span>
                  <CopyButton text={selectedStep} />
                </div>
              </div>
            </div>

            {selectedStepData.aggregated && (
              <>
                <TimeBreakdown methods={selectedStepData.aggregated.method_stats.times} />
                <MGasBreakdown methods={selectedStepData.aggregated.method_stats.mgas_s} />
              </>
            )}

            {suiteHash && (
              <div>
                <h4 className="mb-2 text-sm/6 font-semibold text-gray-900 dark:text-gray-100">Pre-Run Step</h4>
                <ExecutionsList
                  runId={runId}
                  suiteHash={suiteHash}
                  testName={selectedStep}
                  stepType="pre_run"
                />
              </div>
            )}
          </div>
        </Modal>
      )}
    </div>
  )
}
