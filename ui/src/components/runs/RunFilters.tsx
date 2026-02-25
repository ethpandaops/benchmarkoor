import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from '@headlessui/react'
import clsx from 'clsx'
import { JDenticon } from '@/components/shared/JDenticon'

export type TestStatusFilter = 'all' | 'passing' | 'failing'

interface RunFiltersProps {
  clients: string[]
  selectedClient: string | undefined
  onClientChange: (client: string | undefined) => void
  images: string[]
  selectedImage: string | undefined
  onImageChange: (image: string | undefined) => void
  selectedStatus: TestStatusFilter
  onStatusChange: (status: TestStatusFilter) => void
  suites?: { hash: string; name?: string }[]
  selectedSuite?: string | undefined
  onSuiteChange?: (suite: string | undefined) => void
  strategies?: string[]
  selectedStrategy?: string | undefined
  onStrategyChange?: (strategy: string | undefined) => void
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

function FilterDropdown<T extends string>({
  label,
  value,
  onChange,
  options,
  allLabel,
  width = 'w-40',
}: {
  label: string
  value: T | ''
  onChange: (value: T | '') => void
  options: { value: T | ''; label: string; icon?: React.ReactNode }[]
  allLabel: string
  width?: string
}) {
  const selectedOption = options.find((o) => o.value === value)
  return (
    <div className="flex flex-col gap-1">
      <label className="text-sm/5 font-medium text-gray-700 dark:text-gray-300">{label}</label>
      <Listbox value={value} onChange={onChange}>
        <div className="relative">
          <ListboxButton
            className={clsx(
              'relative cursor-pointer rounded-sm bg-white py-2 pr-10 pl-3 text-left text-sm/6 text-gray-900 shadow-xs ring-1 ring-gray-300 ring-inset focus:outline-hidden focus:ring-2 focus:ring-blue-600 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-600 dark:focus:ring-blue-500',
              width,
            )}
          >
            <span className="flex items-center gap-1.5 truncate">
              {selectedOption?.icon}
              {selectedOption?.label ?? allLabel}
            </span>
            <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2">
              <ChevronIcon />
            </span>
          </ListboxButton>
          <ListboxOptions className="absolute z-10 mt-1 max-h-60 w-full min-w-max overflow-auto rounded-sm bg-white py-1 text-base shadow-sm ring-1 ring-black/5 focus:outline-hidden dark:bg-gray-800 dark:ring-gray-700">
            {options.map((option) => (
              <ListboxOption
                key={option.value}
                value={option.value}
                className={({ active }) =>
                  clsx(
                    'relative cursor-pointer py-2 pr-9 pl-3 select-none',
                    active ? 'bg-blue-600 text-white' : 'text-gray-900 dark:text-gray-100',
                  )
                }
              >
                <span className="flex items-center gap-1.5">
                  {option.icon}
                  {option.label}
                </span>
              </ListboxOption>
            ))}
          </ListboxOptions>
        </div>
      </Listbox>
    </div>
  )
}

export function RunFilters({
  clients,
  selectedClient,
  onClientChange,
  images,
  selectedImage,
  onImageChange,
  selectedStatus,
  onStatusChange,
  suites,
  selectedSuite,
  onSuiteChange,
  strategies,
  selectedStrategy,
  onStrategyChange,
}: RunFiltersProps) {
  const clientOptions = [{ value: '' as const, label: 'All clients' }, ...clients.map((c) => ({ value: c, label: c }))]
  const imageOptions = [{ value: '' as const, label: 'All images' }, ...images.map((i) => ({ value: i, label: i }))]
  const statusOptions: { value: TestStatusFilter | ''; label: string }[] = [
    { value: 'all', label: 'All runs' },
    { value: 'passing', label: 'Passing only' },
    { value: 'failing', label: 'Has failures' },
  ]
  const suiteOptions = suites ? [{ value: '' as const, label: 'All suites' }, ...suites.map((s) => ({ value: s.hash, label: s.name ? `${s.name} (${s.hash.slice(0, 4)})` : s.hash, icon: <JDenticon value={s.hash} size={16} /> }))] : []
  const strategyOptions = strategies ? [{ value: '' as const, label: 'All strategies' }, ...strategies.map((s) => ({ value: s, label: s }))] : []

  return (
    <div className="flex flex-wrap items-center gap-4">
      <FilterDropdown
        label="Client"
        value={selectedClient ?? ''}
        onChange={(v) => onClientChange(v || undefined)}
        options={clientOptions}
        allLabel="All clients"
      />
      <FilterDropdown
        label="Image"
        value={selectedImage ?? ''}
        onChange={(v) => onImageChange(v || undefined)}
        options={imageOptions}
        allLabel="All images"
        width="w-64"
      />
      {suites && onSuiteChange && (
        <FilterDropdown
          label="Suite"
          value={selectedSuite ?? ''}
          onChange={(v) => onSuiteChange(v || undefined)}
          options={suiteOptions}
          allLabel="All suites"
          width="w-44"
        />
      )}
      {strategies && strategies.length > 0 && onStrategyChange && (
        <FilterDropdown
          label="Strategy"
          value={selectedStrategy ?? ''}
          onChange={(v) => onStrategyChange(v || undefined)}
          options={strategyOptions}
          allLabel="All strategies"
          width="w-52"
        />
      )}
      <FilterDropdown
        label="Status"
        value={selectedStatus}
        onChange={(v) => onStatusChange((v || 'all') as TestStatusFilter)}
        options={statusOptions}
        allLabel="All runs"
        width="w-36"
      />
    </div>
  )
}
