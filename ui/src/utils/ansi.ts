// ANSI escape-code parsing, shared between the file viewer and the
// live runner log panel. Keep this file component-free so the React
// fast-refresh plugin doesn't yell about mixed exports — the rendering
// component lives in components/shared/AnsiLine.tsx.

// eslint-disable-next-line no-control-regex
const ANSI_REGEX = /\x1b\[[0-9;]*m/g

export function hasAnsiCodes(content: string): boolean {
  // Check the first 10 lines so the call stays cheap on huge inputs.
  const first10Lines = content.split('\n').slice(0, 10).join('\n')
  return ANSI_REGEX.test(first10Lines)
}

// Palette is OneDark — reads well on the near-black backgrounds both
// consumers use.
export const ANSI_COLORS: Record<number, string> = {
  30: '#1e1e1e', // black
  31: '#e06c75', // red
  32: '#98c379', // green
  33: '#e5c07b', // yellow
  34: '#61afef', // blue
  35: '#c678dd', // magenta
  36: '#56b6c2', // cyan
  37: '#abb2bf', // white
  90: '#5c6370', // bright black (gray)
  91: '#e06c75',
  92: '#98c379',
  93: '#e5c07b',
  94: '#61afef',
  95: '#c678dd',
  96: '#56b6c2',
  97: '#ffffff',
}

export const ANSI_BG_COLORS: Record<number, string> = {
  40: '#1e1e1e',
  41: '#e06c75',
  42: '#98c379',
  43: '#e5c07b',
  44: '#61afef',
  45: '#c678dd',
  46: '#56b6c2',
  47: '#abb2bf',
  100: '#5c6370',
  101: '#e06c75',
  102: '#98c379',
  103: '#e5c07b',
  104: '#61afef',
  105: '#c678dd',
  106: '#56b6c2',
  107: '#ffffff',
}

export interface AnsiStyle {
  color?: string
  backgroundColor?: string
  fontWeight?: string
  fontStyle?: string
  textDecoration?: string
}

export function parseAnsiCodes(codes: number[]): AnsiStyle {
  const style: AnsiStyle = {}

  for (const code of codes) {
    if (code === 0) {
      return {}
    } else if (code === 1) {
      style.fontWeight = 'bold'
    } else if (code === 3) {
      style.fontStyle = 'italic'
    } else if (code === 4) {
      style.textDecoration = 'underline'
    } else if (code >= 30 && code <= 37) {
      style.color = ANSI_COLORS[code]
    } else if (code >= 40 && code <= 47) {
      style.backgroundColor = ANSI_BG_COLORS[code]
    } else if (code >= 90 && code <= 97) {
      style.color = ANSI_COLORS[code]
    } else if (code >= 100 && code <= 107) {
      style.backgroundColor = ANSI_BG_COLORS[code]
    }
  }

  return style
}
