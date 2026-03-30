import { useEffect, useRef, useState } from 'react'

import { defaultOverlayState, fetchOverlayState, type SpeechKitOverlayState } from '@/lib/speechkit'
import { smoothOverlaySnapshot } from '@/components/overlay-state'

export function useOverlaySnapshot() {
  const [snapshot, setSnapshot] = useState<SpeechKitOverlayState>(defaultOverlayState)
  const lastSyncRef = useRef<number>(Date.now())

  useEffect(() => {
    let mounted = true
    let inFlight = false

    const sync = async () => {
      if (inFlight) return
      inFlight = true
      try {
        const next = await fetchOverlayState()
        if (mounted) {
          const now = Date.now()
          const elapsedMs = now - lastSyncRef.current
          lastSyncRef.current = now
          setSnapshot((prev) => smoothOverlaySnapshot(prev, next, elapsedMs))
        }
      } catch {
        // Ignore transient polling failures.
      } finally {
        inFlight = false
      }
    }

    void sync()
    const interval = window.setInterval(sync, 90)
    return () => {
      mounted = false
      window.clearInterval(interval)
    }
  }, [])

  return snapshot
}
