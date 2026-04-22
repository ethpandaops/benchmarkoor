import { useState } from 'react'
import clsx from 'clsx'
import { Plus, Trash2 } from 'lucide-react'
import { JDenticon } from '@/components/shared/JDenticon'
import { type IndexEntry, getIndexAggregatedStats } from '@/api/types'
import { formatTimestamp } from '@/utils/date'
import { formatDuration } from '@/utils/format'
import type { GroupDef } from './groupUtils'

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
  groupRunCounts: number[]
  /** Matched index entries per group (sorted newest-first, full list before sample-size truncation). */
  groupMatchedRuns: IndexEntry[][]
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
  groupRunCounts,
  groupMatchedRuns,
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
    updateGroup(idx, { metadata: { ...group.metadata, [key]: value } })
  }

  const removeMetadata = (idx: number, key: string) => {
    const group = groups[idx]
    const next = { ...group.metadata }
    delete next[key]
    updateGroup(idx, { metadata: next })
  }

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
            {availableSuites.map((hash) => (
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
          <span className="text-xs text-gray-500 dark:text-gray-400">latest runs per group</span>
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
              runCount={groupRunCounts[idx] ?? 0}
              sampleSize={sampleSize}
              matchedRuns={groupMatchedRuns[idx] ?? []}
              onClientChange={(client) => updateGroup(idx, { client, metadata: {} })}
              onAddMetadata={(key, val) => addMetadata(idx, key, val)}
              onRemoveMetadata={(key) => removeMetadata(idx, key)}
              onRemove={() => removeGroup(idx)}
              canRemove={groups.length > 1}
            />
          ))}
          {availableClients.length > 0 && groups.length < 5 && (
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
  runCount,
  sampleSize,
  matchedRuns,
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
  runCount: number
  sampleSize: number
  matchedRuns: IndexEntry[]
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
          {availableClients.map((c) => (
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

        <span className={clsx(
          'ml-auto rounded-xs px-2 py-0.5 text-xs/5 font-medium',
          runCount >= sampleSize
            ? 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-200'
            : runCount > 0
              ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/40 dark:text-yellow-200'
              : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300',
        )}>
          {runCount} run{runCount !== 1 ? 's' : ''} found
        </span>

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
        const sampled = matchedRuns.slice(0, sampleSize)
        const mgasValues = sampled
          .map((r) => {
            const s = getIndexAggregatedStats(r)
            return s.gasUsedDuration > 0 ? (s.gasUsed * 1000) / s.gasUsedDuration : undefined
          })
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
      {matchedRuns.length > 0 && <RunBoxes runs={matchedRuns} sampleSize={sampleSize} />}
    </div>
  )
}

// ── Run boxes with tooltip ───────────────────────────────────────

function RunBoxes({ runs, sampleSize }: { runs: IndexEntry[]; sampleSize: number }) {
  const [tooltip, setTooltip] = useState<{ run: IndexEntry; x: number; y: number } | null>(null)

  return (
    <div className="relative flex flex-wrap items-center gap-1">
      <span className="mr-1 text-xs text-gray-500 dark:text-gray-400">Runs:</span>
      {runs.map((run, i) => {
        const inSample = i < sampleSize
        const passed = run.tests.tests_total > 0 && run.tests.tests_passed === run.tests.tests_total
        const failed = run.status === 'container_died' || run.status === 'cancelled'

        return (
          <a
            key={run.run_id}
            href={`/runs/${run.run_id}`}
            target="_blank"
            rel="noopener noreferrer"
            onMouseEnter={(e) => {
              const rect = e.currentTarget.getBoundingClientRect()
              setTooltip({ run, x: rect.left + rect.width / 2, y: rect.top })
            }}
            onMouseLeave={() => setTooltip(null)}
            className={clsx(
              'relative size-4 shrink-0 rounded-xs',
              !inSample && 'opacity-30',
              inSample && 'ring-1 ring-inset ring-black/10 dark:ring-white/10',
            )}
            style={{
              backgroundColor: failed
                ? '#6b7280'
                : passed
                  ? '#22c55e'
                  : '#eab308',
            }}
          >
            {failed && (
              <svg className="absolute inset-0 size-4 text-red-500" viewBox="0 0 16 16" fill="none">
                <path d="M3 3l10 10M3 13L13 3" stroke="currentColor" strokeWidth="1.5" />
              </svg>
            )}
          </a>
        )
      })}

      {tooltip && (() => {
        const stats = getIndexAggregatedStats(tooltip.run)
        const mgas = stats.gasUsedDuration > 0 ? (stats.gasUsed * 1000) / stats.gasUsedDuration : undefined
        return (
          <div
            className="pointer-events-none fixed z-50 rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-700"
            style={{ left: tooltip.x, top: tooltip.y - 8, transform: 'translate(-50%, -100%)' }}
          >
            <div className="flex flex-col gap-1">
              <div className="font-medium">{formatTimestamp(tooltip.run.timestamp)}</div>
              {(tooltip.run.status === 'container_died' || tooltip.run.status === 'cancelled') && (
                <div className="font-medium text-red-600 dark:text-red-400">
                  {tooltip.run.status === 'container_died' ? 'Container Died' : 'Cancelled'}
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
              <div className="text-gray-400 dark:text-gray-500">Click to open run details</div>
            </div>
          </div>
        )
      })()}
    </div>
  )
}
