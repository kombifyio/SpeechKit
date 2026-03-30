import { useEffect, useRef } from 'react'

/**
 * Auto-close hook for popup/overlay windows.
 * Fires `onClose` when:
 * - The window loses focus (user clicks elsewhere)
 * - An optional idle timeout expires without interaction
 *
 * Reusable across Quick Capture, Quick Note editor, and future popups.
 */
export function useAutoClose({
  onClose,
  onBeforeClose,
  idleTimeoutMs,
  enabled = true,
}: {
  /** Called to close/destroy the window */
  onClose: () => void
  /** Optional: called before close to save state (can be async) */
  onBeforeClose?: () => void | Promise<void>
  /** Optional: auto-close after this many ms of idle (no keypress/mouse) */
  idleTimeoutMs?: number
  /** Set to false to temporarily disable (e.g., during recording) */
  enabled?: boolean
}) {
  const idleTimer = useRef<number | null>(null)

  const doClose = () => {
    if (onBeforeClose) {
      const result = onBeforeClose()
      if (result instanceof Promise) {
        void result.then(onClose)
        return
      }
    }
    onClose()
  }

  // Close on window blur (user clicks elsewhere)
  useEffect(() => {
    if (!enabled) return

    const onBlur = () => {
      setTimeout(() => {
        if (!document.hasFocus()) {
          doClose()
        }
      }, 200)
    }

    window.addEventListener('blur', onBlur)
    return () => window.removeEventListener('blur', onBlur)
  })

  // Idle timeout: reset on any interaction, close when expired
  useEffect(() => {
    if (!enabled || !idleTimeoutMs) return

    const resetTimer = () => {
      if (idleTimer.current) window.clearTimeout(idleTimer.current)
      idleTimer.current = window.setTimeout(doClose, idleTimeoutMs)
    }

    resetTimer()
    window.addEventListener('keydown', resetTimer)
    window.addEventListener('mousemove', resetTimer)
    window.addEventListener('mousedown', resetTimer)

    return () => {
      if (idleTimer.current) window.clearTimeout(idleTimer.current)
      window.removeEventListener('keydown', resetTimer)
      window.removeEventListener('mousemove', resetTimer)
      window.removeEventListener('mousedown', resetTimer)
    }
  })
}
