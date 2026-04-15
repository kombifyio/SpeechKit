import { useCallback, useEffect, useRef, useState } from 'react'

type PrompterState = 'inactive' | 'connecting' | 'listening' | 'processing' | 'speaking' | 'deactivating'
type PrompterRole = 'user' | 'assistant' | 'system'

type PrompterMessage = {
  id: number
  role: PrompterRole
  text: string
  done: boolean
}

type PrompterAPI = {
  addMessage: (message: { role: PrompterRole; text: string; done: boolean }) => void
  clear: () => void
  updateState: (state: string) => void
}

type WailsWindow = Window & {
  __prompter?: PrompterAPI
  wails?: {
    Events?: {
      Emit?: (name: string) => void
    }
  }
}

const stateMeta: Record<PrompterState, { color: string; label: string }> = {
  inactive: { color: 'bg-muted-foreground/40', label: 'Inactive' },
  connecting: { color: 'bg-primary', label: 'Connecting...' },
  listening: { color: 'bg-emerald-400', label: 'Listening' },
  processing: { color: 'bg-amber-400', label: 'Processing' },
  speaking: { color: 'bg-primary', label: 'Speaking' },
  deactivating: { color: 'bg-muted-foreground/40', label: 'Stopping...' },
}

export function VoiceAgentPrompter() {
  const [messages, setMessages] = useState<PrompterMessage[]>([])
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
        setTimeout(scrollToBottom, 16)
        return
      }

      const activeRef = message.role === 'user' ? activeUserMessageIdRef : activeAssistantMessageIdRef
      const otherActiveRef = message.role === 'user' ? activeAssistantMessageIdRef : activeUserMessageIdRef

      if (activeRef.current !== null && !message.done) {
        setMessages((current) =>
          current.map((entry) =>
            entry.id === activeRef.current ? { ...entry, text: message.text, done: false } : entry,
          ),
        )
      } else if (activeRef.current !== null && message.done) {
        setMessages((current) =>
          current.map((entry) =>
            entry.id === activeRef.current ? { ...entry, text: message.text, done: true } : entry,
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
    activeUserMessageIdRef.current = null
    activeAssistantMessageIdRef.current = null
    nextIdRef.current = 1
  }, [])

  const updateState = useCallback((nextState: string) => {
    if (nextState in stateMeta) {
      setState(nextState as PrompterState)
    }
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
    win.__prompter = { addMessage, clear, updateState }
    return () => {
      delete win.__prompter
    }
  }, [addMessage, clear, updateState])

  const stopVoiceAgent = useCallback(() => {
    const win = window as WailsWindow
    win.wails?.Events?.Emit?.('voiceagent:stop')
  }, [])

  const meta = stateMeta[state]
  const showStopButton = state !== 'inactive'

  return (
    <div className="dark h-screen w-screen select-none bg-background font-sans text-foreground">
      <div
        className="flex items-center justify-between border-b border-outline-variant/20 bg-card px-4 py-2.5"
        style={{ ['--wails-draggable' as string]: 'drag' }}
      >
        <div className="flex items-center gap-2.5">
          <div className={`h-2.5 w-2.5 shrink-0 rounded-full ${meta.color}`} />
          <span className="text-xs font-medium text-muted-foreground">{meta.label}</span>
        </div>
        <div className="flex items-center gap-1">
          {showStopButton ? (
            <button
              type="button"
              onClick={stopVoiceAgent}
              className="rounded p-1 text-destructive transition-colors hover:bg-destructive/15"
              title="Stop voice agent"
            >
              <span className="material-symbols-outlined text-base">stop_circle</span>
            </button>
          ) : null}
          <button
            type="button"
            onClick={() => setTranscriptHidden((current) => !current)}
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            title={transcriptHidden ? 'Show transcript' : 'Hide transcript'}
          >
            <span className="material-symbols-outlined text-base">
              {transcriptHidden ? 'expand_more' : 'expand_less'}
            </span>
          </button>
        </div>
      </div>

      {transcriptHidden ? (
        <div className="flex h-[calc(100vh-50px)] items-center justify-center">
          <p className="text-xs text-muted-foreground/50">Transcript hidden</p>
        </div>
      ) : (
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="flex h-[calc(100vh-50px)] flex-col gap-3 overflow-y-auto px-4 py-3"
        >
          {messages.length === 0 ? (
            <div className="flex flex-1 items-center justify-center">
              <p className="text-sm italic text-muted-foreground/50">Waiting for conversation...</p>
            </div>
          ) : null}

          {messages.map((message) => (
            <PrompterBubble key={message.id} message={message} />
          ))}
        </div>
      )}
    </div>
  )
}

function PrompterBubble({ message }: { message: PrompterMessage }) {
  if (message.role === 'system') {
    return (
      <div className="flex justify-center">
        <span className="text-xs italic text-muted-foreground/60">{message.text}</span>
      </div>
    )
  }

  const isUser = message.role === 'user'

  return (
    <div className={`flex ${isUser ? 'justify-start' : 'justify-end'}`}>
      <div
        className={`max-w-[85%] rounded-2xl px-3.5 py-2 ${
          isUser ? 'bg-secondary text-secondary-foreground' : 'bg-primary/20 text-foreground'
        }`}
      >
        <div className="flex items-start gap-2">
          <span
            className={`material-symbols-outlined mt-0.5 shrink-0 text-sm ${
              isUser ? 'text-muted-foreground' : 'text-primary'
            }`}
            style={{ fontVariationSettings: "'FILL' 1" }}
          >
            {isUser ? 'mic' : 'auto_awesome'}
          </span>
          <p className="break-words text-sm leading-relaxed">
            {message.text}
            {!message.done ? (
              <span className="ml-0.5 inline-block h-4 w-1.5 animate-pulse rounded-sm bg-primary/60 align-text-bottom" />
            ) : null}
          </p>
        </div>
      </div>
    </div>
  )
}
