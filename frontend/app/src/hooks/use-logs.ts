import { useCallback, useEffect, useRef, useState } from 'react'

import { fetchLogs, type LogEntry } from '@/lib/speechkit'

export function useLogs(pollMs = 2000) {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const containerRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    try {
      return await fetchLogs()
    } catch {
      return null
    }
  }, [])

  useEffect(() => {
    let active = true
    const sync = async () => {
      const entries = await load()
      if (!active) return
      if (entries) setLogs(entries)
      setLoading(false)
    }

    void sync()
    const timer = window.setInterval(() => void sync(), pollMs)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [load, pollMs])

  // Auto-scroll
  useEffect(() => {
    const el = containerRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [logs])

  return { logs, loading, containerRef }
}
