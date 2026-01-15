import { useMemo } from 'react'
import * as jdenticon from 'jdenticon'

interface JDenticonProps {
  value: string
  size?: number
  className?: string
}

export function JDenticon({ value, size = 32, className }: JDenticonProps) {
  const svg = useMemo(() => {
    return jdenticon.toSvg(value, size)
  }, [value, size])

  return (
    <span
      className={className}
      style={{ width: size, height: size, display: 'inline-block' }}
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
