import { useEffect, useRef, useState } from 'react'
import { Check } from 'lucide-react'
import { updateQuickNote } from '@/lib/speechkit'
import { useAutoClose } from '@/hooks/use-auto-close'

/**
 * Quick Capture: minimal frameless notepad.
 * Backend creates an empty note and passes its ID via ?noteId=X.
 * Backend auto-starts recording. Polls only THIS note for text updates.
 * Auto-stops on silence. Only UI: textarea + checkmark to save & close.
 */
export function QuickCaptureApp() {
  const [text, setText] = useState('')
  const [noteId] = useState(() => {
    const params = new URLSearchParams(window.location.search)
    return Number(params.get('noteId')) || 0
  })
  const [recording, setRecording] = useState(true)
  const [saved, setSaved] = useState(false)
  const textRef = useRef<HTMLTextAreaElement>(null)
  const pollRef = useRef<number | null>(null)

  const closeWindow = () => {
    void fetch('/quicknotes/close-capture', { method: 'POST' })
  }

  // Auto-close on blur (user clicks elsewhere) or 60s idle
  useAutoClose({
    onClose: closeWindow,
    onBeforeClose: () => {
      if (text.trim() && noteId) {
        void updateQuickNote(noteId, text.trim())
      }
    },
    idleTimeoutMs: 60_000,
    enabled: !recording, // don't auto-close while recording
  })

  useEffect(() => {
    textRef.current?.focus()

    if (!noteId) return

    // Poll only THIS specific note for text updates (session-scoped, no data leak)
    pollRef.current = window.setInterval(async () => {
      try {
        const res = await fetch(`/quicknotes/get?id=${noteId}`, { cache: 'no-store' })
        const data = (await res.json()) as { text?: string }
        if (data.text) {
          setText(data.text)
          setRecording(false)
          // Keep polling in case user records again
        }
      } catch {
        /* ignore */
      }
    }, 400)

    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [noteId])

  const handleDone = async () => {
    // Save text if present, then always close
    if (text.trim() && noteId) {
      try {
        await updateQuickNote(noteId, text.trim())
        setSaved(true)
      } catch { /* ignore */ }
    }
    closeWindow()
  }

  return (
    <div
      className="flex h-screen flex-col bg-[#0b0f14] selection:bg-orange-500/30"
      style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
    >
      {recording && (
        <div className="h-[2px] w-full flex-shrink-0 bg-gradient-to-r from-transparent via-orange-500 to-transparent animate-pulse" />
      )}

      <textarea
        ref={textRef}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={recording ? 'Listening... stops automatically on silence' : 'Type or speak...'}
        className="flex-1 resize-none bg-transparent px-4 py-3 text-sm leading-relaxed text-white/85 outline-none placeholder:text-white/25"
        style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
      />

      <div className="flex flex-shrink-0 justify-end px-3 pb-3">
        <button
          type="button"
          onClick={handleDone}
          className={[
            'flex h-7 w-7 items-center justify-center rounded-full transition-all',
            saved
              ? 'bg-emerald-500/30 text-emerald-300'
              : text.trim()
                ? 'bg-orange-500/20 text-orange-300 hover:bg-orange-500/30'
                : 'text-white/20 hover:text-white/40',
          ].join(' ')}
          title="Save & close"
          style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
        >
          <Check className="h-4 w-4" />
        </button>
      </div>
    </div>
  )
}
