import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { Plus, Trash2 } from 'lucide-react'
import { JDenticon } from '@/components/shared/JDenticon'
import { Spinner } from '@/components/shared/Spinner'
import { type IndexEntry, getIndexAggregatedStats } from '@/api/types'
import { formatTimestamp } from '@/utils/date'
import { formatDuration } from '@/utils/format'
import { NEUTRAL_COLOR, createColorScale } from '@/utils/runColorScale'
import { type GroupDef, setGroupRuns, toggleGroupRun } from './groupUtils'
import { RUN_SLOTS } from './constants'

// ── Props ────────────────────────────────────────────────────────

interface GroupBuilderProps {
  availableSuites: string[]
  selectedSuite: string
  onSuiteChange: (hash: string) => void
  suiteName?: string
  groups: GroupDef[]
  onGroupsChange: (groups: GroupDef[]) => void
  availableClients: string[]
  availableMetadataKeys: Map<string, Set<string>>
  sampleSize: number
  onSampleSizeChange: (n: number) => void
  aggMode: 'avg' | 'median'
  onAggModeChange: (mode: 'avg' | 'median') => void
  /** Run IDs each group averages: its explicit selection, or the newest `sampleSize` matched runs. */
  groupSelectedRunIds: string[][]
  /** Matched index entries per group (sorted newest-first, full list before sample-size truncation). */
  groupMatchedRuns: IndexEntry[][]
  /** Per-group loading flag — true while this group's config or any of its result queries are in-flight. */
  groupLoadingFlags: boolean[]
  /** True while the run index is in-flight. The option lists and run counts are empty until it arrives. */
  indexLoading: boolean
}

/** MGas/s of a run over all its steps, or undefined when it has no throughput yet. */
function mgasPerSec(run: IndexEntry): number | undefined {
  const stats = getIndexAggregatedStats(run)
  return stats.gasUsedDuration > 0 ? (stats.gasUsed * 1000) / stats.gasUsedDuration : undefined
}

// No status means completed, for entries written before the field existed.
function isRunCompleted(run: IndexEntry): boolean {
  return !run.status || run.status === 'completed'
}

function isRunLive(run: IndexEntry): boolean {
  return run.status === 'running'
}

// ── Component ────────────────────────────────────────────────────

export function GroupBuilder({
  availableSuites,
  selectedSuite,
  onSuiteChange,
  suiteName,
  groups,
  onGroupsChange,
  availableClients,
  availableMetadataKeys,
  sampleSize,
  onSampleSizeChange,
  aggMode,
  onAggModeChange,
  groupSelectedRunIds,
  groupMatchedRuns,
  groupLoadingFlags,
  indexLoading,
}: GroupBuilderProps) {
  const addGroup = () => {
    const nextClient = availableClients.find((c) => !groups.some((g) => g.client === c)) ?? availableClients[0] ?? ''
    onGroupsChange([...groups, { client: nextClient, metadata: {} }])
  }

  const removeGroup = (idx: number) => {
    onGroupsChange(groups.filter((_, i) => i !== idx))
  }

  const updateGroup = (idx: number, patch: Partial<GroupDef>) => {
    onGroupsChange(groups.map((g, i) => (i === idx ? { ...g, ...patch } : g)))
  }

  const addMetadata = (idx: number, key: string, value: string) => {
    const group = groups[idx]
    updateGroup(idx, { metadata: { ...group.metadata, [key]: value }, runs: undefined })
  }

  // Toggle one run in the group's selection (see toggleGroupRun).
  const toggleRun = (idx: number, runId: string) => {
    onGroupsChange(groups.map((g, i) => (i === idx ? toggleGroupRun(g, groupMatchedRuns[idx] ?? [], sampleSize, runId) : g)))
  }

  // Shift-click: set a whole range to the anchor's state.
  const setRuns = (idx: number, runIds: string[], include: boolean) => {
    onGroupsChange(groups.map((g, i) => (i === idx ? setGroupRuns(g, groupMatchedRuns[idx] ?? [], sampleSize, runIds, include) : g)))
  }

  const resetRuns = (idx: number) => {
    onGroupsChange(groups.map((g, i) => (i === idx ? { client: g.client, metadata: g.metadata } : g)))
  }

  const removeMetadata = (idx: number, key: string) => {
    const group = groups[idx]
    const next = { ...group.metadata }
    delete next[key]
    updateGroup(idx, { metadata: next, runs: undefined })
  }

  // The URL can select a suite before the index arrives. Keep it in the
  // option list so the select does not fall back to the placeholder.
  const suiteOptions = selectedSuite && !availableSuites.includes(selectedSuite)
    ? [selectedSuite, ...availableSuites]
    : availableSuites

  return (
    <div className="flex flex-col gap-4 rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      {/* Suite picker + controls row */}
      <div className="flex flex-wrap items-center gap-4">
        <div className="flex items-center gap-2">
          <label className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Suite:</label>
          <select
            value={selectedSuite}
            onChange={(e) => onSuiteChange(e.target.value)}
            className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
          >
            <option value="">Select a suite…</option>
            {suiteOptions.map((hash) => (
              <option key={hash} value={hash}>
                {hash === selectedSuite && suiteName ? suiteName : hash.slice(0, 12)}
              </option>
            ))}
          </select>
          {selectedSuite && (
            <JDenticon value={selectedSuite} size={20} className="shrink-0 rounded-xs" />
          )}
        </div>

        <div className="flex items-center gap-2">
          <label className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Sample:</label>
          <input
            type="number"
            min={1}
            max={50}
            value={sampleSize}
            onChange={(e) => onSampleSizeChange(Math.max(1, Math.min(50, parseInt(e.target.value, 10) || 5)))}
            className="w-14 rounded-xs border border-gray-300 bg-white px-2 py-1 text-center text-sm/6 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
          />
          <span className="text-xs text-gray-500 dark:text-gray-400">latest runs per group · click a run box to pick runs by hand</span>
        </div>

        <div className="flex items-center gap-2">
          <label className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Mode:</label>
          {(['avg', 'median'] as const).map((m) => (
            <button
              key={m}
              onClick={() => onAggModeChange(m)}
              className={clsx(
                'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                aggMode === m
                  ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                  : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
              )}
            >
              {m === 'avg' ? 'Average' : 'Median'}
            </button>
          ))}
        </div>
      </div>

      {/* Group cards */}
      {selectedSuite && (
        <div className="flex flex-col gap-3">
          {groups.map((group, idx) => (
            <GroupCard
              key={idx}
              group={group}
              index={idx}
              availableClients={availableClients}
              availableMetadataKeys={availableMetadataKeys}
              selectedRunIds={groupSelectedRunIds[idx] ?? []}
              sampleSize={sampleSize}
              matchedRuns={groupMatchedRuns[idx] ?? []}
              loading={groupLoadingFlags[idx] ?? false}
              indexLoading={indexLoading}
              onToggleRun={(runId) => toggleRun(idx, runId)}
              onSetRuns={(runIds, include) => setRuns(idx, runIds, include)}
              onResetRuns={() => resetRuns(idx)}
              onClientChange={(client) => updateGroup(idx, { client, metadata: {}, runs: undefined })}
              onAddMetadata={(key, val) => addMetadata(idx, key, val)}
              onRemoveMetadata={(key) => removeMetadata(idx, key)}
              onRemove={() => removeGroup(idx)}
              canRemove={groups.length > 1}
            />
          ))}
          {availableClients.length > 0 && groups.length < RUN_SLOTS.length && (
            <button
              onClick={addGroup}
              className="flex items-center gap-1.5 self-start rounded-xs border border-dashed border-gray-300 px-3 py-1.5 text-sm/6 text-gray-600 hover:border-gray-400 hover:text-gray-800 dark:border-gray-600 dark:text-gray-400 dark:hover:border-gray-500 dark:hover:text-gray-200"
            >
              <Plus className="size-4" />
              Add group
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ── Group card ───────────────────────────────────────────────────

function GroupCard({
  group,
  index,
  availableClients,
  availableMetadataKeys,
  selectedRunIds,
  sampleSize,
  matchedRuns,
  loading,
  indexLoading,
  onToggleRun,
  onSetRuns,
  onResetRuns,
  onClientChange,
  onAddMetadata,
  onRemoveMetadata,
  onRemove,
  canRemove,
}: {
  group: GroupDef
  index: number
  availableClients: string[]
  availableMetadataKeys: Map<string, Set<string>>
  selectedRunIds: string[]
  sampleSize: number
  matchedRuns: IndexEntry[]
  loading: boolean
  indexLoading: boolean
  onToggleRun: (runId: string) => void
  onSetRuns: (runIds: string[], include: boolean) => void
  onResetRuns: () => void
  onClientChange: (client: string) => void
  onAddMetadata: (key: string, value: string) => void
  onRemoveMetadata: (key: string) => void
  onRemove: () => void
  canRemove: boolean
}) {
  const SLOT_COLORS = ['bg-blue-100 dark:bg-blue-900/30', 'bg-orange-100 dark:bg-orange-900/30', 'bg-purple-100 dark:bg-purple-900/30', 'bg-green-100 dark:bg-green-900/30', 'bg-red-100 dark:bg-red-900/30']

  // Metadata keys not yet used by this group.
  const unusedKeys = [...availableMetadataKeys.entries()].filter(
    ([key]) => !(key in group.metadata),
  )

  const manual = group.runs !== undefined
  const runCount = selectedRunIds.length

  // The URL can select a client before the index arrives. Keep it in the
  // option list so the select does not fall back to the placeholder.
  const clientOptions = group.client && !availableClients.includes(group.client)
    ? [group.client, ...availableClients]
    : availableClients

  return (
    <div className={clsx('flex flex-col gap-2 rounded-sm border border-gray-200 p-3 dark:border-gray-700', SLOT_COLORS[index % SLOT_COLORS.length])}>
      <div className="flex items-center gap-3">
        <span className="text-xs/5 font-bold text-gray-500 dark:text-gray-400">
          Group {String.fromCharCode(65 + index)}
        </span>

        <select
          value={group.client}
          onChange={(e) => onClientChange(e.target.value)}
          className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
        >
          <option value="">Select client…</option>
          {clientOptions.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>

        {group.client && (
          <img
            src={`/img/clients/${group.client}.jpg`}
            alt={group.client}
            className="size-6 rounded-full object-cover"
          />
        )}

        {(loading || indexLoading) && (
          <span className="ml-auto inline-flex items-center gap-1.5 text-xs/5 text-gray-500 dark:text-gray-400">
            <Spinner size="sm" />
            Loading…
          </span>
        )}

        {/* The count is 0 until the index arrives, so hide it until then. */}
        {!indexLoading && (
          <span className={clsx(
            'rounded-xs px-2 py-0.5 text-xs/5 font-medium',
            !loading && 'ml-auto',
            runCount >= sampleSize || (manual && runCount > 0)
              ? 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-200'
              : runCount > 0
                ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/40 dark:text-yellow-200'
                : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300',
          )}>
            {runCount} run{runCount !== 1 ? 's' : ''} {manual ? 'selected' : 'found'}
          </span>
        )}

        {canRemove && (
          <button
            onClick={onRemove}
            className="text-gray-400 hover:text-red-600 dark:text-gray-500 dark:hover:text-red-400"
            title="Remove group"
          >
            <Trash2 className="size-4" />
          </button>
        )}
      </div>

      {/* Metadata filter pills */}
      {group.client && (
        <div className="flex flex-wrap items-center gap-2">
          {Object.entries(group.metadata).map(([key, val]) => (
            <span
              key={key}
              className="inline-flex items-center gap-1 rounded-xs bg-white px-2 py-0.5 text-xs/5 font-medium text-gray-700 shadow-xs dark:bg-gray-700 dark:text-gray-200"
            >
              {key}={val}
              <button
                onClick={() => onRemoveMetadata(key)}
                className="ml-0.5 text-gray-400 hover:text-red-600 dark:text-gray-500 dark:hover:text-red-400"
              >
                ×
              </button>
            </span>
          ))}

          {unusedKeys.length > 0 && (
            <select
              value=""
              onChange={(e) => {
                const key = e.target.value
                if (!key) return
                const values = availableMetadataKeys.get(key)
                const firstVal = values ? [...values][0] : ''
                if (firstVal) onAddMetadata(key, firstVal)
              }}
              className="rounded-xs border border-dashed border-gray-300 bg-transparent px-2 py-0.5 text-xs/5 text-gray-500 dark:border-gray-600 dark:text-gray-400"
            >
              <option value="">+ filter…</option>
              {unusedKeys.map(([key]) => (
                <option key={key} value={key}>{key}</option>
              ))}
            </select>
          )}

          {/* Value picker for each metadata key — show next to the pill */}
          {Object.entries(group.metadata).map(([key]) => {
            const values = availableMetadataKeys.get(key)
            if (!values || values.size <= 1) return null
            return (
              <select
                key={`val-${key}`}
                value={group.metadata[key]}
                onChange={(e) => onAddMetadata(key, e.target.value)}
                className="rounded-xs border border-gray-300 bg-white px-1.5 py-0.5 text-xs/5 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
              >
                {[...values].sort().map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
            )
          })}
        </div>
      )}

      {/* Per-group aggregate stats across the sampled runs */}
      {matchedRuns.length > 0 && (() => {
        const selected = new Set(selectedRunIds)
        const mgasValues = matchedRuns
          .filter((r) => selected.has(r.run_id))
          .map(mgasPerSec)
          .filter((v): v is number => v !== undefined)
          .sort((a, b) => a - b)

        if (mgasValues.length === 0) return null

        const min = mgasValues[0]
        const max = mgasValues[mgasValues.length - 1]
        const mean = mgasValues.reduce((a, b) => a + b, 0) / mgasValues.length
        const p95 = mgasValues[Math.min(Math.floor(mgasValues.length * 0.95), mgasValues.length - 1)]
        const p99 = mgasValues[Math.min(Math.floor(mgasValues.length * 0.99), mgasValues.length - 1)]

        return (
          <div className="flex flex-wrap gap-3 text-xs text-gray-600 dark:text-gray-300">
            <span>Mean: <strong>{mean.toFixed(2)}</strong></span>
            <span>Min: {min.toFixed(2)}</span>
            <span>Max: {max.toFixed(2)}</span>
            <span>P95: {p95.toFixed(2)}</span>
            <span>P99: {p99.toFixed(2)}</span>
            <span className="text-gray-400 dark:text-gray-500">MGas/s</span>
          </div>
        )
      })()}

      {/* Run boxes — shows which runs matched and which are used in the sample */}
      {matchedRuns.length > 0 && (
        <RunBoxes
          runs={matchedRuns}
          selectedRunIds={selectedRunIds}
          manual={manual}
          onToggleRun={onToggleRun}
          onSetRuns={onSetRuns}
          onResetRuns={onResetRuns}
        />
      )}
    </div>
  )
}

// ── Run boxes with tooltip ───────────────────────────────────────

function RunBoxes({ runs, selectedRunIds, manual, onToggleRun, onSetRuns, onResetRuns }: {
  runs: IndexEntry[]
  selectedRunIds: string[]
  /** True when the group has an explicit selection instead of the newest-N sample. */
  manual: boolean
  onToggleRun: (runId: string) => void
  onSetRuns: (runIds: string[], include: boolean) => void
  onResetRuns: () => void
}) {
  const [tooltip, setTooltip] = useState<{ run: IndexEntry; x: number; y: number } | null>(null)
  const selected = useMemo(() => new Set(selectedRunIds), [selectedRunIds])
  // The last plain-clicked run. A shift-click applies its state to every
  // run between it and the clicked one.
  const [anchor, setAnchor] = useState<string | null>(null)

  const handleClick = (e: React.MouseEvent<HTMLAnchorElement>, run: IndexEntry) => {
    // A modifier or middle click keeps the browser's open-in-new-tab
    // behaviour. Shift is the range selector.
    if (e.metaKey || e.ctrlKey || e.button !== 0) return
    e.preventDefault()

    const anchorIdx = anchor ? runs.findIndex((r) => r.run_id === anchor) : -1
    if (e.shiftKey && anchorIdx >= 0) {
      const clickedIdx = runs.findIndex((r) => r.run_id === run.run_id)
      const [from, to] = anchorIdx < clickedIdx ? [anchorIdx, clickedIdx] : [clickedIdx, anchorIdx]
      onSetRuns(runs.slice(from, to + 1).map((r) => r.run_id), selected.has(anchor as string))
      return
    }

    setAnchor(run.run_id)
    onToggleRun(run.run_id)
  }

  // Grade each box by its shortfall against the best run of this group,
  // the same scale as the per-client mode of the suite page heatmap.
  const scale = useMemo(
    () => createColorScale(runs.map(mgasPerSec).filter((v): v is number => v !== undefined), true),
    [runs],
  )

  return (
    <div className="relative flex flex-wrap items-center gap-1">
      <span className="mr-1 text-xs text-gray-500 dark:text-gray-400">Runs:</span>
      {runs.map((run) => {
        const inSample = selected.has(run.run_id)
        const completed = isRunCompleted(run)
        const live = isRunLive(run)
        // A live run reports its failed count; total - passed would count
        // the tests it has not reached yet.
        const failedTests = live ? run.tests.tests_failed : run.tests.tests_total - run.tests.tests_passed
        const mgas = mgasPerSec(run)

        return (
          <a
            key={run.run_id}
            href={`/runs/${run.run_id}`}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => handleClick(e, run)}
            onMouseEnter={(e) => {
              const rect = e.currentTarget.getBoundingClientRect()
              setTooltip({ run, x: rect.left + rect.width / 2, y: rect.top })
            }}
            onMouseLeave={() => setTooltip(null)}
            className={clsx(
              'relative size-4 shrink-0 rounded-xs transition-all hover:scale-110 hover:opacity-100',
              !inSample && 'opacity-30',
              live
                ? 'ring-2 ring-inset ring-blue-500 dark:ring-blue-400'
                : !completed
                  ? 'ring-2 ring-inset ring-red-600 dark:ring-red-500'
                  : failedTests > 0
                    ? 'ring-2 ring-inset ring-orange-500'
                    : inSample && 'ring-1 ring-inset ring-black/10 dark:ring-white/10',
            )}
            style={{
              backgroundColor: live
                ? '#3b82f6' // blue-500
                : !completed
                  ? '#6b7280' // gray-500
                  : mgas === undefined
                    ? NEUTRAL_COLOR
                    : scale.color(mgas),
            }}
          >
            {live && (
              <span className="absolute inset-0 flex items-center justify-center">
                <span className="size-1.5 animate-pulse rounded-full bg-white" />
              </span>
            )}
            {!live && completed && failedTests > 0 && (
              <svg className="absolute inset-0 size-4" viewBox="0 0 16 16" fill="none">
                <text x="8" y="12" textAnchor="middle" fill="white" fontSize="11" fontWeight="bold" fontFamily="system-ui">!</text>
              </svg>
            )}
            {!live && !completed && (
              <svg className="absolute inset-0 size-4 text-red-600 dark:text-red-400" viewBox="0 0 16 16" fill="none">
                <path d="M3 3l10 10M3 13L13 3" stroke="currentColor" strokeWidth="1.5" />
              </svg>
            )}
          </a>
        )
      })}

      {manual && (
        <button
          onClick={onResetRuns}
          className="ml-1 text-xs text-blue-600 hover:underline dark:text-blue-400"
          title="Drop the explicit selection and use the newest runs again"
        >
          Reset to sample
        </button>
      )}

      {tooltip && (() => {
        const stats = getIndexAggregatedStats(tooltip.run)
        const mgas = mgasPerSec(tooltip.run)
        return (
          <div
            className="pointer-events-none fixed z-50 rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-700"
            style={{ left: tooltip.x, top: tooltip.y - 8, transform: 'translate(-50%, -100%)' }}
          >
            <div className="flex flex-col gap-1">
              <div className="font-medium">{formatTimestamp(tooltip.run.timestamp)}</div>
              {isRunLive(tooltip.run) && (
                <div className="font-medium text-blue-600 dark:text-blue-400">In progress</div>
              )}
              {!isRunLive(tooltip.run) && !isRunCompleted(tooltip.run) && (
                <div className="font-medium text-red-600 dark:text-red-400">
                  {tooltip.run.status === 'container_died' ? 'Container Died' : tooltip.run.status === 'cancelled' ? 'Cancelled' : tooltip.run.status}
                </div>
              )}
              <div>Duration: {formatDuration(stats.duration)}</div>
              {mgas !== undefined && <div>MGas/s: {mgas.toFixed(2)}</div>}
              <div className="flex gap-2">
                <span className="text-green-600 dark:text-green-400">
                  {tooltip.run.tests.tests_passed} passed
                </span>
                {tooltip.run.tests.tests_total - tooltip.run.tests.tests_passed > 0 && (
                  <span className="text-red-600 dark:text-red-400">
                    {tooltip.run.tests.tests_total - tooltip.run.tests.tests_passed} failed
                  </span>
                )}
                <span className="text-gray-500 dark:text-gray-400">
                  ({tooltip.run.tests.tests_total} total)
                </span>
              </div>
              <div className="text-gray-400 dark:text-gray-500">
                Click to {selected.has(tooltip.run.run_id) ? 'exclude' : 'include'} · Shift-click for a range · ⌘/Ctrl-click to open
              </div>
            </div>
          </div>
        )
      })()}
    </div>
  )
}
