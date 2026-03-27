import { describe, expect, it } from 'vitest'

import type { SpeechKitOverlayState } from '@/lib/speechkit'
import { smoothOverlaySnapshot } from '@/components/overlay-state'

function makeSnapshot(
  partial: Partial<SpeechKitOverlayState> = {},
): SpeechKitOverlayState {
  return {
    state: 'idle',
    phase: 'idle',
    text: '',
    level: 0,
    visible: true,
    visualizer: 'pill',
    design: 'default',
    hotkey: 'win+alt',
    dictateHotkey: 'win+alt',
    agentHotkey: 'ctrl+shift+k',
    activeMode: 'dictate',
    position: 'top' as const,
    lastTranscription: '',
    quickNoteMode: false,
    selectedAudioDeviceId: '',
    activeProfiles: {},
    ...partial,
  }
}

describe('smoothOverlaySnapshot', () => {
  it('holds speaking briefly while the audio level decays during recording', () => {
    const previous = makeSnapshot({
      state: 'recording',
      phase: 'speaking',
      level: 0.72,
    })
    const next = makeSnapshot({
      state: 'recording',
      phase: 'listening',
      level: 0,
    })

    const smoothed = smoothOverlaySnapshot(previous, next, 90)

    expect(smoothed.phase).toBe('speaking')
    expect(smoothed.level).toBeGreaterThan(0.45)
  })

  it('lets speaking fall back to listening after a longer quiet period', () => {
    const previous = makeSnapshot({
      state: 'recording',
      phase: 'speaking',
      level: 0.2,
    })
    const next = makeSnapshot({
      state: 'recording',
      phase: 'listening',
      level: 0,
    })

    const smoothed = smoothOverlaySnapshot(previous, next, 420)

    expect(smoothed.phase).toBe('listening')
    expect(smoothed.level).toBeLessThan(0.05)
  })

  it('does not keep speaking alive after recording has stopped', () => {
    const previous = makeSnapshot({
      state: 'recording',
      phase: 'speaking',
      level: 0.65,
    })
    const next = makeSnapshot({
      state: 'processing',
      phase: 'thinking',
      level: 0,
    })

    const smoothed = smoothOverlaySnapshot(previous, next, 90)

    expect(smoothed.phase).toBe('thinking')
    expect(smoothed.state).toBe('processing')
  })
})
