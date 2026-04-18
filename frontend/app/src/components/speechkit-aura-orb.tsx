'use client'

import { type CSSProperties, useMemo } from 'react'
import { type AgentState } from '@livekit/components-react'
import { motion } from 'motion/react'
import { Bot, Mic, Sparkles, Waves } from 'lucide-react'

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
  badge: string
}

const ORB_COPY: Record<SpeechKitAuraOrbState, OrbCopy> = {
  inactive: {
    label: 'Idle',
    detail: 'Waiting for a live voice session to begin.',
    color: '#64748b',
    ring: 'rgba(148, 163, 184, 0.18)',
    badge: 'Standby',
  },
  connecting: {
    label: 'Connecting',
    detail: 'Bringing the realtime dialog online.',
    color: '#67e8f9',
    ring: 'rgba(103, 232, 249, 0.24)',
    badge: 'Live link',
  },
  listening: {
    label: 'Listening',
    detail: 'Ready for the user to continue the conversation.',
    color: '#34d399',
    ring: 'rgba(52, 211, 153, 0.24)',
    badge: 'Awaiting input',
  },
  processing: {
    label: 'Thinking',
    detail: 'Formulating the next turn in real time.',
    color: '#f59e0b',
    ring: 'rgba(245, 158, 11, 0.26)',
    badge: 'Processing',
  },
  speaking: {
    label: 'Speaking',
    detail: 'The agent is responding live.',
    color: '#fb7185',
    ring: 'rgba(251, 113, 133, 0.26)',
    badge: 'Replying',
  },
  settling: {
    label: 'Settling',
    detail: 'The dialog is cooling down.',
    color: '#8b5cf6',
    ring: 'rgba(139, 92, 246, 0.2)',
    badge: 'Cooling down',
  },
  error: {
    label: 'Error',
    detail: 'The live voice session needs attention.',
    color: '#f87171',
    ring: 'rgba(248, 113, 113, 0.24)',
    badge: 'Attention',
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
  userLevel: number,
  assistantLevel: number,
) {
  if (state === 'speaking') {
    return clampLevel(assistantLevel)
  }
  if (state === 'listening') {
    return clampLevel(userLevel)
  }
  return Math.max(clampLevel(userLevel), clampLevel(assistantLevel), 0.12)
}

function resolveStateNote(state: SpeechKitAuraOrbState, userLevel: number, assistantLevel: number) {
  const user = clampLevel(userLevel)
  const assistant = clampLevel(assistantLevel)

  switch (state) {
    case 'connecting':
      return 'Online handshake'
    case 'listening':
      return user > 0.06 ? 'You are speaking' : 'Waiting for the next idea'
    case 'processing':
      return assistant > 0.06 ? 'Drafting a response' : 'Thinking in the background'
    case 'speaking':
      return assistant > 0.06 ? 'Voice agent speaking' : 'Preparing the reply'
    case 'settling':
      return 'Conversation complete'
    case 'error':
      return 'Reconnect required'
    case 'inactive':
    default:
      return 'Standby'
  }
}

function speakerMeterCopy(
  role: 'user' | 'assistant',
  state: SpeechKitAuraOrbState,
  level: number,
) {
  if (role === 'user') {
    if (state === 'listening') {
      return level > 0.06 ? 'Speaking' : 'Ready'
    }
    return 'Listener'
  }

  if (state === 'processing') {
    return level > 0.06 ? 'Thinking' : 'Working'
  }
  if (state === 'speaking') {
    return level > 0.06 ? 'Speaking' : 'Preparing'
  }
  return 'Agent'
}

function SpeakerMeter({
  label,
  level,
  accentClassName,
  note,
}: {
  label: string
  level: number
  accentClassName: string
  note: string
}) {
  const width = `${Math.max(8, Math.round(clampLevel(level) * 100))}%`

  return (
    <div className="rounded-2xl border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)]/82 px-3 py-2.5">
      <div className="flex items-center justify-between gap-2 text-[11px] font-medium uppercase tracking-[0.18em] text-[color:var(--sk-text-muted)]/72">
        <span>{label}</span>
        <span className={accentClassName}>{note}</span>
      </div>
      <div className="mt-2 h-2 overflow-hidden rounded-full bg-[color:var(--sk-surface-1)]/90">
        <motion.div
          className={cn('h-full rounded-full bg-current', accentClassName)}
          style={{ width, boxShadow: '0 0 18px currentColor' }}
          animate={{ opacity: level > 0.06 ? [0.68, 1, 0.82] : 0.45 }}
          transition={{ duration: 1.25, repeat: Infinity, ease: 'easeInOut' }}
        />
      </div>
    </div>
  )
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
  const note = resolveStateNote(state, userLevel, assistantLevel)

  return (
    <section
      data-testid="speechkit-aura-orb"
      data-state={state}
      aria-label={`${copy.label}. ${copy.detail}`}
      className={cn(
        'relative overflow-hidden rounded-[34px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)]/92 px-4 py-4 shadow-[0_24px_72px_rgba(0,0,0,0.28)]',
        className,
      )}
    >
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 opacity-80"
        style={{
          background: `radial-gradient(circle at 50% 14%, ${copy.ring} 0%, rgba(15, 23, 42, 0.08) 46%, rgba(2, 6, 23, 0) 76%)`,
        }}
      />

      <div className="relative flex flex-col gap-4">
        <motion.div
          className="relative mx-auto flex aspect-square w-full max-w-[360px] items-center justify-center"
          animate={ROOT_MOTION[state] as Record<string, unknown>}
          transition={{ duration: state === 'processing' ? 2.4 : 1.6, repeat: Infinity, ease: 'easeInOut' }}
        >
          <motion.div
            aria-hidden="true"
            className="absolute inset-[2%] rounded-full blur-3xl"
            style={{
              background: `radial-gradient(circle, ${copy.ring} 0%, rgba(255,255,255,0.04) 42%, rgba(255,255,255,0) 74%)`,
            }}
            animate={{ opacity: state === 'inactive' ? 0.36 : [0.62, 0.92, 0.68] }}
            transition={{ duration: state === 'processing' ? 2.2 : 1.8, repeat: Infinity, ease: 'easeInOut' }}
          />

          <motion.div
            aria-hidden="true"
            className="absolute inset-[8%] rounded-full border border-white/10 bg-[color:var(--sk-surface-1)]/76 shadow-[inset_0_0_0_1px_rgba(255,255,255,0.02)]"
            animate={{ opacity: state === 'inactive' ? 0.4 : [0.8, 1, 0.86] }}
            transition={{ duration: 2.4, repeat: Infinity, ease: 'easeInOut' }}
          />

          <motion.div
            aria-hidden="true"
            className="absolute inset-[13%] rounded-full border border-white/10 bg-[color:var(--sk-surface-2)]/70"
            animate={{ rotate: state === 'processing' ? 360 : 0 }}
            transition={{ duration: state === 'processing' ? 18 : 0, repeat: Infinity, ease: 'linear' }}
          >
            <AgentAudioVisualizerRadial
              size="xl"
              barCount={24}
              state={visualizerState}
              level={level}
              color={copy.color}
              className="h-full w-full text-current"
            />
          </motion.div>

          <motion.div
            aria-hidden="true"
            className="absolute inset-[30%] rounded-full border border-white/14 bg-[radial-gradient(circle_at_50%_40%,rgba(255,255,255,0.4),rgba(255,255,255,0.12)_34%,rgba(255,255,255,0.02)_68%,rgba(255,255,255,0)_100%)] shadow-[inset_0_0_38px_rgba(255,255,255,0.12)]"
            animate={{ scale: state === 'speaking' ? [0.98, 1.04, 1] : [1, 1.018, 1] }}
            transition={{ duration: state === 'processing' ? 1.6 : 1.8, repeat: Infinity, ease: 'easeInOut' }}
          >
            <div className="absolute inset-[18%] rounded-full bg-[radial-gradient(circle_at_50%_35%,rgba(255,255,255,0.82),rgba(255,255,255,0.12)_46%,rgba(255,255,255,0)_78%)] opacity-80" />
            <Sparkles className="absolute left-1/2 top-1/2 h-4 w-4 -translate-x-1/2 -translate-y-1/2 text-white/80" />
          </motion.div>
        </motion.div>

        <div className="grid gap-3 border-t border-[color:var(--sk-panel-border)] pt-3">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="text-[11px] font-semibold uppercase tracking-[0.26em] text-[color:var(--sk-text-muted)]/72">
                Voice Agent
              </p>
              <p className="mt-1 text-sm text-[color:var(--sk-text-muted)]/82">{copy.detail}</p>
            </div>
            <div className="shrink-0 rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-1.5 text-[11px] font-medium text-[color:var(--sk-text-muted)]/84">
              {copy.badge}
            </div>
          </div>

          <div className="grid gap-2 sm:grid-cols-2">
            <SpeakerMeter
              label="You"
              level={userLevel}
              accentClassName="text-sky-200"
              note={speakerMeterCopy('user', state, userLevel)}
            />
            <SpeakerMeter
              label="Voice Agent"
              level={assistantLevel}
              accentClassName={state === 'processing' ? 'text-amber-200' : 'text-emerald-200'}
              note={speakerMeterCopy('assistant', state, assistantLevel)}
            />
          </div>

          <div className="flex flex-wrap items-center gap-2 text-[11px] font-medium uppercase tracking-[0.18em] text-[color:var(--sk-text-muted)]/68">
            <span className="inline-flex items-center gap-2 rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-1.5">
              <Mic className="h-3.5 w-3.5 text-sky-200" />
              {resolveStateNote(state, userLevel, assistantLevel)}
            </span>
            <span className="inline-flex items-center gap-2 rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-1.5">
              <Bot className="h-3.5 w-3.5 text-emerald-200" />
              Realtime dialog
            </span>
            <span className="inline-flex items-center gap-2 rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-1.5">
              <Waves className="h-3.5 w-3.5 text-amber-200" />
              {note}
            </span>
          </div>
        </div>
      </div>
    </section>
  )
}
