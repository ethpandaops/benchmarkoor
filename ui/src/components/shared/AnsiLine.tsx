import type React from 'react'
import { parseAnsiCodes, type AnsiStyle } from '@/utils/ansi'

/**
 * AnsiLine renders a single line of text with embedded ANSI escape
 * codes (\x1b[...m) interpreted as inline CSS styles. Unknown codes
 * are ignored silently so the text still shows, just unstyled.
 * Shared between FileViewerPage and the live log panel.
 */
export function AnsiLine({ content }: { content: string }) {
  if (!content) {
    return <span>&nbsp;</span>
  }

  const parts: React.ReactNode[] = []
  let lastIndex = 0
  let currentStyle: AnsiStyle = {}
  let partKey = 0

  // eslint-disable-next-line no-control-regex
  const regex = /\x1b\[([0-9;]*)m/g
  let match

  while ((match = regex.exec(content)) !== null) {
    if (match.index > lastIndex) {
      const text = content.slice(lastIndex, match.index)
      if (text) {
        parts.push(
          <span key={partKey++} style={currentStyle}>
            {text}
          </span>,
        )
      }
    }

    const codesStr = match[1]
    if (codesStr === '' || codesStr === '0') {
      currentStyle = {}
    } else {
      const codes = codesStr.split(';').map(Number)
      const newStyle = parseAnsiCodes(codes)
      currentStyle = { ...currentStyle, ...newStyle }
      if (codes.includes(0)) {
        currentStyle = parseAnsiCodes(codes.filter((c) => c !== 0))
      }
    }

    lastIndex = regex.lastIndex
  }

  if (lastIndex < content.length) {
    const text = content.slice(lastIndex)
    if (text) {
      parts.push(
        <span key={partKey++} style={currentStyle}>
          {text}
        </span>,
      )
    }
  }

  return <>{parts.length > 0 ? parts : content}</>
}
