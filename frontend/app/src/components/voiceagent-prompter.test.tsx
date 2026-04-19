import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { vi } from 'vitest'

import { VoiceAgentPrompter } from '@/components/voiceagent-prompter'

const toggleThemeMock = vi.fn()
const {
  eventsEmitMock,
  windowHideMock,
  windowPositionMock,
  windowSetPositionMock,
  windowSetSizeMock,
  windowSizeMock,
} = vi.hoisted(() => ({
  eventsEmitMock: vi.fn<() => Promise<boolean>>(),
  windowHideMock: vi.fn<() => Promise<void>>(),
  windowPositionMock: vi.fn<() => Promise<{ x: number; y: number }>>(),
  windowSetPositionMock: vi.fn<() => Promise<void>>(),
  windowSetSizeMock: vi.fn<() => Promise<void>>(),
  windowSizeMock: vi.fn<() => Promise<{ width: number; height: number }>>(),
}))

vi.mock('@/components/desktop-window-frame', () => ({
  DesktopWindowFrame: ({
    children,
    onClose,
    actions,
    density,
    showThemeToggle,
  }: {
    children: ReactNode
    onClose?: () => void | Promise<void>
    actions?: ReactNode
    density?: string
    showThemeToggle?: boolean
  }) => (
    <div
      data-testid="desktop-window-frame"
      data-density={density ?? ''}
      data-show-theme-toggle={String(showThemeToggle)}
    >
      <div data-testid="window-actions">{actions}</div>
      <button type="button" aria-label="Close window" onClick={() => void onClose?.()}>
        Close
      </button>
      {children}
    </div>
  ),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    Emit: eventsEmitMock,
  },
  Window: {
    Hide: windowHideMock,
    Position: windowPositionMock,
    SetPosition: windowSetPositionMock,
    SetSize: windowSetSizeMock,
    Size: windowSizeMock,
  },
}))

vi.mock('@/components/agent-audio-visualizer-radial', () => ({
  AgentAudioVisualizerRadial: ({
    state,
    level,
  }: {
    state: string
    level: number
  }) => <div data-testid="turn-visualizer" data-state={state} data-level={String(level)} />,
}))

vi.mock('@/components/speechkit-aura-orb', () => ({
  SpeechKitAuraOrb: ({ state, className }: { state: string; className?: string }) => (
    <div data-testid="voice-agent-orb" data-state={state} className={className} />
  ),
}))

vi.mock('@/lib/desktop-theme', () => ({
  useDesktopTheme: () => ({ theme: 'dark', toggleTheme: toggleThemeMock }),
}))

type PrompterWindow = Window & {
  __prompter?: {
    addMessage: (message: { role: 'user' | 'assistant' | 'system'; text: string; done: boolean }) => void
    setMode: (mode: string) => void
    setActivity: (role: 'user' | 'assistant', level: number) => void
    updateState: (state: string) => void
  }
}

describe('VoiceAgentPrompter', () => {
  beforeEach(() => {
    toggleThemeMock.mockReset()
    eventsEmitMock.mockReset()
    windowHideMock.mockReset()
    windowPositionMock.mockReset()
    windowSetPositionMock.mockReset()
    windowSetSizeMock.mockReset()
    windowSizeMock.mockReset()
    eventsEmitMock.mockResolvedValue(false)
    windowHideMock.mockResolvedValue(undefined)
    windowPositionMock.mockResolvedValue({ x: 100, y: 100 })
    windowSetPositionMock.mockResolvedValue(undefined)
    windowSetSizeMock.mockResolvedValue(undefined)
    windowSizeMock.mockResolvedValue({ width: 390, height: 500 })
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      devices: [
        { deviceId: 'speaker-1', label: 'Desk speakers', isDefault: true },
      ],
      selectedDeviceId: 'speaker-1',
    }), { status: 200 })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps the voice agent orb surface separate from the assist output surface', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    expect(await screen.findByTestId('voice-agent-orb')).toHaveAttribute('data-state', 'inactive')
    expect(screen.getByText('Waiting for a voice session to begin.')).toBeInTheDocument()
    expect(screen.getAllByText('Inactive').length).toBeGreaterThan(0)

    act(() => {
      ;(window as PrompterWindow).__prompter?.setMode('assist')
    })

    await waitFor(() => {
      expect(screen.queryByTestId('voice-agent-orb')).not.toBeInTheDocument()
    })

    expect(screen.getByText('One-shot output surface')).toBeInTheDocument()
    expect(screen.getByText('One-shot utilities, code words, and output-ready text.')).toBeInTheDocument()
  })

  it('uses compact chrome and a small lazy voice orb for the live agent overlay', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    expect(screen.getByTestId('desktop-window-frame')).toHaveAttribute('data-density', 'compact')
    expect(screen.getByTestId('desktop-window-frame')).toHaveAttribute('data-show-theme-toggle', 'false')
    expect(await screen.findByTestId('voice-agent-orb')).toHaveClass('w-[76px]')
    expect(screen.getByRole('button', { name: /hide transcript/i })).toHaveClass('h-8')
    expect(screen.getByRole('button', { name: /hide transcript/i })).toHaveClass('w-8')
  })

  it('uses panel fold icons for transcript visibility instead of a minimise icon', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    const hideButton = screen.getByRole('button', { name: /hide transcript/i })
    expect(hideButton.querySelector('.lucide-panel-bottom-close')).toBeInTheDocument()
    expect(hideButton.querySelector('.lucide-minus')).not.toBeInTheDocument()

    fireEvent.click(hideButton)

    const showButton = screen.getByRole('button', { name: /show transcript/i })
    expect(showButton.querySelector('.lucide-panel-bottom-open')).toBeInTheDocument()
    expect(showButton.querySelector('.lucide-minus')).not.toBeInTheDocument()
  })

  it('shrinks and expands the window from the top-right edge when the transcript is toggled', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    fireEvent.click(screen.getByRole('button', { name: /hide transcript/i }))

    await waitFor(() => {
      expect(windowSetSizeMock).toHaveBeenCalledWith(340, 132)
    })
    expect(windowSetPositionMock).toHaveBeenCalledWith(150, 100)
    expect(screen.getByTestId('voice-agent-collapsed-surface')).toBeInTheDocument()
    expect(screen.queryByText('Transcript hidden')).not.toBeInTheDocument()
    const showButton = screen.getByRole('button', { name: /show transcript/i })
    expect(showButton).toBeInTheDocument()

    windowSizeMock.mockResolvedValueOnce({ width: 340, height: 132 })
    windowPositionMock.mockResolvedValueOnce({ x: 150, y: 100 })

    fireEvent.click(showButton)

    await waitFor(() => {
      expect(windowSetSizeMock).toHaveBeenCalledWith(390, 500)
    })
    expect(windowSetPositionMock).toHaveBeenCalledWith(100, 100)
  })

  it('renders live mic feedback without adding transcript-side visualizers', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    act(() => {
      ;(window as PrompterWindow).__prompter?.updateState('listening')
      ;(window as PrompterWindow).__prompter?.setActivity('user', 0.42)
    })

    expect(screen.getByTestId('voice-agent-live-user')).toHaveAttribute('data-active', 'true')
    expect(screen.getByTestId('voice-agent-live-user')).toHaveTextContent('Hearing you')
    expect(screen.queryAllByTestId('turn-visualizer')).toHaveLength(0)
  })

  it('renders voice agent live turns as compact multiline rows without inner cards', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    act(() => {
      ;(window as PrompterWindow).__prompter?.addMessage({
        role: 'user',
        text: 'Das ist eine längere Frage,\ndie in mehreren Zeilen angezeigt werden muss.',
        done: true,
      })
      ;(window as PrompterWindow).__prompter?.addMessage({
        role: 'assistant',
        text: 'Antwort mit genug Text, damit das schmale Panel sauber umbrechen kann.',
        done: false,
      })
    })

    expect(screen.getByTestId('voice-agent-surface')).toBeInTheDocument()
    expect(screen.getByTestId('voice-agent-live-user')).toHaveAttribute('data-cardless', 'true')
    expect(screen.getByTestId('voice-agent-live-assistant')).toHaveAttribute('data-cardless', 'true')
    expect(screen.queryAllByTestId('turn-visualizer')).toHaveLength(0)
    expect(screen.getByText(/mehreren Zeilen/)).toHaveClass('whitespace-pre-wrap')
    expect(screen.getByLabelText('Speaker output')).toBeInTheDocument()
  })

  it('replaces stale live turn text after the other speaker responds', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    act(() => {
      ;(window as PrompterWindow).__prompter?.addMessage({
        role: 'user',
        text: 'Erste Frage ohne finales Done Signal',
        done: false,
      })
      ;(window as PrompterWindow).__prompter?.addMessage({
        role: 'assistant',
        text: 'Erste Antwort ist fertig.',
        done: true,
      })
      ;(window as PrompterWindow).__prompter?.addMessage({
        role: 'user',
        text: 'Zweite Frage startet neu',
        done: false,
      })
    })

    const userTurn = screen.getByTestId('voice-agent-live-user')
    expect(userTurn).toHaveTextContent('Zweite Frage startet neu')
    expect(userTurn).not.toHaveTextContent('Erste Frage ohne finales Done Signal')
    expect(screen.getByTestId('voice-agent-live-assistant')).toHaveTextContent('Erste Antwort ist fertig.')
  })

  it('emits the backend close event and hides the window when the close button is clicked', async () => {
    render(<VoiceAgentPrompter />)

    fireEvent.click(screen.getByRole('button', { name: /close window/i }))

    await waitFor(() => {
      expect(eventsEmitMock).toHaveBeenCalledWith('voiceagent:close')
      expect(windowHideMock).toHaveBeenCalledTimes(1)
    })
  })
})
