import clsx from 'clsx'
import { Plus, Trash2 } from 'lucide-react'
import { JDenticon } from '@/components/shared/JDenticon'
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
            max={20}
            value={sampleSize}
            onChange={(e) => onSampleSizeChange(Math.max(1, Math.min(20, parseInt(e.target.value, 10) || 5)))}
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
    </div>
  )
}
