import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { QuickCaptureApp } from '@/components/quickcapture-app'

const { updateQuickNoteMock } = vi.hoisted(() => ({
  updateQuickNoteMock: vi.fn<(id: number, text: string) => Promise<string>>(),
}))

vi.mock('@/lib/speechkit', () => ({
  updateQuickNote: updateQuickNoteMock,
}))

describe('QuickCaptureApp', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    window.history.pushState({}, '', '/quickcapture.html?noteId=42')
    updateQuickNoteMock.mockReset()
    updateQuickNoteMock.mockResolvedValue('updated')
    fetchMock = vi.fn(() => Promise.resolve({
      json: () => Promise.resolve({}),
    }))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders quick capture as a single card without desktop controls', () => {
    render(<QuickCaptureApp />)

    expect(screen.getByTestId('quick-capture-card')).toBeInTheDocument()
    expect(screen.getByText('Note 42')).toBeInTheDocument()
    expect(screen.getByText('Auto-stop on silence')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /save & close/i })).toBeInTheDocument()
    expect(screen.queryByText('Create Quick Note')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /minimise window/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /close window/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /switch to light mode/i })).not.toBeInTheDocument()
  })

  it('saves and closes from the compact save button', async () => {
    render(<QuickCaptureApp />)

    fireEvent.change(screen.getByRole('textbox'), {
      target: { value: 'Meeting note' },
    })
    fireEvent.click(screen.getByRole('button', { name: /save & close/i }))

    await waitFor(() => expect(updateQuickNoteMock).toHaveBeenCalledWith(42, 'Meeting note'))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/quicknotes/close-capture', { method: 'POST' }))
  })

  it('closes when clicking the transparent surface outside the card', async () => {
    render(<QuickCaptureApp />)

    fireEvent.change(screen.getByRole('textbox'), {
      target: { value: 'Close from outside' },
    })
    fireEvent.mouseDown(screen.getByTestId('quick-capture-surface'))

    await waitFor(() => expect(updateQuickNoteMock).toHaveBeenCalledWith(42, 'Close from outside'))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/quicknotes/close-capture', { method: 'POST' }))
  })
})
