import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import type { SpeechKitOverlayState } from '@/lib/speechkit'
import {
  DotAnchorOverlay,
  DotRadialOverlay,
  PillActionsOverlay,
  PillAnchorOverlay,
} from '@/components/overlay-surfaces'

const { fetchOverlayStateMock, setActiveModeMock } = vi.hoisted(() => ({
  fetchOverlayStateMock: vi.fn<() => Promise<SpeechKitOverlayState>>(),
  setActiveModeMock: vi.fn<() => Promise<string>>(),
}))

vi.mock('@/lib/speechkit', async () => {
  const actual = await vi.importActual<typeof import('@/lib/speechkit')>('@/lib/speechkit')
  return {
    ...actual,
    fetchOverlayState: fetchOverlayStateMock,
    setActiveMode: setActiveModeMock,
  }
})

vi.mock('@/components/ui/mic-selector', () => ({
  MicSelector: ({ compact }: { compact?: boolean }) => (
    <button type="button" aria-label={compact ? 'Microphone compact' : 'Microphone'}>
      Mic
    </button>
  ),
}))

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
    lastTranscription: '',
    quickNoteMode: false,
    selectedAudioDeviceId: '',
    activeProfiles: {},
    ...partial,
  }
}

describe('overlay surfaces', () => {
  beforeEach(() => {
    fetchOverlayStateMock.mockReset()
    setActiveModeMock.mockReset()
    setActiveModeMock.mockResolvedValue('')
    vi.restoreAllMocks()
  })

  it('renders the pill anchor as a compact idle surface', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap())

    render(<PillAnchorOverlay />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    const shell = await screen.findByTestId('pill-anchor-shell')
    expect(screen.getByTestId('pill-anchor-stage')).toHaveClass('absolute', 'inset-0', 'flex', 'items-center', 'justify-center')
    expect(shell).toHaveAttribute('data-overlay-surface', 'pill-anchor')
    expect(shell).toHaveAttribute('data-overlay-mode', 'pill')
    expect(shell).not.toHaveTextContent(/copy|quick note|agent|mic/i)
  })

  it('opens the dedicated pill panel host on hover', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap())
    const fetchSpy = vi.spyOn(window, 'fetch').mockResolvedValue(new Response(null, { status: 200 }))

    render(<PillAnchorOverlay />)

    const shell = await screen.findByTestId('pill-anchor-shell')
    fireEvent.mouseEnter(shell)

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith('/overlay/pill-panel/show', { method: 'POST' }),
    )
  })

  it('renders the pill actions panel with the expanded action set', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ quickNoteMode: true }))

    render(<PillActionsOverlay />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    expect(screen.getByTestId('pill-panel-stage')).toHaveClass('absolute', 'inset-0', 'flex', 'items-center', 'justify-center')
    expect(screen.getByTestId('pill-panel-shell')).toHaveAttribute('data-overlay-surface', 'pill-panel')
    expect(screen.getByTestId('pill-panel-center-shell')).toHaveAttribute('data-overlay-surface', 'pill-panel-center')
    expect(screen.getByRole('button', { name: /copy/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /note/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /dictate/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /agent/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /microphone compact/i })).toBeInTheDocument()
    expect(screen.getByTestId('pill-panel-shell')).not.toHaveTextContent(/speechkit|ready|processing|actions|dictate|agent/i)
  })

  it('returns from the pill panel host on mouse leave', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap())
    const fetchSpy = vi.spyOn(window, 'fetch').mockResolvedValue(new Response(null, { status: 200 }))

    render(<PillActionsOverlay />)

    const shell = await screen.findByTestId('pill-panel-shell')
    fireEvent.mouseLeave(shell)

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith('/overlay/pill-panel/hide', { method: 'POST' }),
    )
  })

  it('renders the dot anchor as a compact circular surface', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ visualizer: 'circle' }))

    render(<DotAnchorOverlay />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    const shell = await screen.findByTestId('dot-anchor-shell')
    expect(screen.getByTestId('dot-anchor-stage')).toHaveClass('absolute', 'inset-0', 'flex', 'items-center', 'justify-center')
    expect(shell).toHaveAttribute('data-overlay-surface', 'dot-anchor')
    expect(shell).toHaveAttribute('data-overlay-mode', 'circle')
    expect(screen.getByTestId('dot-anchor-glyph')).toBeInTheDocument()
  })

  it('opens the dedicated radial host on context menu from the dot anchor', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ visualizer: 'circle' }))
    const fetchSpy = vi.spyOn(window, 'fetch').mockResolvedValue(new Response(null, { status: 200 }))

    render(<DotAnchorOverlay />)

    const shell = await screen.findByTestId('dot-anchor-shell')
    fireEvent.contextMenu(shell)

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith('/overlay/radial/show', { method: 'POST' }),
    )
  })

  it('renders the dot radial panel with the action ring', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ visualizer: 'circle' }))

    render(<DotRadialOverlay />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    const shell = await screen.findByTestId('dot-radial-shell')
    expect(screen.getByTestId('dot-radial-stage')).toHaveClass('absolute', 'inset-0', 'flex', 'items-center', 'justify-center')
    expect(shell).toHaveAttribute('data-overlay-surface', 'dot-radial')
    expect(screen.queryByText(/dot menu/i)).not.toBeInTheDocument()
    expect(shell.querySelectorAll('path').length).toBeGreaterThan(0)
    expect(shell.querySelectorAll('foreignObject, foreignobject').length).toBeGreaterThan(0)
  })

  it('returns from the radial host on mouse leave', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ visualizer: 'circle' }))
    const fetchSpy = vi.spyOn(window, 'fetch').mockResolvedValue(new Response(null, { status: 200 }))

    render(<DotRadialOverlay />)

    const shell = await screen.findByTestId('dot-radial-shell')
    fireEvent.mouseLeave(shell)

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith('/overlay/radial/hide', { method: 'POST' }),
    )
  })

  it('toggles the active mode from the pill panel', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ activeMode: 'dictate' }))
    render(<PillActionsOverlay />)

    const agentButton = await screen.findByRole('button', { name: /agent/i })
    fireEvent.click(agentButton)

    await waitFor(() => expect(setActiveModeMock).toHaveBeenCalledWith('agent'))
  })
})
