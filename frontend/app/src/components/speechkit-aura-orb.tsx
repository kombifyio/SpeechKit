'use client'

import { useMemo } from 'react'
import { type AgentState } from '@livekit/components-react'
import { motion } from 'motion/react'
import { Sparkles } from 'lucide-react'

import { AgentAudioVisualizerRadial } from '@/components/agent-audio-visualizer-radial'
import { cn } from '@/lib/utils'

export type SpeechKitAuraOrbState =
  | 'inactive'
  | 'connecting'
  | 'listening'
  | 'processing'
  | 'speaking'
  | 'settling'
  | 'error'

type SpeechKitAuraOrbProps = {
  state?: SpeechKitAuraOrbState
  userLevel?: number
  assistantLevel?: number
  className?: string
}

type OrbCopy = {
  label: string
  detail: string
  color: `#${string}`
  ring: string
}

const ORB_COPY: Record<SpeechKitAuraOrbState, OrbCopy> = {
  inactive: {
    label: 'Idle',
    detail: 'Waiting for a live voice session to begin.',
    color: '#64748b',
    ring: 'rgba(148, 163, 184, 0.18)',
  },
  connecting: {
    label: 'Connecting',
    detail: 'Bringing the realtime dialog online.',
    color: '#67e8f9',
    ring: 'rgba(103, 232, 249, 0.24)',
  },
  listening: {
    label: 'Listening',
    detail: 'Ready for the user to continue the conversation.',
    color: '#34d399',
    ring: 'rgba(52, 211, 153, 0.24)',
  },
  processing: {
    label: 'Thinking',
    detail: 'Formulating the next turn in real time.',
    color: '#f59e0b',
    ring: 'rgba(245, 158, 11, 0.26)',
  },
  speaking: {
    label: 'Speaking',
    detail: 'The agent is responding live.',
    color: '#fb7185',
    ring: 'rgba(251, 113, 133, 0.26)',
  },
  settling: {
    label: 'Settling',
    detail: 'The dialog is cooling down.',
    color: '#8b5cf6',
    ring: 'rgba(139, 92, 246, 0.2)',
  },
  error: {
    label: 'Error',
    detail: 'The live voice session needs attention.',
    color: '#f87171',
    ring: 'rgba(248, 113, 113, 0.24)',
  },
}

const VISUALIZER_STATE: Record<SpeechKitAuraOrbState, AgentState> = {
  inactive: 'listening',
  connecting: 'connecting',
  listening: 'listening',
  processing: 'thinking',
  speaking: 'speaking',
  settling: 'listening',
  error: 'listening',
}

const ROOT_MOTION = {
  inactive: { scale: 0.985, opacity: 0.72 },
  connecting: { scale: [0.98, 1.015, 0.99], rotate: [0, 1.5, 0], opacity: 0.95 },
  listening: { scale: [1, 1.01, 1], opacity: 0.98 },
  processing: { scale: [1, 1.02, 0.995], rotate: [0, -1.25, 0], opacity: 0.98 },
  speaking: { scale: [1, 1.03, 1.01], opacity: 1 },
  settling: { scale: [1, 1.008, 1], opacity: [0.92, 1, 0.92] },
  error: { scale: [1, 0.992, 1], opacity: [0.88, 1, 0.88] },
} satisfies Record<SpeechKitAuraOrbState, object>

function clampLevel(level: number) {
  if (!Number.isFinite(level)) {
    return 0
  }
  return Math.max(0, Math.min(1, level))
}

function resolveLevelForState(
  state: SpeechKitAuraOrbState,
  _userLevel: number,
  assistantLevel: number,
) {
  if (state === 'speaking') {
    return clampLevel(assistantLevel)
  }
  return 0.12
}

export function SpeechKitAuraOrb({
  state = 'inactive',
  userLevel = 0,
  assistantLevel = 0,
  className,
}: SpeechKitAuraOrbProps) {
  const copy = ORB_COPY[state]
  const visualizerState = VISUALIZER_STATE[state]
  const level = useMemo(
    () => resolveLevelForState(state, userLevel, assistantLevel),
    [state, userLevel, assistantLevel],
  )

  return (
    <div
      data-testid="speechkit-aura-orb"
      data-state={state}
      data-variant="ambient"
      aria-label={`${copy.label}. ${copy.detail}`}
      className={cn('relative isolate mx-auto flex aspect-square items-center justify-center overflow-visible', className)}
    >
      <motion.div
        aria-hidden="true"
        className="absolute inset-[-26%] rounded-full blur-3xl"
        style={{
          background: `radial-gradient(circle, ${copy.ring} 0%, rgba(255,255,255,0.035) 42%, rgba(255,255,255,0) 72%)`,
        }}
        animate={{ opacity: state === 'inactive' ? 0.34 : [0.62, 0.95, 0.68] }}
        transition={{ duration: state === 'processing' ? 2.2 : 1.8, repeat: Infinity, ease: 'easeInOut' }}
      />

      <motion.div
        className="relative flex h-full w-full items-center justify-center"
        animate={ROOT_MOTION[state] as never}
        transition={{ duration: state === 'processing' ? 2.4 : 1.6, repeat: Infinity, ease: 'easeInOut' }}
      >
        <motion.div
          aria-hidden="true"
          className="absolute inset-[4%] rounded-full border border-white/8 bg-[radial-gradient(circle_at_50%_50%,rgba(255,255,255,0.04),rgba(255,255,255,0)_64%)]"
          animate={{ opacity: state === 'inactive' ? 0.32 : [0.72, 1, 0.78] }}
          transition={{ duration: 2.4, repeat: Infinity, ease: 'easeInOut' }}
        />

        <motion.div
          aria-hidden="true"
          className="absolute inset-[12%] rounded-full"
          animate={{ rotate: state === 'processing' ? 360 : 0 }}
          transition={{ duration: state === 'processing' ? 18 : 0, repeat: Infinity, ease: 'linear' }}
        >
          <AgentAudioVisualizerRadial
            size="sm"
            radius={18}
            barCount={16}
            state={visualizerState}
            level={level}
            color={copy.color}
            className="h-full w-full text-current"
          />
        </motion.div>

        <motion.div
          aria-hidden="true"
          className="absolute inset-[31%] rounded-full border border-white/14 bg-[radial-gradient(circle_at_50%_36%,rgba(255,255,255,0.78),rgba(255,255,255,0.16)_42%,rgba(255,255,255,0)_76%)]"
          animate={{ scale: state === 'speaking' ? [0.98, 1.04, 1] : [1, 1.018, 1] }}
          transition={{ duration: state === 'processing' ? 1.6 : 1.8, repeat: Infinity, ease: 'easeInOut' }}
        >
          <Sparkles className="absolute left-1/2 top-1/2 h-3.5 w-3.5 -translate-x-1/2 -translate-y-1/2 text-white/78" />
        </motion.div>
      </motion.div>
    </div>
  )
}
