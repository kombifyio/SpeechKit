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
    <div className="flex h-screen flex-col bg-[#131318] text-[13px] text-[#e4e1e9]">
      {/* Drag region / header */}
      <div
        className="flex shrink-0 items-center justify-between border-b border-[#35343a]/20 px-4 pt-3 pb-2"
        style={{ WebkitAppRegion: 'drag' } as CSSProperties}
      >
        <span className="text-xs font-semibold text-[#cabeff]">
          {noteId ? `Quick Note #${noteId}` : 'New Quick Note'}
        </span>
        {recording && (
          <span className="flex items-center gap-1.5 text-[10px] text-red-400">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-red-400" />
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
        className="mx-3 mt-3 flex-1 resize-none rounded-lg border border-[#484555]/50 bg-[#0e0e13] px-3 py-2 text-sm leading-relaxed text-[#e4e1e9]/85 outline-none placeholder:text-[#938ea1]/50 focus:border-[#947dff]/30"
      />

      {/* Summary section */}
      {summary && (
        <div className="mx-3 mt-2 rounded-lg border border-[#484555]/40 bg-[#1f1f25] px-3 py-2">
          <span className="text-[10px] font-semibold uppercase tracking-wider text-[#938ea1]">Summary</span>
          <p className="mt-1 text-xs leading-relaxed text-[#c9c4d8]">{summary}</p>
        </div>
      )}

      {/* Toolbar */}
      <div className="flex shrink-0 items-center gap-2 border-t border-[#35343a]/20 px-3 py-2.5">
        <button
          type="button"
          onClick={handleSave}
          className="rounded-lg signature-gradient px-4 py-1.5 text-xs font-bold text-[#2b0088] hover:opacity-90 transition-all"
        >
          Save
        </button>
        <button
          type="button"
          onClick={handleRecord}
          className="rounded-lg border border-[#484555]/50 px-3 py-1.5 text-xs text-[#938ea1] hover:bg-[#35343a]/50 hover:text-[#e4e1e9] transition-colors"
        >
          Record
        </button>
        <button
          type="button"
          onClick={handleSummary}
          className="rounded-lg border border-[#484555]/50 px-3 py-1.5 text-xs text-[#938ea1] hover:bg-[#35343a]/50 hover:text-[#e4e1e9] transition-colors"
        >
          Summary
        </button>
        <button
          type="button"
          onClick={handleEmail}
          className="rounded-lg border border-[#484555]/50 px-3 py-1.5 text-xs text-[#938ea1] hover:bg-[#35343a]/50 hover:text-[#e4e1e9] transition-colors"
        >
          Email
        </button>
        <span className="ml-auto text-[10px] text-[#938ea1]/60">{text.split(/\s+/).filter(Boolean).length} words</span>
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
