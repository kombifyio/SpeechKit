'use client'

import { useMemo, type ComponentProps, type CSSProperties } from 'react'
import type { AgentState } from '@livekit/components-react'
import { useAgentAudioVisualizerBarAnimator } from '@/hooks/use-agent-audio-visualizer-bar'
import { cn } from '@/lib/utils'

export interface AgentAudioVisualizerBarProps {
  size?: 'icon' | 'sm' | 'md' | 'lg' | 'xl'
  state?: AgentState
  color?: `#${string}`
  barCount?: number
  /** Direct audio level 0-1 (used instead of LiveKit track) */
  level?: number
  className?: string
}

const sizeConfig = {
  icon: { height: 'h-[24px]', gap: 'gap-[2px]', bar: 'w-[4px] min-h-[4px]' },
  sm: { height: 'h-[56px]', gap: 'gap-[4px]', bar: 'w-[8px] min-h-[8px]' },
  md: { height: 'h-[112px]', gap: 'gap-[8px]', bar: 'w-[16px] min-h-[16px]' },
  lg: { height: 'h-[224px]', gap: 'gap-[16px]', bar: 'w-[32px] min-h-[32px]' },
  xl: { height: 'h-[448px]', gap: 'gap-[32px]', bar: 'w-[64px] min-h-[64px]' },
}

export function AgentAudioVisualizerBar({
  size = 'md',
  state = 'connecting',
  color,
  barCount,
  level = 0,
  className,
  style,
  ...props
}: AgentAudioVisualizerBarProps & ComponentProps<'div'>) {
  const count = useMemo(() => {
    if (barCount) return barCount
    return size === 'icon' || size === 'sm' ? 3 : 5
  }, [barCount, size])

  const sequencerInterval = useMemo(() => {
    switch (state) {
      case 'connecting': return 2000 / count
      case 'initializing': return 2000
      case 'listening': return 500
      case 'thinking': return 150
      default: return 1000
    }
  }, [state, count])

  const highlightedIndices = useAgentAudioVisualizerBarAnimator(
    state,
    count,
    sequencerInterval,
  )

  // Generate per-bar heights from level (simulate multiband with noise)
  const bands = useMemo(() => {
    if (state !== 'speaking' || level <= 0) {
      return new Array(count).fill(0)
    }
    return Array.from({ length: count }, (_, i) => {
      const center = (count - 1) / 2
      const dist = Math.abs(i - center) / Math.max(center, 1)
      const variation = 0.6 + 0.4 * (1 - dist)
      const harmonic = (Math.sin((i + 1) * 1.618 + count * 0.75) + 1) / 2
      return Math.min(1, level * variation * (0.8 + harmonic * 0.4))
    })
  }, [state, level, count])

  const cfg = sizeConfig[size] || sizeConfig.md

  return (
    <div
      data-lk-state={state}
      style={{ ...style, color } as CSSProperties}
      className={cn('relative flex items-center justify-center', cfg.height, cfg.gap, className)}
      {...props}
    >
      {bands.map((band, idx) => (
        <div
          key={idx}
          data-lk-index={idx}
          data-lk-highlighted={highlightedIndices.includes(idx)}
          style={{ height: `${Math.max(band * 100, 10)}%` }}
          className={cn(
            'rounded-full transition-colors duration-250 ease-linear',
            'bg-current/10 data-[lk-highlighted=true]:bg-current',
            cfg.bar,
          )}
        />
      ))}
    </div>
  )
}
