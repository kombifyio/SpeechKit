import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'

import { SpeechKitAuraOrb } from '@/components/speechkit-aura-orb'

vi.mock('@/components/agent-audio-visualizer-radial', () => ({
  AgentAudioVisualizerRadial: ({
    state,
    level,
    color,
    size,
    radius,
    barCount,
  }: {
    state: string
    level: number
    color?: string
    size?: string
    radius?: number
    barCount?: number
  }) => (
    <div
      data-testid="aura-radial"
      data-state={state}
      data-level={String(level)}
      data-color={color ?? ''}
      data-size={size ?? ''}
      data-radius={String(radius ?? '')}
      data-bar-count={String(barCount ?? '')}
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
    expect(orb).toHaveAttribute('data-variant', 'ambient')
    expect(orb).toHaveAttribute(
      'aria-label',
      expect.stringContaining('Thinking. Formulating the next turn in real time.'),
    )
    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-state', 'thinking')
    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-size', 'sm')
    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-radius', '18')
    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-bar-count', '16')
    expect(screen.queryByText('Processing')).not.toBeInTheDocument()
    expect(screen.queryByText('Voice Agent')).not.toBeInTheDocument()
  })

  it('keeps listening visually calm instead of reacting to user mic level', () => {
    render(
      <SpeechKitAuraOrb
        state="listening"
        userLevel={0.64}
        assistantLevel={0.14}
      />,
    )

    expect(screen.getByTestId('speechkit-aura-orb')).toHaveAttribute('data-state', 'listening')
    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-level', '0.12')
    expect(screen.getByTestId('speechkit-aura-orb')).toHaveAttribute(
      'aria-label',
      expect.stringContaining('Listening. Ready for the user to continue the conversation.'),
    )
    expect(screen.queryByText('Awaiting input')).not.toBeInTheDocument()
    expect(screen.queryByText('Speaking')).not.toBeInTheDocument()
  })

  it('reacts to assistant audio while speaking', () => {
    render(
      <SpeechKitAuraOrb
        state="speaking"
        userLevel={0.2}
        assistantLevel={0.71}
      />,
    )

    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-state', 'speaking')
    expect(screen.getByTestId('aura-radial')).toHaveAttribute('data-level', '0.71')
  })
})
