import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { SuiteTest } from '@/api/types'
import { getGroupedOpcodes, getCategoryColor } from '@/utils/opcodeCategories'
import type { CategorySpan, GroupedResult } from '@/utils/opcodeCategories'

interface OpcodeHeatmapProps {
  tests: SuiteTest[]
  onTestClick?: (testIndex: number) => void
}

const CELL_SIZE = 16
const ROW_HEIGHT = 20
const HEADER_HEIGHT_COLLAPSED = 90
const ROW_LABEL_WIDTH = 50
const MAX_HEIGHT = 600
const BORDER_COLOR_LIGHT = '#e5e7eb'
const BORDER_COLOR_DARK = '#374151'
const BG_LIGHT = '#ffffff'
const BG_DARK = '#1f2937'
const TEXT_LIGHT = '#6b7280'
const TEXT_DARK = '#9ca3af'
const HEADER_BG_LIGHT = '#f9fafb'
const HEADER_BG_DARK = '#1f2937'
const SEPARATOR_LIGHT = '#d1d5db'
const SEPARATOR_DARK = '#4b5563'

function logRatio(count: number, max: number): number {
  if (count <= 0 || max <= 0) return 0
  return Math.log1p(count) / Math.log1p(max)
}

const COLORS_LIGHT = ['transparent', '#eff6ff', '#dbeafe', '#bfdbfe', '#93c5fd', '#818cf8', '#7c3aed', '#6d28d9', '#5b21b6']
const COLORS_DARK = ['transparent', 'rgba(59,130,246,0.15)', 'rgba(59,130,246,0.3)', 'rgba(99,102,241,0.4)', 'rgba(99,102,241,0.55)', 'rgba(139,92,246,0.6)', 'rgba(139,92,246,0.75)', 'rgba(168,85,247,0.85)', 'rgba(168,85,247,1)']

function getColorIndex(ratio: number): number {
  if (ratio === 0) return 0
  if (ratio <= 0.125) return 1
  if (ratio <= 0.25) return 2
  if (ratio <= 0.375) return 3
  if (ratio <= 0.5) return 4
  if (ratio <= 0.625) return 5
  if (ratio <= 0.75) return 6
  if (ratio <= 0.875) return 7
  return 8
}

function getCellColor(ratio: number, isDark: boolean): string {
  const colors = isDark ? COLORS_DARK : COLORS_LIGHT
  return colors[getColorIndex(ratio)]
}

const LEGEND_STEPS = [0.125, 0.25, 0.375, 0.5, 0.625, 0.75, 0.875, 1.0]

function HeatmapLegend({ isDark }: { isDark: boolean }) {
  return (
    <div className="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
      <span>Low</span>
      <div className="flex">
        {LEGEND_STEPS.map((ratio) => (
          <div
            key={ratio}
            className="border border-gray-200 dark:border-gray-700"
            style={{ width: 20, height: 12, backgroundColor: getCellColor(ratio, isDark) }}
          />
        ))}
      </div>
      <span>High</span>
      <span className="text-gray-400 dark:text-gray-500">(log scale, per-opcode)</span>
    </div>
  )
}

function getInitials(name: string): string {
  // Use first letter of each word, or first 2 chars if single word
  const words = name.split(/\s+/)
  if (words.length > 1) return words.map((w) => w[0]).join('').toUpperCase()
  return name.slice(0, 2)
}

function fitLabel(ctx: CanvasRenderingContext2D, name: string, maxWidth: number): string {
  if (ctx.measureText(name).width <= maxWidth) return name
  const initials = getInitials(name)
  if (ctx.measureText(initials).width <= maxWidth) return initials
  return ''
}

/** Compute expanded header height: category row + subcategory row (if any) + opcode labels */
function computeExpandedHeaderHeight(categorySpans: CategorySpan[]): number {
  const hasSubcategories = categorySpans.some((s) => s.subcategories && s.subcategories.length > 0)
  // category row + optional subcategory row + opcode label area
  return ROW_HEIGHT + (hasSubcategories ? ROW_HEIGHT : 0) + 90
}

type SortDir = 'asc' | 'desc'

interface HeatmapCanvasProps {
  filteredTests: { test: SuiteTest; index: number }[]
  columns: string[]
  maxPerColumn: Record<string, number>
  isDark: boolean
  maxHeight?: number
  expanded: boolean
  categorySpans: CategorySpan[]
  getCount: (test: SuiteTest, col: string) => number
  sortCol: string | null
  onSortChange: (col: string) => void
  onTestClick?: (testIndex: number) => void
}

function HeatmapCanvas({ filteredTests, columns, maxPerColumn, isDark, maxHeight, expanded, categorySpans, getCount, sortCol, onSortChange, onTestClick }: HeatmapCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const [tooltip, setTooltip] = useState<{ lines: { text: string; bold?: boolean }[]; x: number; y: number } | null>(null)
  const hoverRef = useRef<{ row: number; col: number } | null>(null)
  const rafRef = useRef(0)

  const headerHeight = expanded ? computeExpandedHeaderHeight(categorySpans) : HEADER_HEIGHT_COLLAPSED
  const totalWidth = ROW_LABEL_WIDTH + columns.length * CELL_SIZE
  const totalHeight = headerHeight + filteredTests.length * CELL_SIZE

  const colorGrid = useMemo(() => {
    const colors = isDark ? COLORS_DARK : COLORS_LIGHT
    const grid = new Uint8Array(filteredTests.length * columns.length)
    for (let row = 0; row < filteredTests.length; row++) {
      const test = filteredTests[row].test
      for (let col = 0; col < columns.length; col++) {
        const count = getCount(test, columns[col])
        const max = maxPerColumn[columns[col]] ?? 1
        grid[row * columns.length + col] = getColorIndex(logRatio(count, max))
      }
    }
    return { grid, colors }
  }, [filteredTests, columns, maxPerColumn, isDark, getCount])

  const draw = useCallback(() => {
    const canvas = canvasRef.current
    const container = containerRef.current
    if (!canvas || !container) return

    const dpr = window.devicePixelRatio || 1
    const viewW = container.clientWidth
    const viewH = container.clientHeight
    const scrollX = container.scrollLeft
    const scrollY = container.scrollTop

    canvas.width = viewW * dpr
    canvas.height = viewH * dpr
    canvas.style.width = `${viewW}px`
    canvas.style.height = `${viewH}px`

    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.scale(dpr, dpr)

    const bg = isDark ? BG_DARK : BG_LIGHT
    const hdrBg = isDark ? HEADER_BG_DARK : HEADER_BG_LIGHT
    const borderColor = isDark ? BORDER_COLOR_DARK : BORDER_COLOR_LIGHT
    const textColor = isDark ? TEXT_DARK : TEXT_LIGHT
    const separatorColor = isDark ? SEPARATOR_DARK : SEPARATOR_LIGHT
    const { grid, colors } = colorGrid

    ctx.fillStyle = bg
    ctx.fillRect(0, 0, viewW, viewH)

    const firstRow = Math.max(0, Math.floor((scrollY - headerHeight) / CELL_SIZE))
    const lastRow = Math.min(filteredTests.length - 1, Math.ceil((scrollY + viewH - headerHeight) / CELL_SIZE))
    const firstCol = Math.max(0, Math.floor((scrollX - ROW_LABEL_WIDTH) / CELL_SIZE))
    const lastCol = Math.min(columns.length - 1, Math.ceil((scrollX + viewW - ROW_LABEL_WIDTH) / CELL_SIZE))

    // Draw cells
    for (let row = firstRow; row <= lastRow; row++) {
      const cy = headerHeight + row * CELL_SIZE - scrollY
      for (let col = firstCol; col <= lastCol; col++) {
        const cx = ROW_LABEL_WIDTH + col * CELL_SIZE - scrollX
        const colorIdx = grid[row * columns.length + col]
        if (colorIdx > 0) {
          ctx.fillStyle = colors[colorIdx]
          ctx.fillRect(cx, cy, CELL_SIZE, CELL_SIZE)
        }
      }
    }

    // Draw cell borders
    ctx.strokeStyle = borderColor
    ctx.lineWidth = 0.5
    ctx.beginPath()
    for (let row = firstRow; row <= lastRow + 1; row++) {
      const y = headerHeight + row * CELL_SIZE - scrollY
      const x0 = ROW_LABEL_WIDTH + firstCol * CELL_SIZE - scrollX
      const x1 = ROW_LABEL_WIDTH + (lastCol + 1) * CELL_SIZE - scrollX
      ctx.moveTo(x0, y)
      ctx.lineTo(x1, y)
    }
    for (let col = firstCol; col <= lastCol + 1; col++) {
      const x = ROW_LABEL_WIDTH + col * CELL_SIZE - scrollX
      const y0 = headerHeight + firstRow * CELL_SIZE - scrollY
      const y1 = headerHeight + (lastRow + 1) * CELL_SIZE - scrollY
      ctx.moveTo(x, y0)
      ctx.lineTo(x, y1)
    }
    ctx.stroke()

    // Hover highlight
    const hover = hoverRef.current
    if (hover && hover.row >= firstRow && hover.row <= lastRow && hover.col >= firstCol && hover.col <= lastCol) {
      const hx = ROW_LABEL_WIDTH + hover.col * CELL_SIZE - scrollX
      const hy = headerHeight + hover.row * CELL_SIZE - scrollY
      ctx.strokeStyle = isDark ? '#f9fafb' : '#111827'
      ctx.lineWidth = 2
      ctx.strokeRect(hx, hy, CELL_SIZE, CELL_SIZE)
    }

    // Header background
    ctx.fillStyle = hdrBg
    ctx.fillRect(0, 0, viewW, headerHeight)
    // Row label column background
    ctx.fillStyle = bg
    ctx.fillRect(0, headerHeight, ROW_LABEL_WIDTH, viewH - headerHeight)
    // Corner
    ctx.fillStyle = hdrBg
    ctx.fillRect(0, 0, ROW_LABEL_WIDTH, headerHeight)

    // Header border
    ctx.strokeStyle = borderColor
    ctx.lineWidth = 1
    ctx.beginPath()
    ctx.moveTo(0, headerHeight)
    ctx.lineTo(viewW, headerHeight)
    ctx.moveTo(ROW_LABEL_WIDTH, 0)
    ctx.lineTo(ROW_LABEL_WIDTH, viewH)
    ctx.stroke()

    // Draw column labels (rotated) — clipped to opcode label area only
    const hasSubcats = expanded && categorySpans.some((s) => s.subcategories && s.subcategories.length > 0)
    const opcodeClipTop = expanded ? ROW_HEIGHT + (hasSubcats ? ROW_HEIGHT : 0) : 0
    ctx.save()
    ctx.beginPath()
    ctx.rect(ROW_LABEL_WIDTH, opcodeClipTop, viewW - ROW_LABEL_WIDTH, headerHeight - opcodeClipTop)
    ctx.clip()
    ctx.textBaseline = 'middle'
    for (let col = firstCol; col <= lastCol; col++) {
      const isSorted = columns[col] === sortCol
      ctx.fillStyle = isSorted ? (isDark ? '#f9fafb' : '#111827') : textColor
      ctx.font = isSorted ? 'bold 10px monospace' : '10px monospace'
      const cx = ROW_LABEL_WIDTH + col * CELL_SIZE - scrollX + CELL_SIZE / 2
      ctx.save()
      ctx.translate(cx, headerHeight - 4)
      ctx.rotate(-Math.PI / 2)
      ctx.textAlign = 'left'
      ctx.fillText(columns[col], 0, 0)
      ctx.restore()
    }

    // Restore opcode label clip, start new clip for category headers
    ctx.restore()
    ctx.save()
    ctx.beginPath()
    ctx.rect(ROW_LABEL_WIDTH, 0, viewW - ROW_LABEL_WIDTH, headerHeight)
    ctx.clip()

    // Draw category and subcategory headers in expanded mode
    if (expanded && categorySpans.length > 0) {
      const catRowY = ROW_HEIGHT / 2
      const subRowY = ROW_HEIGHT + ROW_HEIGHT / 2

      // Category row
      ctx.font = 'bold 10px sans-serif'
      ctx.textBaseline = 'middle'
      ctx.textAlign = 'center'

      // Horizontal separator below category row (full width)
      ctx.strokeStyle = borderColor
      ctx.lineWidth = 1
      ctx.beginPath()
      ctx.moveTo(ROW_LABEL_WIDTH, ROW_HEIGHT)
      ctx.lineTo(viewW, ROW_HEIGHT)
      ctx.stroke()

      // Horizontal separator below subcategory row (only under categories that have subcategories)
      for (const span of categorySpans) {
        if (!span.subcategories || span.subcategories.length === 0) continue
        const sx0 = ROW_LABEL_WIDTH + span.startCol * CELL_SIZE - scrollX
        const sx1 = sx0 + span.count * CELL_SIZE
        if (sx1 < ROW_LABEL_WIDTH || sx0 > viewW) continue

        ctx.beginPath()
        ctx.moveTo(sx0, ROW_HEIGHT * 2)
        ctx.lineTo(sx1, ROW_HEIGHT * 2)
        ctx.stroke()
      }

      for (let i = 0; i < categorySpans.length; i++) {
        const span = categorySpans[i]
        const x0 = ROW_LABEL_WIDTH + span.startCol * CELL_SIZE - scrollX
        const x1 = x0 + span.count * CELL_SIZE
        if (x1 < ROW_LABEL_WIDTH || x0 > viewW) continue

        // Category label (use initials if text doesn't fit)
        const spanWidth = x1 - x0 - 4
        const centerX = (x0 + x1) / 2
        ctx.font = 'bold 10px sans-serif'
        ctx.fillStyle = getCategoryColor(span.name, isDark)
        ctx.textAlign = 'center'
        ctx.textBaseline = 'middle'
        const catLabel = fitLabel(ctx, span.name, spanWidth)
        if (catLabel) ctx.fillText(catLabel, centerX, catRowY)

        // Vertical separator at category boundary (except first) — header portion
        if (span.startCol > 0) {
          ctx.strokeStyle = separatorColor
          ctx.lineWidth = 1
          ctx.beginPath()
          ctx.moveTo(x0, 0)
          ctx.lineTo(x0, headerHeight)
          ctx.stroke()
        }

        // Subcategory labels
        if (span.subcategories) {
          for (const sub of span.subcategories) {
            const sx0 = ROW_LABEL_WIDTH + sub.startCol * CELL_SIZE - scrollX
            const sx1 = sx0 + sub.count * CELL_SIZE
            if (sx1 < ROW_LABEL_WIDTH || sx0 > viewW) continue

            const subWidth = sx1 - sx0 - 4
            const subCenterX = (sx0 + sx1) / 2
            ctx.font = '10px sans-serif'
            ctx.fillStyle = getCategoryColor(span.name, isDark)
            ctx.textAlign = 'center'
            ctx.textBaseline = 'middle'
            const subLabel = fitLabel(ctx, sub.name, subWidth)
            if (subLabel) ctx.fillText(subLabel, subCenterX, subRowY)

            // Vertical separator between subcategories (except at category boundary)
            if (sub.startCol > span.startCol) {
              ctx.strokeStyle = separatorColor
              ctx.lineWidth = 0.5
              ctx.beginPath()
              ctx.moveTo(sx0, ROW_HEIGHT)
              ctx.lineTo(sx0, headerHeight)
              ctx.stroke()
            }
          }
        }
      }
    }
    ctx.restore()

    // Draw row labels
    ctx.save()
    ctx.beginPath()
    ctx.rect(0, headerHeight, ROW_LABEL_WIDTH, viewH - headerHeight)
    ctx.clip()
    ctx.fillStyle = textColor
    ctx.font = '10px monospace'
    ctx.textAlign = 'right'
    ctx.textBaseline = 'middle'
    for (let row = firstRow; row <= lastRow; row++) {
      const cy = headerHeight + row * CELL_SIZE - scrollY + CELL_SIZE / 2
      ctx.fillText(String(filteredTests[row].index + 1), ROW_LABEL_WIDTH - 6, cy)
    }
    ctx.restore()

    // # label
    const hashSorted = sortCol === '#'
    ctx.fillStyle = hashSorted ? (isDark ? '#f9fafb' : '#111827') : textColor
    ctx.font = hashSorted ? 'bold 11px sans-serif' : '11px sans-serif'
    ctx.textAlign = 'left'
    ctx.textBaseline = 'bottom'
    ctx.fillText('#', 6, headerHeight - 4)

    // Draw full-height category separator lines across the data area
    if (expanded && categorySpans.length > 0) {
      ctx.save()
      ctx.beginPath()
      ctx.rect(ROW_LABEL_WIDTH, headerHeight, viewW - ROW_LABEL_WIDTH, viewH - headerHeight)
      ctx.clip()
      for (const span of categorySpans) {
        // Category separator
        if (span.startCol > 0) {
          const x = ROW_LABEL_WIDTH + span.startCol * CELL_SIZE - scrollX
          if (x >= ROW_LABEL_WIDTH && x <= viewW) {
            ctx.strokeStyle = separatorColor
            ctx.lineWidth = 1
            ctx.beginPath()
            ctx.moveTo(x, headerHeight)
            ctx.lineTo(x, viewH)
            ctx.stroke()
          }
        }
        // Subcategory separators
        if (span.subcategories) {
          for (const sub of span.subcategories) {
            if (sub.startCol <= span.startCol) continue
            const x = ROW_LABEL_WIDTH + sub.startCol * CELL_SIZE - scrollX
            if (x < ROW_LABEL_WIDTH || x > viewW) continue
            ctx.strokeStyle = separatorColor
            ctx.lineWidth = 0.5
            ctx.beginPath()
            ctx.moveTo(x, headerHeight)
            ctx.lineTo(x, viewH)
            ctx.stroke()
          }
        }
      }
      ctx.restore()
    }
  }, [filteredTests, columns, colorGrid, isDark, headerHeight, expanded, categorySpans, sortCol])

  useEffect(() => {
    draw()
  }, [draw])

  const handleScroll = useCallback(() => {
    requestAnimationFrame(draw)
  }, [draw])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return
    const observer = new ResizeObserver(() => {
      requestAnimationFrame(draw)
    })
    observer.observe(container)
    return () => observer.disconnect()
  }, [draw])

  const scheduleRedraw = useCallback(() => {
    cancelAnimationFrame(rafRef.current)
    rafRef.current = requestAnimationFrame(draw)
  }, [draw])

  const handleMouseMove = useCallback(
    (e: React.MouseEvent) => {
      const container = containerRef.current
      if (!container) return
      const rect = container.getBoundingClientRect()
      const mx = e.clientX - rect.left + container.scrollLeft
      const my = e.clientY - rect.top + container.scrollTop

      const col = Math.floor((mx - ROW_LABEL_WIDTH) / CELL_SIZE)
      const row = Math.floor((my - headerHeight) / CELL_SIZE)

      // Check if hovering over header (category/subcategory rows) — use screen-relative Y
      const viewY = e.clientY - rect.top
      if (expanded && viewY < headerHeight && viewY < ROW_HEIGHT * 2 && mx >= ROW_LABEL_WIDTH) {
        if (hoverRef.current) {
          hoverRef.current = null
          scheduleRedraw()
        }
        // Find which category/subcategory span the mouse is over
        const colIdx = Math.floor((mx - ROW_LABEL_WIDTH) / CELL_SIZE)
        if (viewY < ROW_HEIGHT) {
          // Category row
          for (const span of categorySpans) {
            if (colIdx >= span.startCol && colIdx < span.startCol + span.count) {
              setTooltip({ lines: [{ text: span.name, bold: true }], x: e.clientX, y: e.clientY + 16 })
              return
            }
          }
        } else {
          // Subcategory row
          for (const span of categorySpans) {
            if (!span.subcategories) continue
            for (const sub of span.subcategories) {
              if (colIdx >= sub.startCol && colIdx < sub.startCol + sub.count) {
                setTooltip({ lines: [{ text: `${span.name} / ${sub.name}`, bold: true }], x: e.clientX, y: e.clientY + 16 })
                return
              }
            }
          }
        }
        setTooltip(null)
        return
      }

      if (col >= 0 && col < columns.length && row >= 0 && row < filteredTests.length) {
        const prev = hoverRef.current
        if (!prev || prev.row !== row || prev.col !== col) {
          hoverRef.current = { row, col }
          scheduleRedraw()
        }
        const test = filteredTests[row].test
        const colName = columns[col]
        const count = getCount(test, colName)
        if (count > 0) {
          setTooltip({
            lines: [
              { text: `Test #${filteredTests[row].index + 1}` },
              { text: `${colName}: ${count.toLocaleString()}`, bold: true },
              { text: test.name },
            ],
            x: e.clientX,
            y: e.clientY - 12,
          })
          return
        }
      } else if (hoverRef.current) {
        hoverRef.current = null
        scheduleRedraw()
      }
      setTooltip(null)
    },
    [filteredTests, columns, scheduleRedraw, headerHeight, getCount, expanded, categorySpans],
  )

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      const container = containerRef.current
      if (!container) return
      const rect = container.getBoundingClientRect()
      // Use screen-relative Y (no scroll offset) since header is sticky
      const viewY = e.clientY - rect.top
      const mx = e.clientX - rect.left + container.scrollLeft
      if (viewY <= headerHeight) {
        // Header click — sort
        if (mx < ROW_LABEL_WIDTH) {
          onSortChange('#')
        } else {
          const col = Math.floor((mx - ROW_LABEL_WIDTH) / CELL_SIZE)
          if (col >= 0 && col < columns.length) {
            onSortChange(columns[col])
          }
        }
      } else if (onTestClick) {
        // Data cell click — open test detail
        const my = e.clientY - rect.top + container.scrollTop
        const row = Math.floor((my - headerHeight) / CELL_SIZE)
        if (row >= 0 && row < filteredTests.length) {
          // testIndex is 1-based
          onTestClick(filteredTests[row].index + 1)
        }
      }
    },
    [columns, headerHeight, onSortChange, onTestClick, filteredTests],
  )

  const handleMouseLeave = useCallback(() => {
    if (hoverRef.current) {
      hoverRef.current = null
      scheduleRedraw()
    }
    setTooltip(null)
  }, [scheduleRedraw])

  return (
    <div className="relative" style={maxHeight ? {} : { height: '100%' }}>
      <div
        ref={containerRef}
        className="overflow-auto rounded-xs border border-gray-200 dark:border-gray-700"
        style={maxHeight ? { maxHeight } : { height: '100%' }}
        onScroll={handleScroll}
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
        onClick={handleClick}
      >
        <div style={{ width: totalWidth, height: totalHeight, position: 'relative' }}>
          <canvas
            ref={canvasRef}
            className="pointer-events-none"
            style={{ position: 'sticky', top: 0, left: 0 }}
          />
        </div>
      </div>
      {tooltip && (
        <div
          className="pointer-events-none fixed z-50 flex max-w-sm flex-col gap-1 break-all rounded-xs bg-gray-900 px-2 py-1.5 text-xs text-white shadow-xs dark:bg-gray-700"
          style={{ left: tooltip.x, top: tooltip.y, transform: 'translate(-50%, -100%)' }}
        >
          {tooltip.lines.map((line, i) => (
            <div key={i} className={line.bold ? 'font-bold' : undefined}>{line.text}</div>
          ))}
        </div>
      )}
    </div>
  )
}

export function OpcodeHeatmap({ tests, onTestClick }: OpcodeHeatmapProps) {
  const [search, setSearch] = useState('')
  const [fullscreen, setFullscreen] = useState(false)
  const [expanded] = useState(true)
  const [groupStack, setGroupStack] = useState(true)
  const [sortCol, setSortCol] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<SortDir>('desc')
  const [isDark] = useState(() => {
    if (typeof window === 'undefined') return false
    return document.documentElement.classList.contains('dark')
  })

  useEffect(() => {
    if (!fullscreen) return
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false)
    }
    document.addEventListener('keydown', handleKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', handleKey)
      document.body.style.overflow = ''
    }
  }, [fullscreen])

  const testsWithOpcodes = useMemo(() => {
    return tests
      .map((t, i) => ({ test: t, index: i }))
      .filter((t) => t.test.eest?.info?.opcode_count && Object.keys(t.test.eest.info.opcode_count).length > 0)
  }, [tests])

  const filteredTests = useMemo(() => {
    if (!search) return testsWithOpcodes
    const q = search.toLowerCase()
    return testsWithOpcodes.filter((t) => t.test.name.toLowerCase().includes(q))
  }, [testsWithOpcodes, search])

  const allOpcodes = useMemo(() => {
    const set = new Set<string>()
    for (const { test } of testsWithOpcodes) {
      const counts = test.eest?.info?.opcode_count
      if (counts) {
        for (const op of Object.keys(counts)) {
          set.add(op)
        }
      }
    }
    return set
  }, [testsWithOpcodes])

  const grouped: GroupedResult = useMemo(() => getGroupedOpcodes(allOpcodes), [allOpcodes])

  // When groupStack is on in expanded mode, replace individual stack subcategory opcodes
  // with a single column per subcategory (Pop, Push, Dup, Swap)
  const stackGrouped = useMemo((): { columns: string[]; categorySpans: CategorySpan[] } => {
    if (!groupStack) return { columns: grouped.columns, categorySpans: grouped.categorySpans }
    const columns: string[] = []
    const categorySpans: CategorySpan[] = []
    for (const span of grouped.categorySpans) {
      if (span.subcategories && span.subcategories.length > 0) {
        // Replace individual opcodes with subcategory names
        const startCol = columns.length
        const subSpans = span.subcategories.map((sub) => {
          const s = { ...sub, startCol: columns.length, count: 1, opcodes: sub.opcodes }
          columns.push(sub.name)
          return s
        })
        categorySpans.push({ ...span, startCol, count: subSpans.length, subcategories: undefined })
      } else {
        const startCol = columns.length
        columns.push(...span.opcodes)
        categorySpans.push({ ...span, startCol, count: span.opcodes.length })
      }
    }
    return { columns, categorySpans }
  }, [grouped, groupStack])

  // Collapsed mode: category names as columns
  const collapsedColumns = useMemo(() => {
    return grouped.categorySpans.map((g) => g.name)
  }, [grouped])

  // Lookup: category name -> all opcodes in that category (for collapsed aggregation)
  const categoryOpcodeMap = useMemo(() => {
    const map = new Map<string, string[]>()
    for (const g of grouped.categorySpans) {
      map.set(g.name, g.opcodes)
    }
    return map
  }, [grouped])

  // Lookup: subcategory name -> opcodes (for groupStack aggregation)
  const subCategoryOpcodeMap = useMemo(() => {
    const map = new Map<string, string[]>()
    for (const span of grouped.categorySpans) {
      if (span.subcategories) {
        for (const sub of span.subcategories) {
          map.set(sub.name, sub.opcodes)
        }
      }
    }
    return map
  }, [grouped])

  const columns = expanded ? stackGrouped.columns : collapsedColumns

  const getCount = useCallback(
    (test: SuiteTest, col: string): number => {
      const counts = test.eest?.info?.opcode_count
      if (!counts) return 0
      if (expanded) {
        // If groupStack is on, a column might be a subcategory name
        if (groupStack) {
          const ops = subCategoryOpcodeMap.get(col)
          if (ops) {
            let total = 0
            for (const op of ops) {
              total += counts[op] ?? 0
            }
            return total
          }
        }
        return counts[col] ?? 0
      }
      // Collapsed: sum all opcodes in the category
      const ops = categoryOpcodeMap.get(col)
      if (!ops) return 0
      let total = 0
      for (const op of ops) {
        total += counts[op] ?? 0
      }
      return total
    },
    [expanded, groupStack, categoryOpcodeMap, subCategoryOpcodeMap],
  )

  const handleSortChange = useCallback(
    (col: string) => {
      if (sortCol === col) {
        // Cycle: desc -> asc -> clear
        if (sortDir === 'desc') {
          setSortDir('asc')
        } else {
          setSortCol(null)
        }
      } else {
        setSortCol(col)
        setSortDir('desc')
      }
    },
    [sortCol, sortDir],
  )

  const sortedTests = useMemo(() => {
    if (!sortCol) return filteredTests
    if (sortCol === '#') {
      return [...filteredTests].sort((a, b) =>
        sortDir === 'desc' ? b.index - a.index : a.index - b.index,
      )
    }
    return [...filteredTests].sort((a, b) => {
      const ca = getCount(a.test, sortCol)
      const cb = getCount(b.test, sortCol)
      return sortDir === 'desc' ? cb - ca : ca - cb
    })
  }, [filteredTests, sortCol, sortDir, getCount])

  const maxPerColumn = useMemo(() => {
    const maxes: Record<string, number> = {}
    for (const col of columns) {
      let max = 0
      for (const { test } of filteredTests) {
        const count = getCount(test, col)
        if (count > max) max = count
      }
      maxes[col] = max
    }
    return maxes
  }, [columns, filteredTests, getCount])

  if (testsWithOpcodes.length === 0) return null

  const toolbar = (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h3 className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">
          Opcode Heatmap
          <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
            ({filteredTests.length} tests, {expanded ? stackGrouped.columns.length + ' opcodes' : collapsedColumns.length + ' categories'}{sortCol ? `, sorted by ${sortCol} ${sortDir}` : ''})
          </span>
        </h3>
        <div className="flex items-center gap-2">
          <input
            type="text"
            placeholder="Filter tests..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="rounded-xs border border-gray-300 bg-white px-3 py-1 text-sm/6 placeholder-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500"
          />
          {expanded && (
            <button
              onClick={() => setGroupStack(!groupStack)}
              className={`rounded-xs border px-2 py-1 text-sm/6 ${groupStack ? 'border-blue-500 bg-blue-50 text-blue-700 dark:border-blue-400 dark:bg-blue-900/30 dark:text-blue-300' : 'border-gray-300 bg-white text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'}`}
              title={groupStack ? 'Expand Stack opcodes' : 'Group Stack opcodes'}
            >
              Group Stack
            </button>
          )}
          <button
            onClick={() => setFullscreen(!fullscreen)}
            className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
            title={fullscreen ? 'Exit fullscreen' : 'Fullscreen'}
          >
            {fullscreen ? (
              <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            ) : (
              <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5v-4m0 4h-4m4 0l-5-5" />
              </svg>
            )}
          </button>
        </div>
      </div>
      <HeatmapLegend isDark={isDark} />
    </div>
  )

  const canvasProps = {
    filteredTests: sortedTests,
    columns,
    maxPerColumn,
    isDark,
    expanded,
    categorySpans: expanded ? stackGrouped.categorySpans : grouped.categorySpans,
    getCount,
    sortCol,
    onSortChange: handleSortChange,
    onTestClick,
  }

  if (fullscreen) {
    return (
      <>
        <div className="flex flex-col gap-3">
          {toolbar}
        </div>
        <div className="fixed inset-0 z-50 flex flex-col gap-3 bg-white p-4 dark:bg-gray-900">
          {toolbar}
          <div className="min-h-0 flex-1">
            <HeatmapCanvas {...canvasProps} />
          </div>
        </div>
      </>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {toolbar}
      <HeatmapCanvas {...canvasProps} maxHeight={MAX_HEIGHT} />
    </div>
  )
}
