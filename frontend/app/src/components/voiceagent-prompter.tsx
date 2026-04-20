import { Suspense, lazy, useCallback, useEffect, useRef, useState, type CSSProperties, type RefObject } from 'react'
import { Bot, Headphones, MessageSquareText, PanelBottomClose, PanelBottomOpen, Play, Square, Waves } from 'lucide-react'
import { Events, Window as WailsWindow } from '@wailsio/runtime'

import { DesktopWindowFrame } from '@/components/desktop-window-frame'
import { useDesktopTheme } from '@/lib/desktop-theme'
import { fetchAudioOutputDevices, setAudioOutputDevice, type AudioDevice } from '@/lib/speechkit'

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

type PrompterWindow = globalThis.Window & {
  __prompter?: PrompterAPI
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
    subtitle: 'One-shot output surface',
    emptyText: 'Waiting for request...',
    icon: MessageSquareText,
    userIcon: Bot,
    assistantIcon: Waves,
  },
  voice_agent: {
    appLabel: 'Voice Agent',
    title: 'Voice Agent',
    subtitle: 'Realtime dialog surface',
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

const prompterWindowSize = {
  collapsed: { width: 340, height: 132 },
  expanded: { width: 390, height: 500 },
} as const

const noDragRegionStyle = {
  ['--wails-draggable' as string]: 'no-drag',
  WebkitAppRegion: 'no-drag',
} as CSSProperties

const LazySpeechKitAuraOrb = lazy(async () => {
  const module = await import('@/components/speechkit-aura-orb')
  return { default: module.SpeechKitAuraOrb }
})

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

function liveSlotStatusLabel(role: LiveSpeakerRole, state: PrompterState, level: number) {
  if (state === 'connecting') {
    return 'Connecting'
  }
  if (state === 'processing') {
    return role === 'assistant' ? 'Thinking' : 'Paused'
  }
  if (role === 'user') {
    if (state === 'listening' && level >= 0.18) {
      return 'Hearing you'
    }
    if (state === 'listening' && level >= 0.06) {
      return 'Listening'
    }
    return state === 'listening' ? 'Ready for you' : 'Paused'
  }
  if (state === 'speaking') {
    return 'Speaking'
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
  const [outputDevices, setOutputDevices] = useState<AudioDevice[]>([])
  const [selectedOutputDeviceId, setSelectedOutputDeviceId] = useState('')
  const [outputDeviceNote, setOutputDeviceNote] = useState('')
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const shouldAutoscrollRef = useRef(true)
  const nextIdRef = useRef(1)
  const activeUserMessageIdRef = useRef<number | null>(null)
  const activeAssistantMessageIdRef = useRef<number | null>(null)
  const lastSpeakerRoleRef = useRef<LiveSpeakerRole | null>(null)
  const didMountRef = useRef(false)

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
      const speakerChanged = lastSpeakerRoleRef.current !== null && lastSpeakerRoleRef.current !== liveRole
      setLiveTurns((current) => {
        const previousTurn = current[liveRole]
        const nextText = previousTurn && !previousTurn.done && !speakerChanged
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
      lastSpeakerRoleRef.current = liveRole

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
    lastSpeakerRoleRef.current = null
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
    const win = window as PrompterWindow
    win.__prompter = { addMessage, clear, updateState, setMode: updateMode, setActivity }
    return () => {
      delete win.__prompter
    }
  }, [addMessage, clear, setActivity, updateMode, updateState])

  useEffect(() => {
    if (mode !== 'voice_agent' || typeof window.fetch !== 'function') {
      return
    }
    let cancelled = false

    fetchAudioOutputDevices()
      .then((result) => {
        if (cancelled) {
          return
        }
        setOutputDevices(result.devices)
        setSelectedOutputDeviceId(result.selectedDeviceId)
        setOutputDeviceNote(result.devices.length === 0 ? 'System default output' : '')
      })
      .catch(() => {
        if (!cancelled) {
          setOutputDeviceNote('Using system default output')
        }
      })

    return () => {
      cancelled = true
    }
  }, [mode])

  useEffect(() => {
    if (!didMountRef.current) {
      didMountRef.current = true
      return
    }

    const target = transcriptHidden ? prompterWindowSize.collapsed : prompterWindowSize.expanded

    void Promise.all([WailsWindow.Size(), WailsWindow.Position()])
      .then(([size, position]) => {
        const nextX = position.x + size.width - target.width
        const nextY = position.y
        return WailsWindow.SetSize(target.width, target.height)
          .then(() => WailsWindow.SetPosition(nextX, nextY))
      })
      .catch(() => {})
  }, [transcriptHidden])

  const stopVoiceAgent = useCallback(() => {
    void Events.Emit('voiceagent:stop').catch(() => {})
  }, [])

  const startVoiceAgent = useCallback(() => {
    void Events.Emit('voiceagent:start').catch(() => {})
  }, [])

  const closePrompter = useCallback(() => {
    void Events.Emit('voiceagent:close').catch(() => {})
    void WailsWindow.Hide().catch(() => {})
  }, [])

  const changeOutputDevice = useCallback(async (deviceId: string) => {
    setSelectedOutputDeviceId(deviceId)
    setOutputDeviceNote(deviceId ? 'Switching speaker...' : 'Switching to system default...')
    try {
      await setAudioOutputDevice(deviceId)
      setOutputDeviceNote(deviceId ? 'Speaker selected' : 'System default output')
    } catch {
      setOutputDeviceNote('Could not switch speaker')
    }
  }, [])

  const chrome = modeChrome[mode]
  const statusLabel = labelForState(mode, state)
  const visibleActivityLevels = {
    user: state === 'listening' ? activityLevels.user : 0,
    assistant: state === 'speaking' ? activityLevels.assistant : 0,
  }
  const voiceAgentRunning =
    mode === 'voice_agent' &&
    ['connecting', 'listening', 'processing', 'speaking', 'ready', 'deactivating'].includes(state)
  const HeaderIcon = chrome.icon

  return (
    <DesktopWindowFrame
      appLabel={chrome.appLabel}
      title={chrome.title}
      subtitle={chrome.subtitle}
      icon={<HeaderIcon className="h-4 w-4" />}
      theme={theme}
      onToggleTheme={toggleTheme}
      density="compact"
      showThemeToggle={false}
      allowMaximise={false}
      onClose={closePrompter}
      contentClassName="bg-[color:var(--sk-surface-1)]/92"
      actions={(
        <>
          {mode === 'voice_agent' ? (
            voiceAgentRunning ? (
              <button
                type="button"
                onClick={stopVoiceAgent}
                style={noDragRegionStyle}
                aria-label="Stop voice agent"
                className="inline-flex h-8 items-center gap-1.5 rounded-full border border-red-400/18 bg-red-500/10 px-2.5 text-[11px] font-medium text-red-100 transition-colors hover:bg-red-500/16"
                title="Stop voice agent"
              >
                <Square className="h-3 w-3 fill-current" />
                Stop
              </button>
            ) : (
              <button
                type="button"
                onClick={startVoiceAgent}
                style={noDragRegionStyle}
                aria-label="Start voice agent"
                className="inline-flex h-8 items-center gap-1.5 rounded-full border border-emerald-400/18 bg-emerald-500/10 px-2.5 text-[11px] font-medium text-emerald-100 transition-colors hover:bg-emerald-500/16"
                title="Start voice agent"
              >
                <Play className="h-3 w-3 fill-current" />
                Start
              </button>
            )
          ) : null}
          <button
            type="button"
            onClick={() => setTranscriptHidden((current) => !current)}
            style={noDragRegionStyle}
            className="sk-secondary-button inline-flex h-8 w-8 items-center justify-center rounded-full transition-colors hover:bg-[color:var(--sk-surface-3)]"
            title={transcriptHidden ? 'Show transcript' : 'Hide transcript'}
            aria-label={transcriptHidden ? 'Show transcript' : 'Hide transcript'}
          >
            {transcriptHidden ? <PanelBottomOpen className="h-3.5 w-3.5" /> : <PanelBottomClose className="h-3.5 w-3.5" />}
          </button>
        </>
      )}
    >
      {transcriptHidden ? (
        mode === 'voice_agent' ? (
            <CollapsedVoiceAgentSurface
              state={state}
              statusLabel={statusLabel}
              liveNotice={liveNotice}
              activityLevels={visibleActivityLevels}
            />
        ) : (
          <CollapsedAssistSurface statusLabel={statusLabel} />
        )
      ) : mode === 'voice_agent' ? (
        <div
          data-testid="voice-agent-surface"
          className="relative flex h-full min-h-0 flex-col overflow-hidden px-2.5 py-2"
        >
          <div className="flex items-start justify-between gap-2 pb-1.5">
            <div className="min-w-0">
              <p className="text-[9px] font-semibold uppercase tracking-[0.22em] text-[color:var(--sk-text-muted)]/66">
                Live dialog
              </p>
              <p className="mt-0.5 line-clamp-1 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]/82">
                {voiceAgentDetailForState(state, liveNotice)}
              </p>
            </div>
            <span className="shrink-0 rounded-full bg-[color:var(--sk-surface-2)]/76 px-2 py-0.5 text-[9px] font-medium text-[color:var(--sk-text-muted)]/84">
              {statusLabel}
            </span>
          </div>

          <div className="relative flex shrink-0 justify-center py-0.5">
            <Suspense
              fallback={(
                <VoiceAgentOrbFallback
                  state={resolveVoiceAgentOrbState(state)}
                  className="w-[76px]"
                />
              )}
            >
              <LazySpeechKitAuraOrb
                state={resolveVoiceAgentOrbState(state)}
                userLevel={visibleActivityLevels.user}
                assistantLevel={visibleActivityLevels.assistant}
                className="w-[76px]"
              />
            </Suspense>
          </div>

          <SpeakerOutputControl
            devices={outputDevices}
            selectedDeviceId={selectedOutputDeviceId}
            note={outputDeviceNote}
            onChange={changeOutputDevice}
          />

          <div className="min-h-0 flex-1 overflow-y-auto pr-1">
            <LiveTurnSlot
              role="user"
              turn={liveTurns.user}
              state={state}
              level={visibleActivityLevels.user}
              compact
            />
            <LiveTurnSlot
              role="assistant"
              turn={liveTurns.assistant}
              state={state}
              level={visibleActivityLevels.assistant}
              compact
            />
          </div>
        </div>
      ) : (
        <AssistOutputView
          chrome={modeChrome.assist}
          messages={messages}
          scrollRef={scrollRef}
          onScroll={onScroll}
          icon={modeChrome.assist.icon}
        />
      )}
    </DesktopWindowFrame>
  )
}

function CollapsedVoiceAgentSurface({
  state,
  statusLabel,
  liveNotice,
  activityLevels,
}: {
  state: PrompterState
  statusLabel: string
  liveNotice: string | null
  activityLevels: Record<LiveSpeakerRole, number>
}) {
  return (
    <div
      data-testid="voice-agent-collapsed-surface"
      className="flex h-full min-h-0 items-center gap-3 px-3 py-2"
    >
      <div className="relative flex h-14 w-14 shrink-0 items-center justify-center">
        <Suspense
          fallback={(
            <VoiceAgentOrbFallback
              state={resolveVoiceAgentOrbState(state)}
              className="w-[48px]"
            />
          )}
        >
          <LazySpeechKitAuraOrb
            state={resolveVoiceAgentOrbState(state)}
            userLevel={activityLevels.user}
            assistantLevel={activityLevels.assistant}
            className="w-[48px]"
          />
        </Suspense>
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-[9px] font-semibold uppercase tracking-[0.22em] text-[color:var(--sk-text-muted)]/68">
            Live dialog
          </span>
          <span className="rounded-full bg-[color:var(--sk-surface-2)]/76 px-2 py-0.5 text-[9px] font-medium text-[color:var(--sk-text-muted)]/84">
            {statusLabel}
          </span>
        </div>
        <p className="mt-1 line-clamp-2 text-[12px] leading-snug text-[color:var(--sk-text)]/88">
          {voiceAgentDetailForState(state, liveNotice)}
        </p>
      </div>
    </div>
  )
}

function CollapsedAssistSurface({
  statusLabel,
}: {
  statusLabel: string
}) {
  return (
    <div
      data-testid="assist-collapsed-surface"
      className="flex h-full min-h-0 items-center gap-3 px-3 py-2"
    >
      <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]">
        <MessageSquareText className="h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-[9px] font-semibold uppercase tracking-[0.22em] text-[color:var(--sk-text-muted)]/68">
            Assist
          </span>
          <span className="rounded-full bg-[color:var(--sk-surface-2)]/76 px-2 py-0.5 text-[9px] font-medium text-[color:var(--sk-text-muted)]/84">
            {statusLabel}
          </span>
        </div>
        <p className="mt-1 line-clamp-2 text-[12px] leading-snug text-[color:var(--sk-text)]/88">
          Output hidden. Use the transcript control to expand.
        </p>
      </div>
    </div>
  )
}

function ModeSurfaceHeader({
  mode,
  statusLabel,
  detail,
}: {
  mode: PrompterMode
  statusLabel: string
  detail: string
}) {
  const chrome = modeChrome[mode]

  return (
    <div className="flex items-end justify-between gap-3 border-b border-[color:var(--sk-panel-border)] pb-3">
      <div className="min-w-0">
        <p className="text-[11px] font-semibold uppercase tracking-[0.24em] text-[color:var(--sk-text-muted)]/72">
          {chrome.title}
        </p>
        <p className="mt-1 text-sm text-[color:var(--sk-text-muted)]/84">
          {detail}
        </p>
      </div>
      <div className="shrink-0 rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-1.5 text-[11px] font-medium text-[color:var(--sk-text-muted)]/84">
        {statusLabel}
      </div>
    </div>
  )
}

function AssistOutputView({
  chrome,
  messages,
  scrollRef,
  onScroll,
  icon: HeaderIcon,
}: {
  chrome: (typeof modeChrome)['assist']
  messages: PrompterMessage[]
  scrollRef: RefObject<HTMLDivElement | null>
  onScroll: () => void
  icon: typeof Bot
}) {
  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      className="flex h-full flex-col gap-3 overflow-y-auto px-4 py-4"
    >
      <ModeSurfaceHeader
        mode="assist"
        statusLabel={chrome.subtitle}
        detail="One-shot utilities, code words, and output-ready text."
      />

      {messages.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]">
            <HeaderIcon className="h-6 w-6" />
          </div>
          <p className="text-sm italic text-[color:var(--sk-text-muted)]/80">{chrome.emptyText}</p>
        </div>
      ) : null}

      {messages.map((message) => (
        <PrompterBubble key={message.id} message={message} mode="assist" />
      ))}
    </div>
  )
}

function VoiceAgentOrbFallback({
  state,
  className,
}: {
  state: ReturnType<typeof resolveVoiceAgentOrbState>
  className?: string
}) {
  const active = state !== 'inactive' && state !== 'error'

  return (
    <div
      data-testid="voice-agent-orb-fallback"
      data-state={state}
      aria-hidden="true"
      className={[
        'relative mx-auto flex aspect-square items-center justify-center rounded-full',
        'bg-[radial-gradient(circle_at_50%_45%,rgba(255,255,255,0.22),rgba(255,255,255,0.06)_36%,rgba(255,255,255,0)_72%)]',
        active ? 'animate-pulse opacity-80' : 'opacity-48',
        className ?? '',
      ].join(' ')}
    >
      <div className="h-5 w-5 rounded-full border border-white/15 bg-white/16" />
    </div>
  )
}

function SpeakerOutputControl({
  devices,
  selectedDeviceId,
  note,
  onChange,
}: {
  devices: AudioDevice[]
  selectedDeviceId: string
  note: string
  onChange: (deviceId: string) => void
}) {
  return (
    <div
      className="mb-0.5 flex shrink-0 items-center gap-1.5 border-y border-[color:var(--sk-panel-border)]/62 py-1.5"
      style={noDragRegionStyle}
    >
      <label
        htmlFor="voice-agent-speaker-output"
        className="shrink-0 text-[9px] font-semibold uppercase tracking-[0.18em] text-[color:var(--sk-text-muted)]/68"
      >
        Speaker
      </label>
      <select
        id="voice-agent-speaker-output"
        aria-label="Speaker output"
        value={selectedDeviceId}
        onChange={(event) => onChange(event.currentTarget.value)}
        className="min-w-0 flex-1 rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)]/70 px-2 py-1 text-[11px] text-[color:var(--sk-text)] outline-none transition-colors hover:bg-[color:var(--sk-surface-2)] focus:border-[color:var(--sk-border-strong)]"
      >
        <option value="">System default</option>
        {devices.map((device) => (
          <option key={device.deviceId} value={device.deviceId}>
            {device.label || (device.isDefault ? 'Default speaker' : 'Speaker')}
          </option>
        ))}
      </select>
      {note ? (
        <span className="hidden shrink-0 text-[10px] text-[color:var(--sk-text-muted)]/60 xl:inline">
          {note}
        </span>
      ) : null}
    </div>
  )
}

function LiveTurnSlot({
  role,
  turn,
  state,
  level = 0,
  compact = false,
}: {
  role: LiveSpeakerRole
  turn: LiveTurn | null
  state: PrompterState
  level?: number
  compact?: boolean
}) {
  const meta = liveSlotMeta[role]
  const active = role === 'assistant' ? state === 'speaking' : state === 'listening'
  const statusLabel = liveSlotStatusLabel(role, state, level)

  return (
    <article
      data-testid={`voice-agent-live-${role}`}
      data-cardless="true"
      data-active={active ? 'true' : 'false'}
      data-level={level.toFixed(2)}
      className="border-b border-[color:var(--sk-panel-border)]/58 py-2 last:border-b-0"
    >
      <div className="min-w-0">
        <div className="flex flex-wrap items-baseline gap-1.5">
          <span className={`text-[10px] font-semibold uppercase tracking-[0.2em] ${meta.accentClassName}`}>
            {meta.label}
          </span>
          <span className="text-[9px] font-medium text-[color:var(--sk-text-muted)]/66">
            {statusLabel}
          </span>
        </div>

        <p
          className={`mt-1.5 whitespace-pre-wrap break-words text-[13px] leading-relaxed ${
            turn?.text ? 'text-[color:var(--sk-text)]' : 'italic text-[color:var(--sk-text-muted)]/78'
          } ${compact ? 'max-h-20 overflow-y-auto pr-1' : ''}`}
        >
          {turn?.text || meta.idleText}
          {turn && !turn.done ? (
            <span className="ml-1 inline-block h-4 w-1.5 animate-pulse rounded-sm bg-[color:var(--sk-accent)]/60 align-text-bottom" />
          ) : null}
        </p>
      </div>
    </article>
  )
}

function voiceAgentDetailForState(state: PrompterState, liveNotice: string | null) {
  if (liveNotice) {
    return liveNotice
  }

  switch (state) {
    case 'connecting':
      return 'Bringing the realtime session online.'
    case 'listening':
      return 'Recording. Speak while the key is held.'
    case 'processing':
      return 'Hold released. Finishing the answer.'
    case 'speaking':
      return 'Responding live.'
    case 'ready':
      return 'The conversation is ready for the next turn.'
    case 'deactivating':
      return 'Shutting down the live dialog.'
    case 'error':
      return 'The voice session needs attention.'
    case 'inactive':
    default:
      return 'Waiting for a voice session to begin.'
  }
}

function resolveVoiceAgentOrbState(state: PrompterState) {
  switch (state) {
    case 'connecting':
      return 'connecting'
    case 'listening':
      return 'listening'
    case 'processing':
      return 'processing'
    case 'speaking':
      return 'speaking'
    case 'ready':
    case 'deactivating':
      return 'settling'
    case 'error':
      return 'error'
    case 'inactive':
    default:
      return 'inactive'
  }
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
