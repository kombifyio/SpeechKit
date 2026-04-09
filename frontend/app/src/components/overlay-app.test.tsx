import { render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { OverlayApp } from '@/components/overlay-app'
import type { SpeechKitOverlayState } from '@/lib/speechkit'

const { fetchOverlayStateMock } = vi.hoisted(() => ({
  fetchOverlayStateMock: vi.fn<() => Promise<SpeechKitOverlayState>>(),
}))

vi.mock('@/lib/speechkit', async () => {
  const actual = await vi.importActual<typeof import('@/lib/speechkit')>('@/lib/speechkit')
  return {
    ...actual,
    fetchOverlayState: fetchOverlayStateMock,
  }
})

function snap(partial: Partial<SpeechKitOverlayState> = {}): SpeechKitOverlayState {
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
    position: 'top',
    movable: false,
    positionFreeX: 0,
    positionFreeY: 0,
    lastTranscription: '',
    quickNoteMode: false,
    selectedAudioDeviceId: '',
    activeProfiles: {},
    ...partial,
  }
}

describe('OverlayApp', () => {
  beforeEach(() => {
    fetchOverlayStateMock.mockReset()
  })

  it('renders the default overlay as the compact pill anchor', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap())

    render(<OverlayApp />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    const shell = await screen.findByTestId('pill-anchor-shell')
    expect(shell).toHaveAttribute('data-overlay-surface', 'pill-anchor')
    expect(shell).toHaveAttribute('data-overlay-mode', 'pill')
    expect(shell).toHaveClass('rounded-full')
  })

  it('keeps the kombify mark inside the pill anchor when configured', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ design: 'kombify' }))

    render(<OverlayApp />)

    const icon = await screen.findByTestId('pill-anchor-kombify-mark')
    expect(icon).toHaveAttribute('src', '/idle-kombify.png')
  })

  it('renders the compact circular anchor in dot mode', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ visualizer: 'circle' }))

    render(<OverlayApp />)

    const shell = await screen.findByTestId('dot-anchor-shell')
    expect(shell).toHaveAttribute('data-overlay-surface', 'dot-anchor')
    expect(shell).toHaveAttribute('data-overlay-mode', 'circle')
  })
})
