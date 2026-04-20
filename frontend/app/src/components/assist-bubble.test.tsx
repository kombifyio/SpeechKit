import { act, render, screen, waitFor } from '@testing-library/react'

import { AssistBubble } from '@/components/assist-bubble'

type AssistBubbleWindow = Window & {
  __assistBubble?: {
    show: (text: string) => void
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
})
