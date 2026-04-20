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
    assistHotkey: 'ctrl+win',
    voiceAgentHotkey: 'ctrl+shift',
    dictateHotkeyBehavior: 'push_to_talk',
    assistHotkeyBehavior: 'push_to_talk',
    voiceAgentHotkeyBehavior: 'toggle',
    modeEnabled: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
    agentHotkey: 'ctrl+win',
    activeMode: 'none',
    availableModes: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
    position: 'top' as const,
    movable: false,
    positionFreeX: 0,
    positionFreeY: 0,
    lastTranscription: '',
    quickNoteMode: false,
    selectedAudioDeviceId: '',
    activeProfiles: {},
    ...partial,
    assistOverlayMode: partial.assistOverlayMode ?? 'small_feedback',
    voiceAgentOverlayMode: partial.voiceAgentOverlayMode ?? 'small_feedback',
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
