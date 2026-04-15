import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  dashboardAudioDownloadURL,
  deleteQuickNote,
  fetchHistory,
  fetchQuickNotes,
  pinQuickNote,
  revealDashboardAudio,
  type QuickNote,
  type TranscriptionRecord,
} from '@/lib/speechkit'

function sortByNewest<T>(items: T[], getDate: (item: T) => string): T[] {
  return [...items].sort(
    (a, b) => new Date(getDate(b)).getTime() - new Date(getDate(a)).getTime(),
  )
}

export function useLibrary() {
  const [history, setHistory] = useState<TranscriptionRecord[]>([])
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([])
  const [loading, setLoading] = useState(true)
  const [copiedId, setCopiedId] = useState<number | null>(null)
  const [copiedNoteId, setCopiedNoteId] = useState<number | null>(null)
  const copyTimer = useRef<number | null>(null)

  const sortedHistory = useMemo(
    () => sortByNewest(history, (r) => r.createdAt),
    [history],
  )

  const sortedQuickNotes = useMemo(
    () => sortByNewest(quickNotes, (n) => n.createdAt),
    [quickNotes],
  )

  const pinnedQuickNotes = useMemo(
    () => sortedQuickNotes.filter((n) => n.pinned),
    [sortedQuickNotes],
  )

  const recentQuickNotes = useMemo(
    () => sortedQuickNotes.filter((n) => !n.pinned),
    [sortedQuickNotes],
  )

  useEffect(() => {
    let active = true
    void fetchHistory()
      .then((records) => {
        if (active) {
          setHistory(records)
          setLoading(false)
        }
      })
      .catch(() => {
        if (active) setLoading(false)
      })
    void fetchQuickNotes()
      .then((notes) => {
        if (active) setQuickNotes(notes)
      })
      .catch(() => {})
    return () => {
      active = false
      if (copyTimer.current) window.clearTimeout(copyTimer.current)
    }
  }, [])

  const copyText = useCallback((id: number, text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopiedId(id)
      if (copyTimer.current) window.clearTimeout(copyTimer.current)
      copyTimer.current = window.setTimeout(() => setCopiedId(null), 1200)
    })
  }, [])

  const copyNoteText = useCallback((id: number, text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedNoteId(id)
    setTimeout(() => setCopiedNoteId(null), 1200)
  }, [])

  const handlePinNote = useCallback(async (id: number, pinned: boolean) => {
    try {
      await pinQuickNote(id, pinned)
      const notes = await fetchQuickNotes()
      setQuickNotes(notes)
    } catch { /* ignore */ }
  }, [])

  const handleDeleteNote = useCallback(async (id: number) => {
    try {
      await deleteQuickNote(id)
      const notes = await fetchQuickNotes()
      setQuickNotes(notes)
    } catch { /* ignore */ }
  }, [])

  const openNoteEditor = useCallback((noteId?: number) => {
    const suffix = typeof noteId === 'number' ? `?id=${noteId}` : ''
    void fetch(`/quicknotes/open-editor${suffix}`, { method: 'POST' })
  }, [])

  const openNoteCapture = useCallback(() => {
    void fetch('/quicknotes/open-capture', { method: 'POST' })
  }, [])

  return {
    // Transcriptions
    history: sortedHistory,
    loading,
    copiedId,
    copyText,
    revealAudio: revealDashboardAudio,
    audioDownloadURL: dashboardAudioDownloadURL,

    // Quick Notes
    quickNotes: sortedQuickNotes,
    pinnedQuickNotes,
    recentQuickNotes,
    copiedNoteId,
    copyNoteText,
    handlePinNote,
    handleDeleteNote,
    openNoteEditor,
    openNoteCapture,
  }
}
