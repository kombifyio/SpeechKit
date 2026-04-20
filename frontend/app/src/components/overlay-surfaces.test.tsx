import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { vi } from 'vitest'

import type { SpeechKitOverlayState } from '@/lib/speechkit'
import {
  DotAnchorOverlay,
  DotRadialOverlay,
  PillActionsOverlay,
  PillAnchorOverlay,
} from '@/components/overlay-surfaces'

  const {
  fetchOverlayStateMock,
  setActiveModeMock,
  setModeEnabledMock,
  fetchAudioDevicesMock,
  setAudioDeviceMock,
} = vi.hoisted(() => ({
  fetchOverlayStateMock: vi.fn<() => Promise<SpeechKitOverlayState>>(),
  setActiveModeMock: vi.fn<() => Promise<string>>(),
  setModeEnabledMock: vi.fn<(mode: string, enabled: boolean) => Promise<string>>(),
  fetchAudioDevicesMock: vi.fn(),
  setAudioDeviceMock: vi.fn<(deviceId: string) => Promise<string>>(),
}))

vi.mock('@/lib/speechkit', async () => {
  const actual = await vi.importActual<typeof import('@/lib/speechkit')>('@/lib/speechkit')
  return {
    ...actual,
    fetchOverlayState: fetchOverlayStateMock,
    fetchAudioDevices: fetchAudioDevicesMock,
    setAudioDevice: setAudioDeviceMock,
    setActiveMode: setActiveModeMock,
    setModeEnabled: setModeEnabledMock,
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
    assistOverlayMode: 'small_feedback',
    voiceAgentOverlayMode: 'small_feedback',
    hotkey: 'win+alt',
    dictateHotkey: 'win+alt',
    assistHotkey: 'ctrl+shift+j',
    voiceAgentHotkey: 'ctrl+shift+v',
    dictateHotkeyBehavior: 'push_to_talk',
    assistHotkeyBehavior: 'push_to_talk',
    voiceAgentHotkeyBehavior: 'toggle',
    agentHotkey: 'ctrl+shift+j',
    activeMode: 'none',
    modeEnabled: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
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
    setModeEnabledMock.mockReset()
    fetchAudioDevicesMock.mockReset()
    setAudioDeviceMock.mockReset()
    setActiveModeMock.mockResolvedValue('')
    setModeEnabledMock.mockResolvedValue('')
    fetchAudioDevicesMock.mockResolvedValue({
      devices: [
        { deviceId: 'mic-1', label: 'Desk Mic' },
        { deviceId: 'mic-2', label: 'Headset Mic' },
      ],
      selectedDeviceId: 'mic-1',
    })
    setAudioDeviceMock.mockResolvedValue('Selected')
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

  it('renders small assist feedback as an expanded pill anchor', async () => {
    const feedbackText = 'Schreibe eine kurze Antwort fuer den Kunden mit allen relevanten Details.'
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        state: 'processing',
        phase: 'thinking',
        activeMode: 'assist',
        text: feedbackText,
        assistOverlayMode: 'small_feedback',
      }),
    )

    render(<PillAnchorOverlay />)

    const shell = await screen.findByTestId('pill-anchor-shell')
    const feedback = screen.getByTestId('pill-anchor-compact-feedback')

    expect(shell).toHaveAttribute('data-compact-feedback', 'true')
    expect(shell).toHaveClass('min-w-[260px]')
    expect(feedback).toHaveTextContent(feedbackText)
    expect(feedback).toHaveClass('whitespace-normal', 'break-words')
    expect(feedback.className).not.toContain('truncate')
  })

  it('renders small voice agent feedback inside the expanded pill controls', async () => {
    const feedbackText = 'Ich habe den Dialog verstanden und fasse die naechste Aktion sichtbar zusammen.'
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        state: 'processing',
        phase: 'thinking',
        activeMode: 'voice_agent',
        text: feedbackText,
        voiceAgentOverlayMode: 'small_feedback',
      }),
    )

    render(<PillActionsOverlay />)

    const panelShell = await screen.findByTestId('pill-panel-shell')
    const centerShell = screen.getByTestId('pill-panel-center-shell')
    const feedback = screen.getByTestId('pill-panel-center-compact-feedback')

    expect(feedback).toHaveTextContent(feedbackText)
    expect(feedback).toHaveClass('whitespace-normal', 'break-words')
    expect(panelShell).toContainElement(feedback)
    expect(centerShell).toHaveAttribute('data-compact-feedback', 'true')
  })

  it('keeps big productivity feedback off the compact pill surface', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        state: 'processing',
        phase: 'thinking',
        activeMode: 'assist',
        text: 'Long answer belongs in the productivity panel.',
        assistOverlayMode: 'big_productivity',
      }),
    )

    render(<PillAnchorOverlay />)

    const shell = await screen.findByTestId('pill-anchor-shell')
    expect(shell).toHaveAttribute('data-compact-feedback', 'false')
    expect(screen.queryByTestId('pill-anchor-compact-feedback')).not.toBeInTheDocument()
    expect(screen.queryByTestId('pill-anchor-compact-feedback-panel')).not.toBeInTheDocument()
  })

  it('renders quick controls on the left and three independent module toggles on the right in the pill panel', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        activeMode: 'voice_agent',
        modeEnabled: {
          dictate: true,
          assist: false,
          voice_agent: true,
        },
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
    const leftControls = screen.getByTestId('pill-panel-left-controls')
    const rightControls = screen.getByTestId('pill-panel-mode-controls')

    expect(screen.getByTestId('pill-panel-shell')).toHaveClass('grid')
    expect(leftControls).toHaveClass('w-[76px]')
    expect(rightControls).toHaveClass('w-[76px]')
    expect(within(leftControls).getByRole('combobox', { name: /microphone quick select/i })).toBeInTheDocument()
    expect(within(leftControls).getByRole('button', { name: /copy/i })).toBeInTheDocument()
    expect(within(leftControls).getByRole('button', { name: /note/i })).toBeInTheDocument()
    expect(within(leftControls).getByTestId('mic-selector-icon')).toHaveAttribute('data-icon', 'mic')
    expect(within(rightControls).getByRole('button', { name: 'Dictation' })).toBeInTheDocument()
    expect(within(rightControls).getByRole('button', { name: 'Assist' })).toBeInTheDocument()
    expect(within(rightControls).getByRole('button', { name: 'Voice Agent' })).toBeInTheDocument()
    expect(within(rightControls).getByTestId('mode-icon-dictation')).toHaveAttribute('data-icon', 'audio-lines')
    expect(within(rightControls).getAllByRole('button')).toHaveLength(3)
    expect(screen.queryByRole('button', { name: /microphone settings/i })).not.toBeInTheDocument()
    expect(screen.getByTestId('pill-panel-status')).toHaveTextContent('No mode ready')
    expect(screen.getByTestId('pill-panel-center-shell')).toHaveAttribute('data-overlay-draggable', 'true')
    expect(within(rightControls).getByRole('button', { name: 'Assist' })).toHaveAttribute('aria-pressed', 'false')
    expect(within(rightControls).getByRole('button', { name: 'Voice Agent' })).toHaveAttribute('data-runtime-active', 'true')
    expect(within(rightControls).getByTestId('mode-toggle-assist-slashed')).toBeInTheDocument()
  })

  it('keeps the hovered pill panel shell as a rounded pill without rectangular shadow chrome', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap())

    render(<PillActionsOverlay />)

    const shell = await screen.findByTestId('pill-panel-shell')

    expect(shell).toHaveClass('rounded-full')
    expect(shell).toHaveClass('bg-neutral-950/84')
    expect(shell).toHaveClass('shadow-none')
    expect(shell.className).not.toContain('backdrop-blur')
  })

  it('switches the microphone from the pill panel quick selector', async () => {
    fetchOverlayStateMock.mockResolvedValue(snap({ selectedAudioDeviceId: 'mic-1' }))

    render(<PillActionsOverlay />)

    const select = await screen.findByRole('combobox', { name: /microphone quick select/i })
    await waitFor(() => expect(select).not.toBeDisabled())
    fireEvent.change(select, { target: { value: 'mic-2' } })

    await waitFor(() => expect(setAudioDeviceMock).toHaveBeenCalledWith('mic-2'))
    expect(select).toHaveValue('mic-2')
  })

  it('moves the movable pill panel through overlay move routes instead of native drag regions', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        movable: true,
        positionFreeX: 640,
        positionFreeY: 360,
      }),
    )
    const fetchSpy = vi.spyOn(window, 'fetch').mockResolvedValue(new Response(null, { status: 200 }))

    render(<PillActionsOverlay />)

    const stage = await screen.findByTestId('pill-panel-stage')
    const centerShell = screen.getByTestId('pill-panel-center-shell')

    expect(centerShell.getAttribute('style') ?? '').not.toContain('drag')

    fireEvent.pointerDown(centerShell, {
      pointerId: 7,
      button: 0,
      screenX: 640,
      screenY: 360,
    })
    fireEvent.mouseLeave(stage)
    fireEvent.pointerMove(centerShell, {
      pointerId: 7,
      buttons: 1,
      screenX: 684,
      screenY: 392,
    })
    fireEvent.pointerUp(centerShell, {
      pointerId: 7,
      button: 0,
      screenX: 684,
      screenY: 392,
    })

    const moveCall = fetchSpy.mock.calls.find(([url]) => url === '/overlay/free-center')
    expect(moveCall).toBeTruthy()
    expect(moveCall?.[1]).toEqual(
      expect.objectContaining({
        method: 'POST',
      }),
    )
    const moveBody = moveCall?.[1]?.body
    expect(moveBody).toBeInstanceOf(URLSearchParams)
    const moveParams = moveBody as URLSearchParams
    expect(moveParams.get('center_x')).toBe('684')
    expect(moveParams.get('center_y')).toBe('392')

    const saveCall = fetchSpy.mock.calls.find(([url]) => url === '/overlay/free-center/save')
    expect(saveCall).toBeTruthy()
    const saveBody = saveCall?.[1]?.body
    expect(saveBody).toBeInstanceOf(URLSearchParams)
    const saveParams = saveBody as URLSearchParams
    expect(saveParams.get('center_x')).toBe('684')
    expect(saveParams.get('center_y')).toBe('392')

    expect(
      fetchSpy.mock.calls.some(([url]) => url === '/overlay/pill-panel/hide'),
    ).toBe(false)
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

  it('toggles a single module without changing the other module buttons', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        modeEnabled: {
          dictate: true,
          assist: false,
          voice_agent: true,
        },
        availableModes: {
          dictate: true,
          assist: false,
          voice_agent: true,
        },
      }),
    )
    render(<PillActionsOverlay />)

    const assistButton = await screen.findByRole('button', { name: 'Assist' })
    fireEvent.click(assistButton)

    await waitFor(() => expect(setModeEnabledMock).toHaveBeenCalledWith('assist', true))
    expect(setActiveModeMock).not.toHaveBeenCalled()
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

  it('lists all three module toggles in the dot radial menu and keeps disabled ones visible', async () => {
    fetchOverlayStateMock.mockResolvedValue(
      snap({
        visualizer: 'circle',
        modeEnabled: {
          dictate: true,
          assist: true,
          voice_agent: false,
        },
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
    expect(labels.getByText('Voice Agent')).toBeInTheDocument()
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
