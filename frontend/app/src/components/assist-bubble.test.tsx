import { act, render, screen, waitFor } from '@testing-library/react'

import { AssistBubble } from '@/components/assist-bubble'

type AssistBubbleWindow = Window & {
  __assistBubble?: {
    show: (text: string) => void
    showPanel: (payload: string | { text: string; inputText?: string }) => void
    hide: () => void
  }
}

describe('AssistBubble', () => {
  it('renders without overlay drop shadow chrome', async () => {
    render(<AssistBubble />)

    await waitFor(() => expect((window as AssistBubbleWindow).__assistBubble).toBeDefined())

    act(() => {
      ;(window as AssistBubbleWindow).__assistBubble?.show('No provider configured')
    })

    const bubble = await screen.findByTitle('Click to dismiss')
    expect(bubble).toHaveStyle({ boxShadow: 'none' })
  })

  it('renders long assist results as a persistent panel', async () => {
    render(<AssistBubble />)

    await waitFor(() => expect((window as AssistBubbleWindow).__assistBubble).toBeDefined())

    act(() => {
      ;(window as AssistBubbleWindow).__assistBubble?.showPanel({
        text: 'Summary\n\nDecision: ship the assist panel.\nNext: run the live smoke.',
        inputText: 'summarize selection',
      })
    })

    expect(await screen.findByRole('dialog', { name: 'Assist result panel' })).toBeInTheDocument()
    expect(screen.getByText('Assist result')).toBeInTheDocument()
    expect(screen.getByText('summarize selection')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument()
  })
})
