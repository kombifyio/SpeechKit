import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { vi } from 'vitest'

import { VoiceAgentPrompter } from '@/components/voiceagent-prompter'

const toggleThemeMock = vi.fn()

vi.mock('@/components/desktop-window-frame', () => ({
  DesktopWindowFrame: ({ children }: { children: ReactNode }) => (
    <div data-testid="desktop-window-frame">{children}</div>
  ),
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
  SpeechKitAuraOrb: ({ state }: { state: string }) => (
    <div data-testid="voice-agent-orb" data-state={state} />
  ),
}))

vi.mock('@/lib/desktop-theme', () => ({
  useDesktopTheme: () => ({ theme: 'dark', toggleTheme: toggleThemeMock }),
}))

type PrompterWindow = Window & {
  __prompter?: {
    setMode: (mode: string) => void
  }
}

describe('VoiceAgentPrompter', () => {
  beforeEach(() => {
    toggleThemeMock.mockReset()
  })

  it('keeps the voice agent orb surface separate from the assist output surface', async () => {
    render(<VoiceAgentPrompter />)

    await waitFor(() => expect((window as PrompterWindow).__prompter).toBeDefined())

    expect(screen.getByTestId('voice-agent-orb')).toHaveAttribute('data-state', 'inactive')
    expect(screen.getByText('Waiting for a voice session to begin.')).toBeInTheDocument()
    expect(screen.getByText('Inactive')).toBeInTheDocument()

    ;(window as PrompterWindow).__prompter?.setMode('assist')

    await waitFor(() => {
      expect(screen.queryByTestId('voice-agent-orb')).not.toBeInTheDocument()
    })

    expect(screen.getByText('One-shot output surface')).toBeInTheDocument()
    expect(screen.getByText('One-shot utilities, code words, and output-ready text.')).toBeInTheDocument()
  })
})
