import { useCallback, useRef } from 'react'
import type { MouseEvent as ReactMouseEvent, MutableRefObject } from 'react'

/**
 * useChartAreaClick wires a "click anywhere on the chart opens the
 * currently-highlighted test" behavior. Same approach used by the
 * run-detail resource charts.
 *
 * Usage:
 *   const { highlightedTestRef, handleMouseDown, handleClick, cursor } =
 *     useChartAreaClick(onTestClick)
 *
 *   // In the tooltip formatter (trigger: 'axis'), stash the hovered test:
 *   highlightedTestRef.current = testName
 *
 *   // Wrap <ReactECharts> in a div that forwards the mouse handlers:
 *   <div onMouseDown={handleMouseDown} onClick={handleClick} style={{ cursor }}>
 *     <ReactECharts ... />
 *   </div>
 *
 * The mouse-down/click pair suppresses accidental triggers when the
 * user drags the data-zoom slider — only a click without movement
 * opens the modal.
 */
export function useChartAreaClick(
  onTestClick?: (testName: string) => void,
  externalRef?: MutableRefObject<string | null>,
): {
  highlightedTestRef: MutableRefObject<string | null>
  handleMouseDown: (e: ReactMouseEvent) => void
  handleClick: (e: ReactMouseEvent) => void
  cursor: 'pointer' | 'default'
} {
  const internalRef = useRef<string | null>(null)
  const highlightedTestRef = externalRef ?? internalRef
  const mouseDownPos = useRef<{ x: number; y: number } | null>(null)

  const handleMouseDown = useCallback((e: ReactMouseEvent) => {
    mouseDownPos.current = { x: e.clientX, y: e.clientY }
  }, [])

  const handleClick = useCallback((e: ReactMouseEvent) => {
    if (mouseDownPos.current) {
      const dx = Math.abs(e.clientX - mouseDownPos.current.x)
      const dy = Math.abs(e.clientY - mouseDownPos.current.y)
      if (dx > 5 || dy > 5) return
    }
    if (onTestClick && highlightedTestRef.current) {
      onTestClick(highlightedTestRef.current)
    }
  }, [onTestClick, highlightedTestRef])

  return {
    highlightedTestRef,
    handleMouseDown,
    handleClick,
    cursor: onTestClick ? 'pointer' : 'default',
  }
}
