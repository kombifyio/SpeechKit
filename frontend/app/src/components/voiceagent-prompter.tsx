import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react'
import { type AgentState } from '@livekit/components-react'
import { Bot, ChevronDown, ChevronUp, Headphones, MessageSquareText, Play, Square, Waves } from 'lucide-react'

import { AgentAudioVisualizerRadial } from '@/components/agent-audio-visualizer-radial'
import { DesktopWindowFrame } from '@/components/desktop-window-frame'
import { useDesktopTheme } from '@/lib/desktop-theme'

type PrompterMode = 'assist' | 'voice_agent'
type PrompterState =
  | 'inactive'
  | 'connecting'
  | 'listening'
  | 'processing'
  | 'speaking'
  | 'ready'
  | 'error'
  | 'deactivating'
type PrompterRole = 'user' | 'assistant' | 'system'
type LiveSpeakerRole = Exclude<PrompterRole, 'system'>

type PrompterMessage = {
  id: number
  role: PrompterRole
  text: string
  done: boolean
}

type LiveTurn = {
  text: string
  done: boolean
}

type PrompterAPI = {
  addMessage: (message: { role: PrompterRole; text: string; done: boolean }) => void
  clear: () => void
  updateState: (state: string) => void
  setMode: (mode: string) => void
  setActivity: (role: LiveSpeakerRole, level: number) => void
}

type WailsWindow = Window & {
  __prompter?: PrompterAPI
  wails?: {
    Events?: {
      Emit?: (name: string) => void
    }
  }
}

const stateMeta: Record<PrompterState, { color: string; assistLabel: string; voiceLabel: string }> = {
  inactive: { color: 'bg-muted-foreground/40', assistLabel: 'Ready', voiceLabel: 'Inactive' },
  connecting: { color: 'bg-primary', assistLabel: 'Starting…', voiceLabel: 'Connecting…' },
  listening: { color: 'bg-emerald-400', assistLabel: 'Listening', voiceLabel: 'Listening' },
  processing: { color: 'bg-amber-400', assistLabel: 'Generating…', voiceLabel: 'Processing…' },
  speaking: { color: 'bg-primary', assistLabel: 'Speaking', voiceLabel: 'Speaking' },
  ready: { color: 'bg-emerald-400', assistLabel: 'Response ready', voiceLabel: 'Ready' },
  error: { color: 'bg-red-400', assistLabel: 'Error', voiceLabel: 'Error' },
  deactivating: { color: 'bg-muted-foreground/40', assistLabel: 'Closing…', voiceLabel: 'Stopping…' },
}

const modeChrome = {
  assist: {
    appLabel: 'Assist',
    title: 'Assist',
    subtitle: 'Side panel',
    emptyText: 'Waiting for request...',
    icon: MessageSquareText,
    userIcon: Bot,
    assistantIcon: Waves,
  },
  voice_agent: {
    appLabel: 'Voice Agent',
    title: 'Voice Agent',
    subtitle: 'Live transcript',
    emptyText: 'Waiting for conversation...',
    icon: Headphones,
    userIcon: Headphones,
    assistantIcon: Waves,
  },
} as const

const liveSlotMeta: Record<
  LiveSpeakerRole,
  { label: string; idleText: string; color: `#${string}`; accentClassName: string; subtleClassName: string }
> = {
  user: {
    label: 'You',
    idleText: 'Waiting for you to speak',
    color: '#8FA9FF',
    accentClassName: 'text-sky-200',
    subtleClassName: 'bg-sky-400/10 border-sky-300/10',
  },
  assistant: {
    label: 'Voice Agent',
    idleText: 'Waiting for the next answer',
    color: '#47E3C1',
    accentClassName: 'text-emerald-200',
    subtleClassName: 'bg-emerald-400/10 border-emerald-300/10',
  },
}

const defaultActivityLevels: Record<LiveSpeakerRole, number> = {
  user: 0,
  assistant: 0,
}

const noDragRegionStyle = {
  ['--wails-draggable' as string]: 'no-drag',
  WebkitAppRegion: 'no-drag',
} as CSSProperties

function labelForState(mode: PrompterMode, state: PrompterState) {
  const meta = stateMeta[state]
  return mode === 'assist' ? meta.assistLabel : meta.voiceLabel
}

function clampLevel(level: number) {
  if (!Number.isFinite(level)) {
    return 0
  }
  return Math.max(0, Math.min(1, level))
}

function resetDormantActivityLevels(
  state: PrompterState,
  current: Record<LiveSpeakerRole, number>,
) {
  let nextLevels = current

  if (state !== 'listening' && nextLevels.user !== 0) {
    nextLevels = { ...nextLevels, user: 0 }
  }
  if (state !== 'speaking' && nextLevels.assistant !== 0) {
    nextLevels = { ...nextLevels, assistant: 0 }
  }

  return nextLevels
}

function liveSlotIsActive(role: LiveSpeakerRole, state: PrompterState, level: number) {
  if (role === 'user') {
    return state === 'listening' && level > 0.04
  }
  return state === 'speaking' && level > 0.04
}

function liveSlotVisualizerState(role: LiveSpeakerRole, state: PrompterState, active: boolean): AgentState {
  if (state === 'connecting') {
    return 'connecting'
  }
  if (state === 'processing') {
    return role === 'assistant' ? 'thinking' : 'listening'
  }
  if (state === 'speaking') {
    return role === 'assistant' && active ? 'speaking' : 'listening'
  }
  if (state === 'listening') {
    return role === 'user' && active ? 'speaking' : 'listening'
  }
  return 'listening'
}

function liveSlotStatusLabel(role: LiveSpeakerRole, state: PrompterState, active: boolean) {
  if (state === 'connecting') {
    return 'Connecting'
  }
  if (state === 'processing') {
    return role === 'assistant' ? 'Thinking' : 'Waiting'
  }
  if (role === 'user') {
    if (active) {
      return 'Talking'
    }
    return state === 'listening' ? 'Ready for you' : 'Paused'
  }
  if (active) {
    return 'Speaking'
  }
  if (state === 'speaking') {
    return 'Finishing'
  }
  return 'Standing by'
}

export function VoiceAgentPrompter() {
  const { theme, toggleTheme } = useDesktopTheme('dark')
  const [mode, setMode] = useState<PrompterMode>('voice_agent')
  const [messages, setMessages] = useState<PrompterMessage[]>([])
  const [liveTurns, setLiveTurns] = useState<Record<LiveSpeakerRole, LiveTurn | null>>({
    user: null,
    assistant: null,
  })
  const [liveNotice, setLiveNotice] = useState<string | null>(null)
  const [activityLevels, setActivityLevels] = useState<Record<LiveSpeakerRole, number>>(defaultActivityLevels)
  const [state, setState] = useState<PrompterState>('inactive')
  const [transcriptHidden, setTranscriptHidden] = useState(false)
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const shouldAutoscrollRef = useRef(true)
  const nextIdRef = useRef(1)
  const activeUserMessageIdRef = useRef<number | null>(null)
  const activeAssistantMessageIdRef = useRef<number | null>(null)

  const scrollToBottom = useCallback(() => {
    if (shouldAutoscrollRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [])

  const addMessage = useCallback(
    (message: { role: PrompterRole; text: string; done: boolean }) => {
      if (message.role === 'system') {
        const id = nextIdRef.current++
        setMessages((current) => [...current, { id, role: message.role, text: message.text, done: true }])
        activeUserMessageIdRef.current = null
        activeAssistantMessageIdRef.current = null
        setLiveNotice(message.text)
        setTimeout(scrollToBottom, 16)
        return
      }

      setLiveNotice(null)
      const liveRole = message.role as LiveSpeakerRole
      setLiveTurns((current) => {
        const previousTurn = current[liveRole]
        const nextText = previousTurn && !previousTurn.done
          ? mergeStreamingText(previousTurn.text, message.text)
          : message.text
        return {
          ...current,
          [liveRole]: {
            text: nextText,
            done: message.done,
          },
        }
      })

      const activeRef = message.role === 'user' ? activeUserMessageIdRef : activeAssistantMessageIdRef
      const otherActiveRef = message.role === 'user' ? activeAssistantMessageIdRef : activeUserMessageIdRef

      if (activeRef.current !== null && !message.done) {
        const activeMessageId = activeRef.current
        setMessages((current) =>
          current.map((entry) =>
            entry.id === activeMessageId
              ? { ...entry, text: mergeStreamingText(entry.text, message.text), done: false }
              : entry,
          ),
        )
      } else if (activeRef.current !== null && message.done) {
        const activeMessageId = activeRef.current
        setMessages((current) =>
          current.map((entry) =>
            entry.id === activeMessageId
              ? { ...entry, text: mergeStreamingText(entry.text, message.text), done: true }
              : entry,
          ),
        )
        activeRef.current = null
      } else {
        const id = nextIdRef.current++
        activeRef.current = message.done ? null : id
        otherActiveRef.current = null
        setMessages((current) => [...current, { id, role: message.role, text: message.text, done: message.done }])
      }

      setTimeout(scrollToBottom, 16)
    },
    [scrollToBottom],
  )

  const clear = useCallback(() => {
    setMessages([])
    setLiveTurns({
      user: null,
      assistant: null,
    })
    setLiveNotice(null)
    setActivityLevels(defaultActivityLevels)
    setState('inactive')
    activeUserMessageIdRef.current = null
    activeAssistantMessageIdRef.current = null
    nextIdRef.current = 1
  }, [])

  const updateState = useCallback((nextState: string) => {
    if (nextState in stateMeta) {
      const resolvedState = nextState as PrompterState
      setState(resolvedState)
      setActivityLevels((current) => resetDormantActivityLevels(resolvedState, current))
    }
  }, [])

  const updateMode = useCallback((nextMode: string) => {
    if (nextMode === 'assist' || nextMode === 'voice_agent') {
      setMode(nextMode)
    }
  }, [])

  const setActivity = useCallback((role: LiveSpeakerRole, level: number) => {
    const nextLevel = clampLevel(level)
    setActivityLevels((current) => {
      if (current[role] === nextLevel) {
        return current
      }
      return {
        ...current,
        [role]: nextLevel,
      }
    })
  }, [])

  const onScroll = useCallback(() => {
    if (!scrollRef.current) {
      return
    }
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
    shouldAutoscrollRef.current = scrollHeight - scrollTop - clientHeight < 40
  }, [])

  useEffect(() => {
    const win = window as WailsWindow
    win.__prompter = { addMessage, clear, updateState, setMode: updateMode, setActivity }
    return () => {
      delete win.__prompter
    }
  }, [addMessage, clear, setActivity, updateMode, updateState])

  const stopVoiceAgent = useCallback(() => {
    const win = window as WailsWindow
    win.wails?.Events?.Emit?.('voiceagent:stop')
  }, [])

  const startVoiceAgent = useCallback(() => {
    const win = window as WailsWindow
    win.wails?.Events?.Emit?.('voiceagent:start')
  }, [])

  const closePrompter = useCallback(() => {
    const win = window as WailsWindow
    win.wails?.Events?.Emit?.('voiceagent:close')
  }, [])

  const chrome = modeChrome[mode]
  const meta = stateMeta[state]
  const statusLabel = labelForState(mode, state)
  const voiceAgentRunning =
    mode === 'voice_agent' &&
    ['connecting', 'listening', 'processing', 'speaking', 'ready', 'deactivating'].includes(state)
  const HeaderIcon = chrome.icon

  return (
    <DesktopWindowFrame
      appLabel={chrome.appLabel}
      title={chrome.title}
      subtitle={chrome.subtitle}
      icon={<HeaderIcon className="h-5 w-5" />}
      theme={theme}
      onToggleTheme={toggleTheme}
      allowMaximise={false}
      onClose={closePrompter}
      contentClassName="bg-[color:var(--sk-surface-1)]/92"
      actions={(
        <>
          <div className="hidden items-center gap-2 rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-2 md:flex">
            <div className={`h-2.5 w-2.5 shrink-0 rounded-full ${meta.color}`} />
            <span className="text-xs font-medium text-[color:var(--sk-text-muted)]">{statusLabel}</span>
          </div>
          {mode === 'voice_agent' ? (
            voiceAgentRunning ? (
              <button
                type="button"
                onClick={stopVoiceAgent}
                style={noDragRegionStyle}
                aria-label="Stop voice agent"
                className="inline-flex items-center gap-2 rounded-full border border-red-400/18 bg-red-500/10 px-3 py-2 text-xs font-medium text-red-100 transition-colors hover:bg-red-500/16"
                title="Stop voice agent"
              >
                <Square className="h-3.5 w-3.5 fill-current" />
                Stop
              </button>
            ) : (
              <button
                type="button"
                onClick={startVoiceAgent}
                style={noDragRegionStyle}
                aria-label="Start voice agent"
                className="inline-flex items-center gap-2 rounded-full border border-emerald-400/18 bg-emerald-500/10 px-3 py-2 text-xs font-medium text-emerald-100 transition-colors hover:bg-emerald-500/16"
                title="Start voice agent"
              >
                <Play className="h-3.5 w-3.5 fill-current" />
                Start
              </button>
            )
          ) : null}
          <button
            type="button"
            onClick={() => setTranscriptHidden((current) => !current)}
            style={noDragRegionStyle}
            className="sk-secondary-button inline-flex items-center gap-2 rounded-full px-3 py-2 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
            title={transcriptHidden ? 'Show transcript' : 'Hide transcript'}
            aria-label={transcriptHidden ? 'Show transcript' : 'Hide transcript'}
          >
            {transcriptHidden ? <ChevronDown className="h-4 w-4" /> : <ChevronUp className="h-4 w-4" />}
            {transcriptHidden ? 'Show transcript' : 'Hide transcript'}
          </button>
        </>
      )}
    >
      {transcriptHidden ? (
        <div className="flex h-full items-center justify-center">
          <p className="text-xs text-[color:var(--sk-text-muted)]/70">Transcript hidden</p>
        </div>
      ) : mode === 'voice_agent' ? (
        <div className="flex h-full flex-col gap-4 overflow-y-auto px-4 py-4">
          {liveNotice ? (
            <div className="flex justify-center">
              <span className="rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-1 text-[11px] font-medium uppercase tracking-[0.18em] text-[color:var(--sk-text-muted)]/80">
                {liveNotice}
              </span>
            </div>
          ) : null}

          <LiveTurnSlot
            role="user"
            turn={liveTurns.user}
            state={state}
            level={activityLevels.user}
          />
          <LiveTurnSlot
            role="assistant"
            turn={liveTurns.assistant}
            state={state}
            level={activityLevels.assistant}
          />
        </div>
      ) : (
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="flex h-full flex-col gap-3 overflow-y-auto px-4 py-4"
        >
          {messages.length === 0 ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]">
                <HeaderIcon className="h-6 w-6" />
              </div>
              <p className="text-sm italic text-[color:var(--sk-text-muted)]/80">{chrome.emptyText}</p>
            </div>
          ) : null}

          {messages.map((message) => (
            <PrompterBubble key={message.id} message={message} mode={mode} />
          ))}
        </div>
      )}
    </DesktopWindowFrame>
  )
}

function LiveTurnSlot({
  role,
  turn,
  state,
  level,
}: {
  role: LiveSpeakerRole
  turn: LiveTurn | null
  state: PrompterState
  level: number
}) {
  const meta = liveSlotMeta[role]
  const active = liveSlotIsActive(role, state, level)
  const visualizerState = liveSlotVisualizerState(role, state, active)
  const statusLabel = liveSlotStatusLabel(role, state, active)

  return (
    <section
      data-testid={`${role}-live-slot`}
      data-active={active ? 'true' : 'false'}
      className={`rounded-[28px] border px-4 py-4 transition-colors sm:px-5 ${
        active
          ? 'border-[color:var(--sk-border-strong)] bg-[color:var(--sk-surface-2)] shadow-[0_0_0_1px_rgba(255,255,255,0.03)]'
          : `border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)]/70 ${meta.subtleClassName}`
      }`}
    >
      <div className="flex items-start gap-4">
        <div className="flex h-16 w-16 shrink-0 items-center justify-center rounded-[22px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)]">
          <AgentAudioVisualizerRadial
            size="sm"
            state={visualizerState}
            level={level}
            color={meta.color}
            className={active ? 'opacity-100' : 'opacity-70'}
          />
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className={`text-[11px] font-semibold uppercase tracking-[0.22em] ${meta.accentClassName}`}>
              {meta.label}
            </span>
            <span className="rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] px-2.5 py-1 text-[11px] font-medium text-[color:var(--sk-text-muted)]">
              {statusLabel}
            </span>
          </div>

          <p
            className={`mt-3 whitespace-pre-wrap break-words text-sm leading-relaxed sm:text-[15px] ${
              turn?.text ? 'text-[color:var(--sk-text)]' : 'italic text-[color:var(--sk-text-muted)]/78'
            }`}
          >
            {turn?.text || meta.idleText}
            {turn && !turn.done ? (
              <span className="ml-1 inline-block h-4 w-1.5 animate-pulse rounded-sm bg-[color:var(--sk-accent)]/60 align-text-bottom" />
            ) : null}
          </p>
        </div>
      </div>
    </section>
  )
}

function PrompterBubble({
  message,
  mode,
}: {
  message: PrompterMessage
  mode: PrompterMode
}) {
  if (message.role === 'system') {
    return (
      <div className="flex justify-center">
        <span className="text-xs italic text-muted-foreground/60">{message.text}</span>
      </div>
    )
  }

  const isUser = message.role === 'user'
  const chrome = modeChrome[mode]
  const RoleIcon = isUser ? chrome.userIcon : chrome.assistantIcon

  return (
    <div className={`flex ${isUser ? 'justify-start' : 'justify-end'}`}>
      <div
        className={`w-fit max-w-[92%] rounded-[24px] px-4 py-2.5 ${
          isUser ? 'bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text)]' : 'bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-text)]'
        }`}
      >
        <div className="flex items-start gap-2">
          <RoleIcon
            className={`mt-0.5 h-4 w-4 shrink-0 ${
              isUser ? 'text-[color:var(--sk-text-muted)]' : 'text-[color:var(--sk-accent)]'
            }`}
          />
          <p className="whitespace-pre-wrap break-words text-sm leading-relaxed">
            {message.text}
            {!message.done ? (
              <span className="ml-0.5 inline-block h-4 w-1.5 animate-pulse rounded-sm bg-[color:var(--sk-accent)]/60 align-text-bottom" />
            ) : null}
          </p>
        </div>
      </div>
    </div>
  )
}

function mergeStreamingText(currentText: string, nextText: string) {
  if (!currentText) {
    return nextText
  }
  if (!nextText || nextText === currentText) {
    return currentText
  }
  if (nextText.includes(currentText)) {
    return nextText
  }
  if (currentText.includes(nextText)) {
    return currentText
  }

  const maxOverlap = Math.min(currentText.length, nextText.length)
  for (let overlap = maxOverlap; overlap > 0; overlap -= 1) {
    if (currentText.slice(-overlap) === nextText.slice(0, overlap)) {
      return currentText + nextText.slice(overlap)
    }
  }

  return currentText + nextText
}
