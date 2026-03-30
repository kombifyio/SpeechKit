import type { AgentState } from '@livekit/components-react'

import type { SpeechKitOverlayState } from '@/lib/speechkit'

export type OverlayTone = {
  state: AgentState
  size: 'icon' | 'sm'
  color: `#${string}`
  level: number
  className: string
  shellClassName: string
  visualizerClassName: string
}

export function resolveOverlayTone(snapshot: SpeechKitOverlayState): OverlayTone {
  const isCircle = snapshot.visualizer === 'circle'
  const kombifyPill = snapshot.visualizer === 'pill' && snapshot.design === 'kombify'
  const activePillShell = 'h-[26px] rounded-full bg-black/68 px-2.5 shadow-[inset_0_0_0_1px_rgba(255,255,255,0.04)]'
  const idlePillShell = 'h-[20px] rounded-full bg-black/60 px-2 shadow-[inset_0_0_0_1px_rgba(255,255,255,0.04)]'

  if (snapshot.phase === 'thinking') {
    return {
      state: 'thinking',
      size: 'icon',
      color: '#7dd3fc',
      level: 0,
      className: 'opacity-94 scale-100',
      shellClassName: isCircle
        ? 'aspect-square h-[18px] w-[18px] overflow-hidden rounded-full border border-white/10 bg-black/32'
        : activePillShell,
      visualizerClassName: isCircle ? 'h-full w-full' : '',
    }
  }

  if (snapshot.phase === 'done') {
    return {
      state: 'listening',
      size: 'icon',
      color: kombifyPill ? '#22c55e' : '#86efac',
      level: 0,
      className: 'opacity-90 scale-100',
      shellClassName: isCircle
        ? 'aspect-square h-[18px] w-[18px] overflow-hidden rounded-full border border-emerald-200/14 bg-black/30'
        : activePillShell,
      visualizerClassName: isCircle ? 'h-full w-full' : '',
    }
  }

  if (snapshot.phase === 'speaking') {
    return {
      state: 'speaking',
      size: 'icon',
      color: kombifyPill ? '#22c55e' : '#fb923c',
      level: snapshot.level,
      className: 'opacity-100 scale-100',
      shellClassName: isCircle
        ? 'aspect-square h-[18px] w-[18px] overflow-hidden rounded-full border border-orange-200/16 bg-black/34'
        : activePillShell,
      visualizerClassName: isCircle ? 'h-full w-full' : '',
    }
  }

  if (snapshot.phase === 'listening') {
    return {
      state: 'listening',
      size: 'icon',
      color: isCircle ? '#f8fafc' : kombifyPill ? '#22c55e' : '#fbbf24',
      level: snapshot.level,
      className: isCircle ? 'opacity-74 scale-100' : 'opacity-92 scale-100',
      shellClassName: isCircle
        ? 'aspect-square h-[18px] w-[18px] overflow-hidden rounded-full border border-white/10 bg-black/30'
        : activePillShell,
      visualizerClassName: isCircle ? 'h-full w-full' : '',
    }
  }

  return {
    state: 'listening',
    size: 'icon',
    color: '#e2e8f0',
    level: 0,
    className: 'opacity-62 scale-100',
    shellClassName: isCircle
      ? 'aspect-square h-[18px] w-[18px] overflow-hidden rounded-full border border-white/8 bg-black/24'
      : idlePillShell,
    visualizerClassName: isCircle ? 'h-full w-full' : 'h-[16px]',
  }
}

export function bubblePositionClass(position: string): string {
  switch (position) {
    case 'bottom':
      return 'absolute bottom-1.5 left-1/2 -translate-x-1/2'
    case 'left':
      return 'absolute left-1.5 top-1/2 -translate-y-1/2'
    case 'right':
      return 'absolute right-1.5 top-1/2 -translate-y-1/2'
    default:
      return 'absolute top-1.5 left-1/2 -translate-x-1/2'
  }
}
