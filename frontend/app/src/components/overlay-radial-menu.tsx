'use client'

import { motion, AnimatePresence } from 'motion/react'
import { ClipboardCopy, FileText, Mic } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

interface MenuItem {
  key: string
  label: string
  Icon: LucideIcon
  disabled?: boolean
}

interface OverlayRadialMenuProps {
  onCopy: () => void
  onNote: () => void
  onDictate: () => void
  onAgent?: () => void
  visible: boolean
  onClose: () => void
}

// Only show actions that are actually available
const ITEMS: MenuItem[] = [
  { key: 'copy', label: 'Copy', Icon: ClipboardCopy },
  { key: 'note', label: 'Note', Icon: FileText },
  { key: 'dictate', label: 'Dictate', Icon: Mic },
]

const SIZE = 220
const CENTER = SIZE / 2
const INNER_R = 40  // large enough to keep the bubble visible in the center
const OUTER_R = 96
const GAP_ANGLE = 3 // degrees gap between segments

function arcPath(
  cx: number, cy: number,
  innerR: number, outerR: number,
  startAngle: number, endAngle: number,
): string {
  const toRad = (deg: number) => (deg * Math.PI) / 180
  const s1 = toRad(startAngle)
  const e1 = toRad(endAngle)
  const largeArc = endAngle - startAngle > 180 ? 1 : 0

  const ox1 = cx + outerR * Math.cos(s1)
  const oy1 = cy + outerR * Math.sin(s1)
  const ox2 = cx + outerR * Math.cos(e1)
  const oy2 = cy + outerR * Math.sin(e1)
  const ix1 = cx + innerR * Math.cos(e1)
  const iy1 = cy + innerR * Math.sin(e1)
  const ix2 = cx + innerR * Math.cos(s1)
  const iy2 = cy + innerR * Math.sin(s1)

  return [
    `M ${ox1} ${oy1}`,
    `A ${outerR} ${outerR} 0 ${largeArc} 1 ${ox2} ${oy2}`,
    `L ${ix1} ${iy1}`,
    `A ${innerR} ${innerR} 0 ${largeArc} 0 ${ix2} ${iy2}`,
    'Z',
  ].join(' ')
}

function iconPosition(cx: number, cy: number, innerR: number, outerR: number, midAngle: number) {
  const r = (innerR + outerR) / 2
  const rad = (midAngle * Math.PI) / 180
  return {
    x: cx + r * Math.cos(rad),
    y: cy + r * Math.sin(rad),
  }
}

export function OverlayRadialMenu({
  onCopy, onNote, onDictate, onAgent, visible, onClose,
}: OverlayRadialMenuProps) {
  const handlers: Record<string, (() => void) | undefined> = {
    dictate: onDictate,
    copy: onCopy,
    agent: onAgent,
    note: onNote,
  }

  const count = ITEMS.length
  const sliceAngle = 360 / count

  return (
    <AnimatePresence>
      {visible && (
        <motion.div
          className="pointer-events-auto absolute"
          style={{
            width: SIZE,
            height: SIZE,
            left: '50%',
            top: '50%',
            marginLeft: -SIZE / 2,
            marginTop: -SIZE / 2,
          }}
          initial={{ opacity: 0, scale: 0.7 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, scale: 0.7 }}
          transition={{ duration: 0.18, ease: 'easeOut' }}
        >
          {/* Click-away backdrop -- scoped to overlay window, not full screen */}
          <div
            className="pointer-events-auto absolute inset-[-50px]"
            onClick={onClose}
          />

          <svg
            width={SIZE}
            height={SIZE}
            viewBox={`0 0 ${SIZE} ${SIZE}`}
            className="relative"
          >
            {/* Background ring */}
            <circle
              cx={CENTER}
              cy={CENTER}
              r={OUTER_R}
              fill="rgba(15,15,20,0.92)"
              stroke="rgba(255,255,255,0.08)"
              strokeWidth={1}
            />
            {/* Inner circle: transparent so the bubble shows through */}
            <circle
              cx={CENTER}
              cy={CENTER}
              r={INNER_R}
              fill="transparent"
              stroke="rgba(255,255,255,0.08)"
              strokeWidth={1}
            />

            {/* Arc segments */}
            {ITEMS.map((item, i) => {
              const startAngle = -90 + i * sliceAngle + GAP_ANGLE / 2
              const endAngle = -90 + (i + 1) * sliceAngle - GAP_ANGLE / 2
              const midAngle = (startAngle + endAngle) / 2
              const { x: iconX, y: iconY } = iconPosition(CENTER, CENTER, INNER_R, OUTER_R, midAngle)

              return (
                <g key={item.key}>
                  {/* Hit area (invisible, larger) */}
                  <path
                    d={arcPath(CENTER, CENTER, INNER_R, OUTER_R, startAngle, endAngle)}
                    fill="transparent"
                    className={item.disabled ? 'cursor-not-allowed' : 'cursor-pointer'}
                    onClick={() => {
                      if (!item.disabled) {
                        handlers[item.key]?.()
                        onClose()
                      }
                    }}
                    onMouseEnter={(e) => {
                      if (!item.disabled) {
                        const path = e.currentTarget
                        path.setAttribute('fill', 'rgba(255,255,255,0.08)')
                      }
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.setAttribute('fill', 'transparent')
                    }}
                  />

                  {/* Separator line */}
                  {(() => {
                    const rad = ((startAngle - GAP_ANGLE / 2) * Math.PI) / 180
                    return (
                      <line
                        x1={CENTER + INNER_R * Math.cos(rad)}
                        y1={CENTER + INNER_R * Math.sin(rad)}
                        x2={CENTER + OUTER_R * Math.cos(rad)}
                        y2={CENTER + OUTER_R * Math.sin(rad)}
                        stroke="rgba(255,255,255,0.06)"
                        strokeWidth={1}
                      />
                    )
                  })()}

                  {/* Icon */}
                  <foreignObject
                    x={iconX - 10}
                    y={iconY - 10}
                    width={20}
                    height={20}
                    className="pointer-events-none"
                  >
                    <div className={`flex h-full w-full items-center justify-center ${item.disabled ? 'opacity-25' : 'opacity-75'}`}>
                      <item.Icon size={14} color="white" />
                    </div>
                  </foreignObject>

                  {/* Label below icon */}
                  <text
                    x={iconX}
                    y={iconY + 16}
                    textAnchor="middle"
                    fontSize={7}
                    fill={item.disabled ? 'rgba(255,255,255,0.2)' : 'rgba(255,255,255,0.45)'}
                    className="pointer-events-none select-none"
                  >
                    {item.label}
                  </text>
                </g>
              )
            })}
          </svg>
        </motion.div>
      )}
    </AnimatePresence>
  )
}
