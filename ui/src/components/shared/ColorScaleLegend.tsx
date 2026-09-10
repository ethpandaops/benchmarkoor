import { useState } from 'react'
import clsx from 'clsx'

interface ColorScaleLegendProps {
  /** Scale colours, best first. */
  colors: readonly string[]
  /** Caption before the swatches — the best end of the scale. */
  startLabel?: string
  /** Caption after the swatches — the worst end of the scale. */
  endLabel?: string
  /** Heading of the hover panel. */
  title?: string
  /** Range of a step, one line in the hover panel. */
  stepRange: (step: number) => string
  /** Extra line under the ranges in the hover panel. */
  note?: string
}

interface PanelPosition {
  step: number
  x: number
  y: number
}

/**
 * Legend for a discrete colour scale. Hover a swatch to open a panel
 * with the range of every bucket, with the hovered one highlighted.
 */
export function ColorScaleLegend({ colors, startLabel, endLabel, title, stepRange, note }: ColorScaleLegendProps) {
  const [panel, setPanel] = useState<PanelPosition | null>(null)

  const handleMouseEnter = (step: number, event: React.MouseEvent) => {
    const rect = event.currentTarget.getBoundingClientRect()
    setPanel({ step, x: rect.left + rect.width / 2, y: rect.top })
  }

  return (
    <span className="flex items-center gap-1">
      {startLabel && <span>{startLabel}</span>}
      <span className="flex gap-0.5">
        {colors.map((color, step) => (
          <span
            key={step}
            className={clsx(
              'size-3 cursor-help rounded-xs ring-inset transition-transform',
              panel?.step === step && 'scale-125 ring-1 ring-gray-600 dark:ring-gray-200',
            )}
            style={{ backgroundColor: color }}
            onMouseEnter={(event) => handleMouseEnter(step, event)}
            onMouseLeave={() => setPanel(null)}
          />
        ))}
      </span>
      {endLabel && <span>{endLabel}</span>}

      {panel && (
        <div
          className="pointer-events-none fixed z-50 rounded-sm bg-white px-3 py-2 text-xs/5 text-gray-900 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-700"
          style={{ left: panel.x, top: panel.y - 8, transform: 'translate(-50%, -100%)' }}
        >
          {title && <div className="mb-1 font-medium">{title}</div>}
          <div className="flex flex-col gap-0.5">
            {colors.map((color, step) => (
              <div
                key={step}
                className={clsx(
                  'flex items-center gap-2 whitespace-nowrap',
                  step === panel.step ? 'font-medium' : 'text-gray-500 dark:text-gray-400',
                )}
              >
                <span className="size-3 shrink-0 rounded-xs" style={{ backgroundColor: color }} />
                <span>{stepRange(step)}</span>
              </div>
            ))}
          </div>
          {note && (
            <div className="mt-1 border-t border-gray-200 pt-1 text-gray-400 dark:border-gray-700 dark:text-gray-500">{note}</div>
          )}
        </div>
      )}
    </span>
  )
}
