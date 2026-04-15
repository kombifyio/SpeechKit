import { useCallback, useEffect, useRef, useState } from 'react'

type BubbleState = 'hidden' | 'entering' | 'visible' | 'exiting'

const ENTER_DURATION = 200
const EXIT_DURATION = 300
const AUTO_HIDE_MS = 8000

export function AssistBubble() {
  const [text, setText] = useState('')
  const [bubbleState, setBubbleState] = useState<BubbleState>('hidden')
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const show = useCallback((newText: string) => {
    if (hideTimerRef.current) {
      clearTimeout(hideTimerRef.current)
      hideTimerRef.current = null
    }

    setText(newText)
    setBubbleState('entering')

    setTimeout(() => {
      setBubbleState('visible')
    }, ENTER_DURATION)

    hideTimerRef.current = setTimeout(() => {
      setBubbleState('exiting')
      setTimeout(() => {
        setBubbleState('hidden')
        setText('')
      }, EXIT_DURATION)
    }, AUTO_HIDE_MS)
  }, [])

  const hide = useCallback(() => {
    if (hideTimerRef.current) {
      clearTimeout(hideTimerRef.current)
      hideTimerRef.current = null
    }
    setBubbleState('exiting')
    setTimeout(() => {
      setBubbleState('hidden')
      setText('')
    }, EXIT_DURATION)
  }, [])

  // Expose to Go backend via window.__assistBubble
  useEffect(() => {
    const api = { show, hide }
    ;(window as unknown as Record<string, unknown>).__assistBubble = api
    return () => {
      delete (window as unknown as Record<string, unknown>).__assistBubble
    }
  }, [show, hide])

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
      <div
        style={{
          maxWidth: 420,
          width: '100%',
          background: 'rgba(24, 24, 27, 0.92)',
          backdropFilter: 'blur(12px)',
          border: '1px solid rgba(255, 255, 255, 0.08)',
          borderRadius: 12,
          padding: '12px 16px',
          color: '#fafafa',
          fontSize: 13,
          lineHeight: 1.5,
          boxShadow: '0 4px 24px rgba(0, 0, 0, 0.4), 0 0 0 1px rgba(255,255,255,0.04)',
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
    </div>
  )
}
