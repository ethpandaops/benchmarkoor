import { useState, type InputHTMLAttributes } from 'react'

type FilterInputProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange'> & {
  value: string
  onValueChange: (value: string) => void
}

/**
 * A controlled text input that keeps local state to avoid cursor-jump issues
 * when the canonical value lives in URL search params or other async state.
 *
 * Tracks the last-seen prop value as state. When the prop changes from an
 * external source (e.g. browser back/forward), local state re-syncs. During
 * typing, the prop echoes back our own value so the condition is false and
 * local state is left untouched — preserving the cursor position.
 */
export function FilterInput({ value, onValueChange, ...rest }: FilterInputProps) {
  const [local, setLocal] = useState(value)
  const [prevValue, setPrevValue] = useState(value)

  if (value !== prevValue) {
    setPrevValue(value)

    if (value !== local) {
      setLocal(value)
    }
  }

  return (
    <input
      {...rest}
      type="text"
      value={local}
      onChange={(e) => {
        setLocal(e.target.value)
        onValueChange(e.target.value)
      }}
    />
  )
}
