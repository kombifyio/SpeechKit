import { useCallback, useEffect, useRef, useState } from 'react'

type BubbleState = 'hidden' | 'entering' | 'visible' | 'exiting'
type AssistSurface = 'bubble' | 'panel' | 'silent' | 'action_ack'

type AssistSurfaceOptions = {
  surface?: AssistSurface
  inputText?: string
  title?: string
}

type AssistPanelPayload = AssistSurfaceOptions & {
  text?: string
  resultText?: string
}

const ENTER_DURATION = 200
const EXIT_DURATION = 300
const AUTO_HIDE_MS = 8000

function shouldUsePanel(text: string) {
  return text.length > 240 || text.split(/\r?\n/).filter(Boolean).length > 2
}

export function AssistBubble() {
  const [text, setText] = useState('')
  const [inputText, setInputText] = useState('')
  const [title, setTitle] = useState('Assist result')
  const [surface, setSurface] = useState<'bubble' | 'panel'>('bubble')
  const [bubbleState, setBubbleState] = useState<BubbleState>('hidden')
  const [copied, setCopied] = useState(false)
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearHideTimer = useCallback(() => {
    if (hideTimerRef.current) {
      clearTimeout(hideTimerRef.current)
      hideTimerRef.current = null
    }
  }, [])

  const hide = useCallback(() => {
    clearHideTimer()
    setBubbleState('exiting')
    setTimeout(() => {
      setBubbleState('hidden')
      setText('')
      setInputText('')
      setCopied(false)
    }, EXIT_DURATION)
  }, [clearHideTimer])

  const present = useCallback(
    (newText: string, options: AssistSurfaceOptions = {}) => {
      if (options.surface === 'silent') {
        hide()
        return
      }

      clearHideTimer()
      setCopied(false)
      setText(newText)
      setInputText(options.inputText ?? '')
      setTitle(options.title ?? 'Assist result')
      const nextSurface =
        options.surface === 'panel' || shouldUsePanel(newText)
          ? 'panel'
          : 'bubble'
      setSurface(nextSurface)
      setBubbleState('entering')

      setTimeout(() => {
        setBubbleState('visible')
      }, ENTER_DURATION)

      if (nextSurface === 'bubble') {
        hideTimerRef.current = setTimeout(() => {
          setBubbleState('exiting')
          setTimeout(() => {
            setBubbleState('hidden')
            setText('')
            setInputText('')
          }, EXIT_DURATION)
        }, AUTO_HIDE_MS)
      }
    },
    [clearHideTimer, hide],
  )

  const show = useCallback(
    (newText: string, options: AssistSurfaceOptions = {}) => {
      present(newText, options)
    },
    [present],
  )

  const showPanel = useCallback(
    (payload: string | AssistPanelPayload) => {
      if (typeof payload === 'string') {
        present(payload, { surface: 'panel' })
        return
      }
      present(payload.resultText ?? payload.text ?? '', {
        surface: 'panel',
        inputText: payload.inputText,
        title: payload.title,
      })
    },
    [present],
  )

  const copyResult = useCallback(() => {
    const writePromise = navigator.clipboard?.writeText(text)
    if (!writePromise) return
    void writePromise.then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    })
  }, [text])

  // Expose to Go backend via window.__assistBubble.
  useEffect(() => {
    const api = { show, showPanel, hide }
    ;(window as unknown as Record<string, unknown>).__assistBubble = api
    return () => {
      delete (window as unknown as Record<string, unknown>).__assistBubble
    }
  }, [show, showPanel, hide])

  if (bubbleState === 'hidden') return null

  const opacity =
    bubbleState === 'entering' ? 0 :
    bubbleState === 'exiting' ? 0 : 1

  const translateY =
    bubbleState === 'entering' ? 8 :
    bubbleState === 'exiting' ? -4 : 0

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'flex-start',
        padding: '12px 16px',
        pointerEvents: 'none',
        fontFamily: "'Geist', system-ui, -apple-system, sans-serif",
      }}
    >
      {surface === 'panel' ? (
        <div
          role="dialog"
          aria-label="Assist result panel"
          style={{
            width: '100%',
            maxWidth: 540,
            maxHeight: 326,
            overflow: 'hidden',
            background: 'rgba(18, 18, 22, 0.96)',
            backdropFilter: 'blur(16px)',
            border: '1px solid rgba(255, 255, 255, 0.1)',
            borderRadius: 10,
            color: '#fafafa',
            boxShadow: 'none',
            opacity,
            transform: `translateY(${translateY}px)`,
            transition: `opacity ${bubbleState === 'entering' ? ENTER_DURATION : EXIT_DURATION}ms ease, transform ${bubbleState === 'entering' ? ENTER_DURATION : EXIT_DURATION}ms ease`,
            pointerEvents: 'auto',
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
              padding: '12px 14px',
            }}
          >
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: 700 }}>{title}</div>
              {inputText && (
                <div
                  style={{
                    marginTop: 2,
                    maxWidth: 370,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    color: 'rgba(250, 250, 250, 0.58)',
                    fontSize: 11,
                  }}
                >
                  {inputText}
                </div>
              )}
            </div>
            <div style={{ display: 'flex', gap: 6 }}>
              <button type="button" onClick={copyResult} style={panelButtonStyle}>
                {copied ? 'Copied' : 'Copy'}
              </button>
              <button type="button" onClick={hide} style={panelButtonStyle}>
                Close
              </button>
            </div>
          </div>
          <div
            style={{
              maxHeight: 250,
              overflowY: 'auto',
              padding: '14px',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              fontSize: 13,
              lineHeight: 1.55,
              color: 'rgba(250, 250, 250, 0.84)',
            }}
          >
            {text}
          </div>
        </div>
      ) : (
        <div
          style={{
            maxWidth: 420,
            width: '100%',
            background: 'rgba(24, 24, 27, 0.92)',
            backdropFilter: 'blur(12px)',
            border: '1px solid rgba(255, 255, 255, 0.08)',
            borderRadius: 10,
            padding: '12px 16px',
            color: '#fafafa',
            fontSize: 13,
            lineHeight: 1.5,
            boxShadow: 'none',
            opacity,
            transform: `translateY(${translateY}px)`,
            transition: `opacity ${bubbleState === 'entering' ? ENTER_DURATION : EXIT_DURATION}ms ease, transform ${bubbleState === 'entering' ? ENTER_DURATION : EXIT_DURATION}ms ease`,
            pointerEvents: 'auto',
          }}
          onClick={hide}
          title="Click to dismiss"
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 8,
            }}
          >
            <div
              style={{
                width: 6,
                height: 6,
                borderRadius: '50%',
                background: '#a78bfa',
                marginTop: 5,
                flexShrink: 0,
              }}
            />
            <span style={{ flex: 1, wordBreak: 'break-word' }}>{text}</span>
          </div>
        </div>
      )}
    </div>
  )
}

const panelButtonStyle = {
  height: 28,
  borderRadius: 6,
  border: '1px solid rgba(255, 255, 255, 0.1)',
  background: 'rgba(255, 255, 255, 0.06)',
  color: 'rgba(250, 250, 250, 0.78)',
  padding: '0 10px',
  fontSize: 11,
  fontWeight: 600,
  cursor: 'pointer',
} as const
