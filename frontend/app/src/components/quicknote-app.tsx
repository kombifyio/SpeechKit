import { useEffect, useRef, useState } from 'react'
import { Mail, Mic, NotebookPen, Save, Sparkles } from 'lucide-react'

import { DesktopWindowFrame } from '@/components/desktop-window-frame'
import { useDesktopTheme } from '@/lib/desktop-theme'
import {
  createQuickNote,
  updateQuickNote,
  quickNoteSummary,
  quickNoteEmail,
  armQuickNoteRecording,
} from '@/lib/speechkit'

export function QuickNoteApp() {
  const { theme, toggleTheme } = useDesktopTheme('dark')
  const initialNoteId = (() => {
    const id = new URLSearchParams(window.location.search).get('id')
    return id ? Number(id) : null
  })()
  const [text, setText] = useState('')
  const [noteId, setNoteId] = useState<number | null>(initialNoteId)
  const [summary, setSummary] = useState('')
  const [toast, setToast] = useState('')
  const [recording, setRecording] = useState(false)
  const textRef = useRef<HTMLTextAreaElement>(null)
  const toastTimer = useRef<number | null>(null)

  useEffect(() => {
    if (initialNoteId) {
      const id = String(initialNoteId)
      fetch(`/quicknotes/get?id=${id}`)
        .then((r) => r.json())
        .then((data: { text?: string }) => {
          if (data.text) setText(data.text)
        })
        .catch(() => {})
    }
    textRef.current?.focus()
  }, [initialNoteId])

  const showToast = (msg: string) => {
    if (toastTimer.current) window.clearTimeout(toastTimer.current)
    setToast(msg)
    toastTimer.current = window.setTimeout(() => setToast(''), 1400)
  }

  const handleSave = async () => {
    if (!text.trim()) return
    try {
      if (noteId) {
        await updateQuickNote(noteId, text.trim())
      } else {
        const result = await createQuickNote(text.trim())
        setNoteId(result.id)
      }
      showToast('Saved')
    } catch {
      showToast('Save failed')
    }
  }

  const handleSummary = async () => {
    if (!noteId) {
      await handleSave()
    }
    try {
      const result = await quickNoteSummary(noteId ?? 0)
      setSummary(result)
    } catch {
      showToast('Summary failed')
    }
  }

  const handleEmail = async () => {
    if (!noteId) await handleSave()
    try {
      const result = await quickNoteEmail(noteId ?? 0)
      await navigator.clipboard.writeText(result)
      showToast('Email draft copied')
    } catch {
      showToast('Email failed')
    }
  }

  const handleRecord = async () => {
    try {
      let resolvedNoteId = noteId

      if (text.trim() && !noteId) {
        const result = await createQuickNote(text.trim())
        setNoteId(result.id)
        resolvedNoteId = result.id
      }

      await armQuickNoteRecording(resolvedNoteId ?? undefined)
      setRecording(true)
      showToast('Recording armed — press hotkey to dictate')

      const pollInterval = setInterval(async () => {
        try {
          const res = await fetch('/dashboard/quicknotes', { cache: 'no-store' })
          const notes = await res.json() as Array<{ id: number; text: string }>
          if (notes.length > 0) {
            const latest = notes[0]
            if (!noteId || latest.id !== noteId) {
              setNoteId(latest.id)
              setText((prev) => prev ? prev + '\n' + latest.text : latest.text)
              setRecording(false)
              showToast('Transcription added')
              clearInterval(pollInterval)
            }
          }
        } catch { /* ignore poll errors */ }
      }, 500)

      setTimeout(() => {
        clearInterval(pollInterval)
        setRecording(false)
      }, 30000)
    } catch {
      showToast('Failed to arm recording')
    }
  }

  return (
    <DesktopWindowFrame
      appLabel="Quick Note"
      title={noteId ? `Quick Note #${noteId}` : 'New Quick Note'}
      subtitle="Capture, refine, and forward a thought"
      icon={<NotebookPen className="h-5 w-5" />}
      theme={theme}
      onToggleTheme={toggleTheme}
      contentClassName="bg-[color:var(--sk-surface-1)]/92"
    >
      <div className="flex min-h-0 flex-1 flex-col text-[13px] text-[color:var(--sk-text)]">
        <div className="flex items-center justify-between border-b border-[color:var(--sk-shell-divider)] px-4 py-3">
          <div className="flex items-center gap-2 text-xs text-[color:var(--sk-text-muted)]">
            <span className="rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-2.5 py-1">
              {noteId ? `ID ${noteId}` : 'Draft'}
            </span>
            <span>{text.split(/\s+/).filter(Boolean).length} words</span>
          </div>
          {recording && (
            <span className="flex items-center gap-1.5 rounded-full bg-red-500/12 px-2.5 py-1 text-[10px] font-medium text-red-200">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-red-300" />
              Recording armed
            </span>
          )}
        </div>

        <textarea
          ref={textRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Start typing your note..."
          className="sk-input mx-4 mt-4 flex-1 resize-none rounded-[22px] px-4 py-3 text-sm leading-relaxed"
        />

        {summary && (
          <div className="mx-4 mt-3 rounded-[22px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-4 py-3">
            <span className="sk-kicker">Summary</span>
            <p className="mt-1 text-xs leading-relaxed text-[color:var(--sk-text-muted)]">{summary}</p>
          </div>
        )}

        <div className="flex shrink-0 items-center gap-2 border-t border-[color:var(--sk-shell-divider)] px-4 py-3">
          <QuickNoteActionButton accent onClick={handleSave} icon={<Save className="h-3.5 w-3.5" />}>
            Save
          </QuickNoteActionButton>
          <QuickNoteActionButton onClick={handleRecord} icon={<Mic className="h-3.5 w-3.5" />}>
            Record
          </QuickNoteActionButton>
          <QuickNoteActionButton onClick={handleSummary} icon={<Sparkles className="h-3.5 w-3.5" />}>
            Summary
          </QuickNoteActionButton>
          <QuickNoteActionButton onClick={handleEmail} icon={<Mail className="h-3.5 w-3.5" />}>
            Email
          </QuickNoteActionButton>
        </div>

        <div
          className={[
            'pointer-events-none fixed top-5 right-5 rounded-2xl border border-emerald-400/20 bg-emerald-500/12 px-3 py-1.5 text-xs text-emerald-100 transition-all',
            toast ? 'translate-y-0 opacity-100' : '-translate-y-2 opacity-0',
          ].join(' ')}
        >
          {toast || '\u00A0'}
        </div>
      </div>
    </DesktopWindowFrame>
  )
}

function QuickNoteActionButton({
  accent,
  onClick,
  icon,
  children,
}: {
  accent?: boolean
  onClick: () => void
  icon: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'inline-flex items-center gap-2 rounded-full px-4 py-2 text-xs font-medium transition-colors',
        accent
          ? 'sk-primary-button'
          : 'sk-secondary-button hover:bg-[color:var(--sk-surface-3)]',
      ].join(' ')}
    >
      {icon}
      {children}
    </button>
  )
}
