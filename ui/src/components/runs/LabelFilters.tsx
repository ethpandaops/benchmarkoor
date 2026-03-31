import { useMemo, useState, useEffect, useRef } from 'react'
import clsx from 'clsx'
import { Plus, X } from 'lucide-react'
import type { IndexEntry } from '@/api/types'
import type { LabelFilters as LabelFiltersType } from './labelFilterUtils'

interface LabelFiltersProps {
  entries: IndexEntry[]
  filters: LabelFiltersType
  onChange: (filters: LabelFiltersType) => void
}

export function LabelFilters({ entries, filters, onChange }: LabelFiltersProps) {
  const [keyDropdownOpen, setKeyDropdownOpen] = useState(false)
  const [valueDropdownKey, setValueDropdownKey] = useState<string | null>(null)
  const keyRef = useRef<HTMLDivElement>(null)
  const valueRef = useRef<HTMLDivElement>(null)

  // Close dropdowns on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (keyDropdownOpen && keyRef.current && !keyRef.current.contains(e.target as Node)) {
        setKeyDropdownOpen(false)
      }
      if (valueDropdownKey && valueRef.current && !valueRef.current.contains(e.target as Node)) {
        setValueDropdownKey(null)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [keyDropdownOpen, valueDropdownKey])

  const { allKeys, valuesForKey } = useMemo(() => {
    const valMap = new Map<string, Set<string>>()
    for (const entry of entries) {
      if (entry.metadata) {
        for (const [key, value] of Object.entries(entry.metadata)) {
          let set = valMap.get(key)
          if (!set) {
            set = new Set()
            valMap.set(key, set)
          }
          set.add(value)
        }
      }
    }
    const keys = Array.from(valMap.keys()).sort()
    const values = new Map<string, string[]>()
    for (const [key, set] of valMap) {
      values.set(key, Array.from(set).sort())
    }
    return { allKeys: keys, valuesForKey: values }
  }, [entries])

  if (allKeys.length === 0) return null

  const availableKeys = allKeys.filter((k) => !filters.has(k) && k !== valueDropdownKey)

  const toggleValue = (key: string, value: string) => {
    const next = new Map(filters)
    const values = new Set(next.get(key) ?? [])
    if (values.has(value)) {
      values.delete(value)
      if (values.size === 0) {
        next.delete(key)
      } else {
        next.set(key, values)
      }
    } else {
      values.add(value)
      next.set(key, values)
    }
    onChange(next)
  }

  const removeFilter = (key: string) => {
    const next = new Map(filters)
    next.delete(key)
    onChange(next)
    if (valueDropdownKey === key) setValueDropdownKey(null)
  }

  const addKey = (key: string) => {
    setKeyDropdownOpen(false)
    setValueDropdownKey(key)
  }

  // Build the list of chips: active filters + pending key (not yet in filters)
  const chipKeys = Array.from(filters.keys())
  if (valueDropdownKey && !filters.has(valueDropdownKey)) {
    chipKeys.push(valueDropdownKey)
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {chipKeys.map((key) => {
        const values = filters.get(key)
        const isPending = !values
        return (
          <div key={key} className="relative" ref={valueDropdownKey === key ? valueRef : undefined}>
            <div
              role="button"
              tabIndex={0}
              onClick={() => setValueDropdownKey(valueDropdownKey === key ? null : key)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  setValueDropdownKey(valueDropdownKey === key ? null : key)
                }
              }}
              className={clsx(
                'flex cursor-pointer items-center gap-1.5 rounded-xs border px-2 py-1 text-xs/5 font-medium transition-colors',
                isPending
                  ? 'border-dashed border-blue-300 bg-blue-50/50 text-blue-500 dark:border-blue-700 dark:bg-blue-900/20 dark:text-blue-400'
                  : 'border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300 dark:hover:bg-blue-900/50',
              )}
            >
              <span className="font-semibold">{key}</span>
              {values && values.size > 0 && (
                <>
                  <span>=</span>
                  <span>{Array.from(values).join(', ')}</span>
                </>
              )}
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  if (isPending) {
                    setValueDropdownKey(null)
                  } else {
                    removeFilter(key)
                  }
                }}
                className="ml-0.5 rounded-xs p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800"
              >
                <X className="size-3" />
              </button>
            </div>

            {/* Value multi-select dropdown */}
            {valueDropdownKey === key && (
              <div className="absolute top-full left-0 z-50 mt-1 max-h-64 min-w-48 overflow-y-auto rounded-xs border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-800">
                {(valuesForKey.get(key) ?? []).map((val) => {
                  const selected = values?.has(val) ?? false
                  return (
                    <button
                      key={val}
                      onClick={() => toggleValue(key, val)}
                      className={clsx(
                        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm/6 transition-colors',
                        selected
                          ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                          : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700',
                      )}
                    >
                      <span
                        className={clsx(
                          'flex size-4 shrink-0 items-center justify-center rounded-xs border text-xs/3',
                          selected
                            ? 'border-blue-500 bg-blue-500 text-white dark:border-blue-400 dark:bg-blue-400'
                            : 'border-gray-300 dark:border-gray-600',
                        )}
                      >
                        {selected && '✓'}
                      </span>
                      {val}
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        )
      })}

      {/* Add filter button */}
      {availableKeys.length > 0 && (
        <div className="relative" ref={keyRef}>
          <button
            onClick={() => {
              setKeyDropdownOpen(!keyDropdownOpen)
              setValueDropdownKey(null)
            }}
            className="flex items-center gap-1 rounded-xs border border-dashed border-gray-300 px-2 py-1 text-xs/5 text-gray-500 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-gray-600 dark:text-gray-400 dark:hover:border-gray-500 dark:hover:text-gray-300"
          >
            <Plus className="size-3" />
            Label
          </button>

          {keyDropdownOpen && (
            <div className="absolute top-full left-0 z-50 mt-1 min-w-36 overflow-hidden rounded-xs border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-800">
              {availableKeys.map((key) => (
                <button
                  key={key}
                  onClick={() => addKey(key)}
                  className="flex w-full px-3 py-1.5 text-left text-sm/6 text-gray-700 transition-colors hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700"
                >
                  {key}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
