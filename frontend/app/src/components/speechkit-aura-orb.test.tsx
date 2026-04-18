import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'

import { SpeechKitAuraOrb } from '@/components/speechkit-aura-orb'

vi.mock('@/components/agent-audio-visualizer-radial', () => ({
  AgentAudioVisualizerRadial: ({
    state,
    level,
    color,
  }: {
    state: string
    level: number
    color?: string
  }) => (
    <div
      data-testid="aura-radial"
      data-state={state}
      data-level={String(level)}
      data-color={color ?? ''}
    />
  ),
}))

describe('SpeechKitAuraOrb', () => {
  it('renders a stateful processing orb with its own visualizer mapping', () => {
    render(
      <SpeechKitAuraOrb
        state="processing"
        userLevel={0.16}
        assistantLevel={0.74}
      />,
    )

    const orb = screen.getByTestId('speechkit-aura-orb')
    expect(orb).toHaveAttribute('data-state', 'processing')
    expect(orb).toHaveAttribute(
      'aria-label',
      expect.stringContaining('Thinking. Formulating the next turn in real time.'),
    )
    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-state', 'thinking')
    expect(screen.getByText('Processing')).toBeInTheDocument()
    expect(screen.getByText('Thinking')).toBeInTheDocument()
    expect(screen.getAllByText('Drafting a response').length).toBeGreaterThan(0)
  })

  it('reflects the listening state on both speaker meters and the state badge', () => {
    render(
      <SpeechKitAuraOrb
        state="listening"
        userLevel={0.64}
        assistantLevel={0.14}
      />,
    )

    expect(screen.getByTestId('speechkit-aura-orb')).toHaveAttribute('data-state', 'listening')
    expect(screen.getByTestId('speechkit-aura-orb')).toHaveAttribute(
      'aria-label',
      expect.stringContaining('Listening. Ready for the user to continue the conversation.'),
    )
    expect(screen.getByText('Awaiting input')).toBeInTheDocument()
    expect(screen.getByText('Speaking')).toBeInTheDocument()
  })
})
