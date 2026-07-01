import clsx from 'clsx'

interface SegmentedControlOption<T extends string> {
  value: T
  label: string
}

interface SegmentedControlProps<T extends string> {
  value: T
  onChange: (value: T) => void
  options: SegmentedControlOption<T>[]
  ariaLabel?: string
  className?: string
}

// SegmentedControl is a compact single-select button group (e.g. a setup |
// test | sum toggle). Values are string literals; the active option is filled.
export function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
  ariaLabel,
  className,
}: SegmentedControlProps<T>) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={clsx('inline-flex rounded-sm border border-gray-300 dark:border-gray-600', className)}
    >
      {options.map((option, index) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
          className={clsx(
            'px-3 py-1 text-xs/5 font-medium transition-colors',
            index > 0 && 'border-l border-gray-300 dark:border-gray-600',
            value === option.value
              ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
              : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}
