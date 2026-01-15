import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from '@headlessui/react'
import clsx from 'clsx'

interface RunFiltersProps {
  clients: string[]
  selectedClient: string | undefined
  onClientChange: (client: string | undefined) => void
  sortOrder: 'newest' | 'oldest'
  onSortChange: (sort: 'newest' | 'oldest') => void
}

export function RunFilters({ clients, selectedClient, onClientChange, sortOrder, onSortChange }: RunFiltersProps) {
  return (
    <div className="flex flex-wrap items-center gap-4">
      <div className="flex items-center gap-2">
        <label className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Client:</label>
        <Listbox value={selectedClient ?? ''} onChange={(v) => onClientChange(v || undefined)}>
          <div className="relative">
            <ListboxButton className="relative w-40 cursor-pointer rounded-sm bg-white py-2 pr-10 pl-3 text-left text-sm/6 shadow-xs ring-1 ring-gray-300 ring-inset focus:outline-hidden focus:ring-2 focus:ring-blue-600 dark:bg-gray-800 dark:ring-gray-600 dark:focus:ring-blue-500">
              <span className="block truncate">{selectedClient ?? 'All clients'}</span>
              <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2">
                <svg className="size-5 text-gray-400" viewBox="0 0 20 20" fill="currentColor">
                  <path
                    fillRule="evenodd"
                    d="M10 3a.75.75 0 01.55.24l3.25 3.5a.75.75 0 11-1.1 1.02L10 4.852 7.3 7.76a.75.75 0 01-1.1-1.02l3.25-3.5A.75.75 0 0110 3zm-3.76 9.2a.75.75 0 011.06.04l2.7 2.908 2.7-2.908a.75.75 0 111.1 1.02l-3.25 3.5a.75.75 0 01-1.1 0l-3.25-3.5a.75.75 0 01.04-1.06z"
                    clipRule="evenodd"
                  />
                </svg>
              </span>
            </ListboxButton>
            <ListboxOptions className="absolute z-10 mt-1 max-h-60 w-full overflow-auto rounded-sm bg-white py-1 text-base shadow-sm ring-1 ring-black/5 focus:outline-hidden dark:bg-gray-800 dark:ring-gray-700">
              <ListboxOption
                value=""
                className={({ active }) =>
                  clsx(
                    'relative cursor-pointer py-2 pr-9 pl-3 select-none',
                    active ? 'bg-blue-600 text-white' : 'text-gray-900 dark:text-gray-100',
                  )
                }
              >
                All clients
              </ListboxOption>
              {clients.map((client) => (
                <ListboxOption
                  key={client}
                  value={client}
                  className={({ active }) =>
                    clsx(
                      'relative cursor-pointer py-2 pr-9 pl-3 select-none',
                      active ? 'bg-blue-600 text-white' : 'text-gray-900 dark:text-gray-100',
                    )
                  }
                >
                  {client}
                </ListboxOption>
              ))}
            </ListboxOptions>
          </div>
        </Listbox>
      </div>

      <div className="flex items-center gap-2">
        <label className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Sort:</label>
        <Listbox value={sortOrder} onChange={onSortChange}>
          <div className="relative">
            <ListboxButton className="relative w-32 cursor-pointer rounded-sm bg-white py-2 pr-10 pl-3 text-left text-sm/6 shadow-xs ring-1 ring-gray-300 ring-inset focus:outline-hidden focus:ring-2 focus:ring-blue-600 dark:bg-gray-800 dark:ring-gray-600 dark:focus:ring-blue-500">
              <span className="block truncate">{sortOrder === 'newest' ? 'Newest first' : 'Oldest first'}</span>
              <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2">
                <svg className="size-5 text-gray-400" viewBox="0 0 20 20" fill="currentColor">
                  <path
                    fillRule="evenodd"
                    d="M10 3a.75.75 0 01.55.24l3.25 3.5a.75.75 0 11-1.1 1.02L10 4.852 7.3 7.76a.75.75 0 01-1.1-1.02l3.25-3.5A.75.75 0 0110 3zm-3.76 9.2a.75.75 0 011.06.04l2.7 2.908 2.7-2.908a.75.75 0 111.1 1.02l-3.25 3.5a.75.75 0 01-1.1 0l-3.25-3.5a.75.75 0 01.04-1.06z"
                    clipRule="evenodd"
                  />
                </svg>
              </span>
            </ListboxButton>
            <ListboxOptions className="absolute z-10 mt-1 max-h-60 w-full overflow-auto rounded-sm bg-white py-1 text-base shadow-sm ring-1 ring-black/5 focus:outline-hidden dark:bg-gray-800 dark:ring-gray-700">
              <ListboxOption
                value="newest"
                className={({ active }) =>
                  clsx(
                    'relative cursor-pointer py-2 pr-9 pl-3 select-none',
                    active ? 'bg-blue-600 text-white' : 'text-gray-900 dark:text-gray-100',
                  )
                }
              >
                Newest first
              </ListboxOption>
              <ListboxOption
                value="oldest"
                className={({ active }) =>
                  clsx(
                    'relative cursor-pointer py-2 pr-9 pl-3 select-none',
                    active ? 'bg-blue-600 text-white' : 'text-gray-900 dark:text-gray-100',
                  )
                }
              >
                Oldest first
              </ListboxOption>
            </ListboxOptions>
          </div>
        </Listbox>
      </div>
    </div>
  )
}
