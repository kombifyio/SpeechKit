import { useState } from 'react'
import type { LucideIcon } from 'lucide-react'

/**
 * Edge-aware SVG donut radial menu for the Focus (Dot) overlay mode.
 *
 * - Renders as a ring of wedge segments (like a pie/donut chart) around the dot.
 * - 4 compass slots: top (270deg), right (0deg), bottom (90deg), left (180deg).
 * - The slot facing the screen edge is a non-interactive dummy (dimmed wedge).
 * - Real action buttons are placed on the remaining slots.
 * - No external dependencies -- pure SVG + React.
 */

export type DotMenuItem = {
  id: string
  label: string
  icon: LucideIcon
  onClick: () => void
}

type Props = {
  screenEdge: 'top' | 'bottom' | 'left' | 'right'
  items: DotMenuItem[]
  open: boolean
  onClose: () => void
  /** Outer radius of the donut ring */
  outerRadius?: number
  /** Inner radius (hole size) */
  innerRadius?: number
}

type Slot = 'top' | 'right' | 'bottom' | 'left'

// Each slot's center angle in degrees (0 = right, clockwise)
const SLOT_ANGLE: Record<Slot, number> = {
  top: 270,
  right: 0,
  bottom: 90,
  left: 180,
}

// Which slot is dummy for each screen edge
const EDGE_DUMMY: Record<string, Slot> = {
  top: 'top',
  bottom: 'bottom',
  left: 'left',
  right: 'right',
}

// Fill order: opposite slot first, then the two perpendicular ones
const FILL_ORDER: Record<string, Slot[]> = {
  top: ['bottom', 'left', 'right'],
  bottom: ['top', 'left', 'right'],
  left: ['right', 'top', 'bottom'],
  right: ['left', 'top', 'bottom'],
}

const ALL_SLOTS: Slot[] = ['top', 'right', 'bottom', 'left']

function degToRad(deg: number) {
  return (deg * Math.PI) / 180
}

/** Create an SVG arc path for a 90-degree wedge segment of a donut ring */
function wedgePath(slot: Slot, outer: number, inner: number): string {
  const center = SLOT_ANGLE[slot]
  const startDeg = center - 45
  const endDeg = center + 45

  const cos = (d: number) => Math.cos(degToRad(d))
  const sin = (d: number) => Math.sin(degToRad(d))

  const ox1 = cos(startDeg) * outer
  const oy1 = sin(startDeg) * outer
  const ox2 = cos(endDeg) * outer
  const oy2 = sin(endDeg) * outer
  const ix1 = cos(startDeg) * inner
  const iy1 = sin(startDeg) * inner
  const ix2 = cos(endDeg) * inner
  const iy2 = sin(endDeg) * inner

  return [
    `M ${ox1} ${oy1}`,
    `A ${outer} ${outer} 0 0 1 ${ox2} ${oy2}`,
    `L ${ix2} ${iy2}`,
    `A ${inner} ${inner} 0 0 0 ${ix1} ${iy1}`,
    'Z',
  ].join(' ')
}

export function DotRadialMenu({
  screenEdge,
  items,
  open,
  onClose,
  outerRadius = 40,
  innerRadius = 16,
}: Props) {
  const [hoveredSlot, setHoveredSlot] = useState<Slot | null>(null)

  if (!open) return null

  const dummySlot = EDGE_DUMMY[screenEdge]
  const fillOrder = FILL_ORDER[screenEdge]

  // Map items to slots
  const slotItem = new Map<Slot, DotMenuItem | null>()
  slotItem.set(dummySlot, null)
  fillOrder.forEach((slot, i) => {
    slotItem.set(slot, items[i] ?? null)
  })

  const size = outerRadius * 2 + 8 // small padding
  const iconRadius = (outerRadius + innerRadius) / 2

  return (
    <div
      className="absolute z-50"
      style={{
        width: size,
        height: size,
        left: -(size / 2),
        top: -(size / 2),
      }}
      onClick={(e) => {
        e.stopPropagation()
        onClose()
      }}
    >
      <svg
        viewBox={`${-size / 2} ${-size / 2} ${size} ${size}`}
        className="h-full w-full"
      >
        {ALL_SLOTS.map((slot) => {
          const item = slotItem.get(slot)
          const isDummy = slot === dummySlot
          const isHovered = hoveredSlot === slot && !isDummy && item != null
          const Icon = item?.icon

          return (
            <g key={slot}>
              {/* Wedge segment */}
              <path
                d={wedgePath(slot, outerRadius, innerRadius)}
                className={
                  isDummy
                    ? 'fill-white/[0.03] stroke-white/[0.06] stroke-[0.5]'
                    : isHovered
                      ? 'fill-white/20 stroke-white/25 stroke-[0.5] cursor-pointer'
                      : 'fill-black/50 stroke-white/10 stroke-[0.5] cursor-pointer'
                }
                onMouseEnter={() => !isDummy && setHoveredSlot(slot)}
                onMouseLeave={() => setHoveredSlot(null)}
                onClick={(e) => {
                  if (!isDummy && item) {
                    e.stopPropagation()
                    item.onClick()
                    onClose()
                  }
                }}
              />

              {/* Icon in the wedge center */}
              {!isDummy && Icon && (
                <foreignObject
                  x={Math.cos(degToRad(SLOT_ANGLE[slot])) * iconRadius - 10}
                  y={Math.sin(degToRad(SLOT_ANGLE[slot])) * iconRadius - 10}
                  width={20}
                  height={20}
                  className="pointer-events-none"
                >
                  <div className="flex h-full w-full items-center justify-center">
                    <Icon
                      className={
                        isHovered
                          ? 'h-3 w-3 text-white'
                          : 'h-3 w-3 text-white/60'
                      }
                    />
                  </div>
                </foreignObject>
              )}
            </g>
          )
        })}

        {/* Center hole indicator (thin ring) */}
        <circle
          cx={0}
          cy={0}
          r={innerRadius - 1}
          className="fill-none stroke-white/[0.06] stroke-[0.5]"
        />
      </svg>
    </div>
  )
}
