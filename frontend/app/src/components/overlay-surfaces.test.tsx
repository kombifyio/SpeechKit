import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
    assistHotkey: 'ctrl+shift+j',
    voiceAgentHotkey: 'ctrl+shift+v',
    agentHotkey: 'ctrl+shift+j',
    activeMode: 'none',
    availableModes: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
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
    expect(screen.getByTestId('pill-anchor-stage').parentElement).toHaveClass('h-full', 'w-full')
    expect(shell).toHaveClass('w-[44px]')
    expect(shell).toHaveAttribute('data-overlay-surface', 'pill-anchor')
    expect(shell).toHaveAttribute('data-overlay-mode', 'pill')
    expect(screen.getByTestId('pill-anchor-status')).toHaveTextContent('No mode ready')
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

  it('shows the active mode badge on the pill while recording', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        state: 'recording',
        phase: 'listening',
        activeMode: 'assist',
      }),
    )

    render(<PillAnchorOverlay />)

    expect(await screen.findByTestId('pill-anchor-shell')).toHaveClass('pr-[26px]')
    expect(await screen.findByTitle('Active mode: Assist')).toBeInTheDocument()
  })

  it('renders only configured mode actions in the pill panel', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        assistHotkey: '',
        availableModes: {
          dictate: true,
          assist: false,
          voice_agent: true,
        },
        quickNoteMode: true,
        movable: true,
      }),
    )

    render(<PillActionsOverlay />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    expect(screen.getByTestId('pill-panel-stage')).toHaveClass('absolute', 'inset-0', 'flex', 'items-center', 'justify-center')
    expect(screen.getByTestId('pill-panel-shell')).toHaveAttribute('data-overlay-surface', 'pill-panel')
    expect(screen.getByTestId('pill-panel-center-shell')).toHaveAttribute('data-overlay-surface', 'pill-panel-center')
    expect(screen.getByRole('button', { name: /copy/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /note/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dictation' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Assist' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Voice Agent' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /microphone settings/i })).toBeInTheDocument()
    expect(screen.getByTestId('pill-panel-status')).toHaveTextContent('No mode ready')
    expect(screen.getByTestId('pill-panel-center-shell')).toHaveAttribute('data-overlay-draggable', 'true')
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

  it('allows toggling an active mode back to none from the pill panel', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ activeMode: 'assist' }))
    render(<PillActionsOverlay />)

    const assistButton = await screen.findByRole('button', { name: 'Assist' })
    fireEvent.click(assistButton)

    await waitFor(() => expect(setActiveModeMock).toHaveBeenCalledWith('none'))
  })

  it('renders the dot anchor as a compact circular surface', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ visualizer: 'circle' }))

    render(<DotAnchorOverlay />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    const shell = await screen.findByTestId('dot-anchor-shell')
    expect(screen.getByTestId('dot-anchor-stage')).toHaveClass('absolute', 'inset-0', 'flex', 'items-center', 'justify-center')
    expect(screen.getByTestId('dot-anchor-stage').parentElement).toHaveClass('h-full', 'w-full')
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

  it('lists configured tri-mode actions in the dot radial menu', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        visualizer: 'circle',
        voiceAgentHotkey: '',
        availableModes: {
          dictate: true,
          assist: true,
          voice_agent: false,
        },
      }),
    )

    render(<DotRadialOverlay />)

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalled())

    const shell = await screen.findByTestId('dot-radial-shell')
    expect(screen.getByTestId('dot-radial-stage')).toHaveClass('absolute', 'inset-0', 'flex', 'items-center', 'justify-center')
    expect(shell).toHaveAttribute('data-overlay-surface', 'dot-radial')
    expect(shell.querySelectorAll('path').length).toBeGreaterThan(0)
    expect(shell.querySelectorAll('foreignObject, foreignobject').length).toBeGreaterThan(0)

    const labels = within(screen.getByTestId('dot-radial-item-labels'))
    expect(labels.getByText('Copy')).toBeInTheDocument()
    expect(labels.getByText('Note')).toBeInTheDocument()
    expect(labels.getByText('Dictation')).toBeInTheDocument()
    expect(labels.getByText('Assist')).toBeInTheDocument()
    expect(labels.queryByText('Voice Agent')).not.toBeInTheDocument()
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

  it('shows the current tri-mode status label on compact overlays', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        activeMode: 'voice_agent',
        state: 'processing',
        phase: 'thinking',
        text: 'Summarizing selection',
      }),
    )

    render(<DotAnchorOverlay />)

    expect(await screen.findByTestId('dot-anchor-status')).toHaveTextContent('Voice Agent summarizing selection')
  })
})
