import { useEffect, useMemo, useState } from 'react'

import {
  fetchDashboardStats,
  fetchHistory,
  fetchQuickNotes,
  type DashboardStats,
  type QuickNote,
  type TranscriptionRecord,
} from '@/lib/speechkit'

function sortByNewest<T>(items: T[], getDate: (item: T) => string): T[] {
  return [...items].sort(
    (a, b) => new Date(getDate(b)).getTime() - new Date(getDate(a)).getTime(),
  )
}

export function useDashboardStats() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [updateInfo, setUpdateInfo] = useState<{
    latestVersion?: string
    updateURL?: string
  } | null>(null)
  const [history, setHistory] = useState<TranscriptionRecord[]>([])
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([])

  const latestTranscription = useMemo(() => {
    if (history.length === 0) return null
    return sortByNewest(history, (r) => r.createdAt)[0] ?? null
  }, [history])

  const featuredNotes = useMemo(() => {
    const sorted = sortByNewest(quickNotes, (n) => n.createdAt)
    const pinned = sorted.filter((n) => n.pinned)
    return pinned.length > 0 ? pinned.slice(0, 3) : sorted.slice(0, 3)
  }, [quickNotes])

  const hasHistory = history.length > 0

  useEffect(() => {
    let active = true
    void fetchDashboardStats()
      .then((next) => {
        if (active) setStats(next)
      })
      .catch(() => {
        if (active) setStats(null)
      })
    return () => { active = false }
  }, [])

  useEffect(() => {
    let active = true
    void fetch('/app/version')
      .then((r) => r.json())
      .then((data) => {
        if (active && data.latestVersion) {
          setUpdateInfo({
            latestVersion: data.latestVersion,
            updateURL: data.updateURL,
          })
        }
      })
      .catch(() => {})
    return () => { active = false }
  }, [])

  useEffect(() => {
    let active = true
    void fetchHistory()
      .then((records) => {
        if (active) setHistory(records)
      })
      .catch(() => {})
    void fetchQuickNotes()
      .then((notes) => {
        if (active) setQuickNotes(notes)
      })
      .catch(() => {})
    return () => { active = false }
  }, [])

  return {
    stats,
    updateInfo,
    latestTranscription,
    featuredNotes,
    hasHistory,
  }
}
