import { useEffect, useRef, useState, type CSSProperties } from 'react'
import {
  createQuickNote,
  updateQuickNote,
  quickNoteSummary,
  quickNoteEmail,
  armQuickNoteRecording,
} from '@/lib/speechkit'

export function QuickNoteApp() {
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

      // If note exists, save first so we have an ID
      if (text.trim() && !noteId) {
        const result = await createQuickNote(text.trim())
        setNoteId(result.id)
        resolvedNoteId = result.id
      }

      await armQuickNoteRecording(resolvedNoteId ?? undefined)
      setRecording(true)
      showToast('Recording armed — press hotkey to dictate')

      // Poll for the transcription result (checks latest quick note)
      const pollInterval = setInterval(async () => {
        try {
          const res = await fetch('/dashboard/quicknotes', { cache: 'no-store' })
          const notes = await res.json() as Array<{ id: number; text: string }>
          if (notes.length > 0) {
            const latest = notes[0]
            // If a new note appeared or existing note was updated with transcribed text
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

      // Stop polling after 30s
      setTimeout(() => {
        clearInterval(pollInterval)
        setRecording(false)
      }, 30000)
    } catch {
      showToast('Failed to arm recording')
    }
  }

  return (
    <div className="flex h-screen flex-col bg-[#0b0f14] text-[13px] text-white/90">
      {/* Drag region */}
      <div
        className="flex flex-shrink-0 items-center justify-between px-4 pt-3 pb-2"
        style={{ WebkitAppRegion: 'drag' } as CSSProperties}
      >
        <span className="text-xs font-semibold text-white/50">
          {noteId ? `Quick Note #${noteId}` : 'New Quick Note'}
        </span>
        {recording && (
          <span className="flex items-center gap-1.5 text-[10px] text-orange-400">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-orange-400" />
            Recording armed
          </span>
        )}
      </div>

      {/* Editor */}
      <textarea
        ref={textRef}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Start typing your note..."
        className="mx-3 flex-1 resize-none rounded-lg border border-white/8 bg-white/[0.03] px-3 py-2 text-sm leading-relaxed text-white/85 outline-none placeholder:text-white/20 focus:border-orange-400/30"
      />

      {/* Summary section */}
      {summary && (
        <div className="mx-3 mt-2 rounded-lg border border-white/6 bg-white/[0.02] px-3 py-2">
          <span className="text-[10px] font-semibold uppercase tracking-wider text-white/30">
            Summary
          </span>
          <p className="mt-1 text-xs leading-relaxed text-white/60">{summary}</p>
        </div>
      )}

      {/* Toolbar */}
      <div className="flex flex-shrink-0 items-center gap-2 px-3 py-2.5">
        <button
          type="button"
          onClick={handleSave}
          className="rounded-lg bg-orange-500/20 px-3 py-1.5 text-xs font-medium text-orange-200 hover:bg-orange-500/30"
        >
          Save
        </button>
        <button
          type="button"
          onClick={handleRecord}
          className="rounded-lg border border-white/10 px-3 py-1.5 text-xs text-white/50 hover:bg-white/5 hover:text-white/70"
        >
          Record
        </button>
        <button
          type="button"
          onClick={handleSummary}
          className="rounded-lg border border-white/10 px-3 py-1.5 text-xs text-white/50 hover:bg-white/5 hover:text-white/70"
        >
          Summary
        </button>
        <button
          type="button"
          onClick={handleEmail}
          className="rounded-lg border border-white/10 px-3 py-1.5 text-xs text-white/50 hover:bg-white/5 hover:text-white/70"
        >
          Email
        </button>
      </div>

      {/* Toast */}
      <div
        className={[
          'pointer-events-none fixed top-3 right-3 rounded-lg border border-emerald-400/20 bg-emerald-500/10 px-3 py-1.5 text-xs text-emerald-200 transition-all',
          toast ? 'translate-y-0 opacity-100' : '-translate-y-2 opacity-0',
        ].join(' ')}
      >
        {toast || '\u00A0'}
      </div>
    </div>
  )
}
