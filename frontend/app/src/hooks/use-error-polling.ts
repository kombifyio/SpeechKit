import { useCallback, useEffect, useRef } from 'react'

import { fetchLogs } from '@/lib/speechkit'

export function useErrorPolling(
  onError: (message: string) => void,
  intervalMs = 3000,
) {
  const lastCountRef = useRef(0)
  const onErrorRef = useRef(onError)
  useEffect(() => { onErrorRef.current = onError }, [onError])

  const poll = useCallback(async () => {
    try {
      const logs = await fetchLogs()
      if (logs.length > lastCountRef.current) {
        const newLogs = logs.slice(lastCountRef.current)
        for (const log of newLogs) {
          if (log.type === 'error') {
            onErrorRef.current(log.message)
          }
        }
        lastCountRef.current = logs.length
      }
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    const interval = setInterval(() => void poll(), intervalMs)
    return () => clearInterval(interval)
  }, [poll, intervalMs])
}
