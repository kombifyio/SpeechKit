import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { Check, Mic, NotebookPen } from 'lucide-react'

import { DesktopWindowFrame } from '@/components/desktop-window-frame'
import { useDesktopTheme } from '@/lib/desktop-theme'
import { updateQuickNote } from '@/lib/speechkit'
import { useAutoClose } from '@/hooks/use-auto-close'

/**
 * Quick Capture: minimal frameless notepad.
 * Backend creates an empty note and passes its ID via ?noteId=X.
 * Backend auto-starts recording. Polls only THIS note for text updates.
 * Auto-stops on silence. Only UI: textarea + checkmark to save & close.
 */
export function QuickCaptureApp() {
  const { theme, toggleTheme } = useDesktopTheme('dark')
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
    <DesktopWindowFrame
      appLabel="Quick Capture"
      title="Create Quick Note"
      subtitle={recording ? 'Listening for speech' : 'Edit and confirm the capture'}
      icon={<NotebookPen className="h-5 w-5" />}
      theme={theme}
      onToggleTheme={toggleTheme}
      allowMaximise={false}
      onClose={closeWindow}
      contentClassName="bg-[color:var(--sk-surface-1)]/92"
    >
      <div className="flex min-h-0 flex-1 flex-col text-[color:var(--sk-text)]">
        <div className="flex items-center justify-between border-b border-[color:var(--sk-shell-divider)] px-4 py-3 text-xs text-[color:var(--sk-text-muted)]">
          <div className="flex items-center gap-2">
            <span className="rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-2.5 py-1">
              {noteId > 0 ? `Note ${noteId}` : 'Untitled'}
            </span>
            <span>{recording ? 'Auto-stop on silence' : 'Ready to save'}</span>
          </div>
          {recording && (
            <span className="flex items-center gap-1.5 rounded-full bg-amber-500/14 px-2.5 py-1 text-[10px] font-medium text-amber-100">
              <Mic className="h-3 w-3" />
              Listening
            </span>
          )}
        </div>

        {recording && (
          <div className="mx-4 mt-4 h-[3px] flex-shrink-0 rounded-full bg-gradient-to-r from-transparent via-[color:var(--sk-accent)] to-transparent animate-pulse" />
        )}

        <textarea
          ref={textRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={recording ? 'Listening... stops automatically on silence' : 'Type or speak...'}
          className="mx-4 mb-4 mt-4 flex-1 resize-none rounded-[22px] border border-[color:var(--sk-panel-border)] bg-transparent px-4 py-3 text-sm leading-relaxed text-[color:var(--sk-text)] outline-none placeholder:text-[color:var(--sk-text-muted)]/70"
          style={{ WebkitAppRegion: 'no-drag' } as CSSProperties}
        />

        <div className="flex flex-shrink-0 justify-end px-4 pb-4">
          <button
            type="button"
            onClick={handleDone}
            className={[
              'inline-flex items-center gap-2 rounded-full px-4 py-2 text-xs font-medium transition-all',
              saved
                ? 'border border-emerald-400/20 bg-emerald-500/20 text-emerald-100'
                : text.trim()
                  ? 'sk-primary-button'
                  : 'sk-secondary-button',
            ].join(' ')}
            title="Save & close"
            style={{ WebkitAppRegion: 'no-drag' } as CSSProperties}
          >
            <Check className="h-4 w-4" />
            Save & close
          </button>
        </div>
      </div>
    </DesktopWindowFrame>
  )
}
