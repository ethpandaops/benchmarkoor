import { useMemo } from 'react'
import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from '@headlessui/react'
import clsx from 'clsx'
import { Plus, X } from 'lucide-react'
import type { IndexEntry } from '@/api/types'
import type { LabelFilter } from './labelFilterUtils'

interface LabelFiltersProps {
  entries: IndexEntry[]
  filters: LabelFilter[]
  onChange: (filters: LabelFilter[]) => void
}

function ChevronIcon() {
  return (
    <svg className="size-5 text-gray-400" viewBox="0 0 20 20" fill="currentColor">
      <path
        fillRule="evenodd"
        d="M10 3a.75.75 0 01.55.24l3.25 3.5a.75.75 0 11-1.1 1.02L10 4.852 7.3 7.76a.75.75 0 01-1.1-1.02l3.25-3.5A.75.75 0 0110 3zm-3.76 9.2a.75.75 0 011.06.04l2.7 2.908 2.7-2.908a.75.75 0 111.1 1.02l-3.25 3.5a.75.75 0 01-1.1 0l-3.25-3.5a.75.75 0 01.04-1.06z"
        clipRule="evenodd"
      />
    </svg>
  )
}

export function LabelFilters({ entries, filters, onChange }: LabelFiltersProps) {
  const allKeys = useMemo(() => {
    const keySet = new Set<string>()
    for (const entry of entries) {
      if (entry.metadata) {
        for (const key of Object.keys(entry.metadata)) {
          keySet.add(key)
        }
      }
    }
    return Array.from(keySet).sort()
  }, [entries])

  const valuesForKey = useMemo(() => {
    const map = new Map<string, Set<string>>()
    for (const entry of entries) {
      if (entry.metadata) {
        for (const [key, value] of Object.entries(entry.metadata)) {
          if (!map.has(key)) map.set(key, new Set())
          map.get(key)!.add(value)
        }
      }
    }
    const result = new Map<string, string[]>()
    for (const [key, values] of map) {
      result.set(key, Array.from(values).sort())
    }
    return result
  }, [entries])

  if (allKeys.length === 0) return null

  const usedKeys = new Set(filters.map((f) => f.key))

  const handleAdd = () => {
    const availableKey = allKeys.find((k) => !usedKeys.has(k))
    if (!availableKey) return
    const values = valuesForKey.get(availableKey) ?? []
    onChange([...filters, { key: availableKey, value: values[0] ?? '' }])
  }

  const handleRemove = (index: number) => {
    onChange(filters.filter((_, i) => i !== index))
  }

  const handleKeyChange = (index: number, newKey: string) => {
    const values = valuesForKey.get(newKey) ?? []
    const updated = filters.map((f, i) => (i === index ? { key: newKey, value: values[0] ?? '' } : f))
    onChange(updated)
  }

  const handleValueChange = (index: number, newValue: string) => {
    const updated = filters.map((f, i) => (i === index ? { ...f, value: newValue } : f))
    onChange(updated)
  }

  const canAdd = allKeys.some((k) => !usedKeys.has(k))

  return (
    <div className="flex flex-wrap items-end gap-2">
      {filters.map((filter, index) => {
        const availableKeys = allKeys.filter((k) => k === filter.key || !usedKeys.has(k))
        const values = valuesForKey.get(filter.key) ?? []

        return (
          <div key={index} className="flex items-end gap-1">
            <div className="flex flex-col gap-1">
              {index === 0 && <label className="text-sm/5 font-medium text-gray-700 dark:text-gray-300">Labels</label>}
              <Listbox value={filter.key} onChange={(v: string) => handleKeyChange(index, v)}>
                <div className="relative">
                  <ListboxButton className="relative w-32 cursor-pointer rounded-xs bg-white py-2 pr-10 pl-3 text-left text-sm/6 text-gray-900 shadow-xs ring-1 ring-gray-300 ring-inset focus:outline-hidden focus:ring-2 focus:ring-blue-600 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-600 dark:focus:ring-blue-500">
                    <span className="truncate">{filter.key}</span>
                    <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2">
                      <ChevronIcon />
                    </span>
                  </ListboxButton>
                  <ListboxOptions className="absolute z-10 mt-1 max-h-60 w-full min-w-max overflow-auto rounded-xs bg-white py-1 text-base shadow-xs ring-1 ring-black/5 focus:outline-hidden dark:bg-gray-800 dark:ring-gray-700">
                    {availableKeys.map((key) => (
                      <ListboxOption
                        key={key}
                        value={key}
                        className={({ active }) =>
                          clsx(
                            'relative cursor-pointer py-2 pr-9 pl-3 select-none',
                            active ? 'bg-blue-600 text-white' : 'text-gray-900 dark:text-gray-100',
                          )
                        }
                      >
                        {key}
                      </ListboxOption>
                    ))}
                  </ListboxOptions>
                </div>
              </Listbox>
            </div>

            <span className="py-2 text-sm/6 text-gray-400">=</span>

            <Listbox value={filter.value} onChange={(v: string) => handleValueChange(index, v)}>
              <div className="relative">
                <ListboxButton className="relative w-36 cursor-pointer rounded-xs bg-white py-2 pr-10 pl-3 text-left text-sm/6 text-gray-900 shadow-xs ring-1 ring-gray-300 ring-inset focus:outline-hidden focus:ring-2 focus:ring-blue-600 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-600 dark:focus:ring-blue-500">
                  <span className="truncate">{filter.value || '—'}</span>
                  <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2">
                    <ChevronIcon />
                  </span>
                </ListboxButton>
                <ListboxOptions className="absolute z-10 mt-1 max-h-60 w-full min-w-max overflow-auto rounded-xs bg-white py-1 text-base shadow-xs ring-1 ring-black/5 focus:outline-hidden dark:bg-gray-800 dark:ring-gray-700">
                  {values.map((value) => (
                    <ListboxOption
                      key={value}
                      value={value}
                      className={({ active }) =>
                        clsx(
                          'relative cursor-pointer py-2 pr-9 pl-3 select-none',
                          active ? 'bg-blue-600 text-white' : 'text-gray-900 dark:text-gray-100',
                        )
                      }
                    >
                      {value}
                    </ListboxOption>
                  ))}
                </ListboxOptions>
              </div>
            </Listbox>

            <button
              onClick={() => handleRemove(index)}
              className="rounded-xs p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-300"
            >
              <X className="size-4" />
            </button>
          </div>
        )
      })}
      {canAdd && (
        <button
          onClick={handleAdd}
          className="flex items-center gap-1 rounded-xs px-2 py-2 text-sm/6 text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/20"
        >
          <Plus className="size-4" />
          {filters.length === 0 && <span>Label filter</span>}
        </button>
      )}
    </div>
  )
}
