import { useState } from 'react'
import type { LucideIcon } from 'lucide-react'

/**
 * Edge-aware SVG donut radial menu for the Focus (Dot) overlay mode.
 *
 * - Renders as a ring of wedge segments around the dot.
 * - Slot count adapts to items.length + 1 (dummy).
 * - The dummy wedge faces the screen edge (non-interactive, dimmed).
 * - Real action buttons fill the remaining slots.
 */

export type DotMenuItem = {
  id: string
  label: string
  icon: LucideIcon
  pressed?: boolean
  runtimeActive?: boolean
  slashed?: boolean
  onClick: () => void
}

type Props = {
  screenEdge: 'top' | 'bottom' | 'left' | 'right'
  items: DotMenuItem[]
  open: boolean
  onClose: () => void
  outerRadius?: number
  innerRadius?: number
}

const EDGE_ANGLE: Record<string, number> = {
  right: 0,
  bottom: 90,
  left: 180,
  top: 270,
}

function degToRad(deg: number) {
  return (deg * Math.PI) / 180
}

function normalizeAngle(deg: number): number {
  return ((deg % 360) + 360) % 360
}

/** Build N evenly spaced slots, each with a center angle and sweep. */
function buildSlots(count: number) {
  const sweep = 360 / count
  return Array.from({ length: count }, (_, i) => ({
    index: i,
    centerAngle: normalizeAngle(i * sweep + 270), // start from top
    sweep,
  }))
}

/** Find which slot index is closest to the screen edge direction. */
function findDummyIndex(slots: { centerAngle: number }[], edgeAngle: number): number {
  let best = 0
  let bestDist = 360
  for (let i = 0; i < slots.length; i++) {
    const diff = Math.abs(normalizeAngle(slots[i].centerAngle - edgeAngle))
    const dist = Math.min(diff, 360 - diff)
    if (dist < bestDist) {
      bestDist = dist
      best = i
    }
  }
  return best
}

/** SVG arc path for a wedge of arbitrary sweep. */
function wedgePath(centerAngle: number, sweep: number, outer: number, inner: number): string {
  const startDeg = centerAngle - sweep / 2
  const endDeg = centerAngle + sweep / 2
  const largeArc = sweep > 180 ? 1 : 0

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
    `A ${outer} ${outer} 0 ${largeArc} 1 ${ox2} ${oy2}`,
    `L ${ix2} ${iy2}`,
    `A ${inner} ${inner} 0 ${largeArc} 0 ${ix1} ${iy1}`,
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
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)

  if (!open) return null

  const slotCount = items.length + 1 // items + 1 dummy
  const slots = buildSlots(slotCount)
  const edgeAngle = EDGE_ANGLE[screenEdge] ?? 270
  const dummyIndex = findDummyIndex(slots, edgeAngle)

  // Map items to non-dummy slots
  const slotItems = new Map<number, DotMenuItem | null>()
  let itemIdx = 0
  for (let i = 0; i < slots.length; i++) {
    if (i === dummyIndex) {
      slotItems.set(i, null)
    } else {
      slotItems.set(i, items[itemIdx] ?? null)
      itemIdx++
    }
  }

  const size = outerRadius * 2 + 8
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
        {slots.map((slot) => {
          const item = slotItems.get(slot.index)
          const isDummy = slot.index === dummyIndex
          const isHovered = hoveredIndex === slot.index && !isDummy && item != null
          const Icon = item?.icon
          const isPressed = Boolean(item?.pressed)
          const isRuntimeActive = Boolean(item?.runtimeActive)

          return (
            <g key={slot.index}>
              <path
                d={wedgePath(slot.centerAngle, slot.sweep, outerRadius, innerRadius)}
                className={
                  isDummy
                    ? 'fill-white/[0.03] stroke-white/[0.06] stroke-[0.5]'
                    : isRuntimeActive
                      ? 'fill-orange-500/18 stroke-orange-400/30 stroke-[0.5] cursor-pointer'
                      : isPressed
                        ? 'fill-white/12 stroke-white/20 stroke-[0.5] cursor-pointer'
                        : isHovered
                          ? 'fill-white/20 stroke-white/25 stroke-[0.5] cursor-pointer'
                          : 'fill-black/50 stroke-white/10 stroke-[0.5] cursor-pointer'
                }
                onMouseEnter={() => !isDummy && setHoveredIndex(slot.index)}
                onMouseLeave={() => setHoveredIndex(null)}
                onClick={(e) => {
                  if (!isDummy && item) {
                    e.stopPropagation()
                    item.onClick()
                    onClose()
                  }
                }}
              />

              {!isDummy && Icon && (
                <foreignObject
                  x={Math.cos(degToRad(slot.centerAngle)) * iconRadius - 10}
                  y={Math.sin(degToRad(slot.centerAngle)) * iconRadius - 10}
                  width={20}
                  height={20}
                  className="pointer-events-none"
                >
                  <div className="relative flex h-full w-full items-center justify-center">
                    <Icon
                      className={
                        isRuntimeActive
                          ? 'h-3 w-3 text-orange-100'
                          : isHovered
                          ? 'h-3 w-3 text-white'
                          : isPressed
                            ? 'h-3 w-3 text-white/85'
                            : 'h-3 w-3 text-white/60'
                      }
                    />
                    {item?.slashed ? (
                      <span className="pointer-events-none absolute h-0.5 w-4 rotate-[-35deg] rounded-full bg-white/55" />
                    ) : null}
                  </div>
                </foreignObject>
              )}
            </g>
          )
        })}

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
