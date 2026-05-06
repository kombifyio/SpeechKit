import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { QuickCaptureApp } from '@/components/quickcapture-app'

const { updateQuickNoteMock, useAutoCloseMock } = vi.hoisted(() => ({
  updateQuickNoteMock: vi.fn<(id: number, text: string) => Promise<string>>(),
  useAutoCloseMock: vi.fn(),
}))

vi.mock('@/lib/speechkit', () => ({
  updateQuickNote: updateQuickNoteMock,
}))

vi.mock('@/hooks/use-auto-close', () => ({
  useAutoClose: useAutoCloseMock,
}))

describe('QuickCaptureApp', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    window.history.pushState({}, '', '/quickcapture.html?noteId=42')
    document.documentElement.dataset.theme = 'light'
    document.documentElement.classList.remove('dark')
    updateQuickNoteMock.mockReset()
    updateQuickNoteMock.mockResolvedValue('updated')
    useAutoCloseMock.mockReset()
    fetchMock = vi.fn(() => Promise.resolve({
      json: () => Promise.resolve({}),
    }))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
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

  it('keeps the capture open when clicking the transparent surface around the card', async () => {
    render(<QuickCaptureApp />)

    fireEvent.change(screen.getByRole('textbox'), {
      target: { value: 'Do not close from surface focus' },
    })
    fireEvent.mouseDown(screen.getByTestId('quick-capture-surface'))

    await Promise.resolve()
    expect(updateQuickNoteMock).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalledWith('/quicknotes/close-capture', { method: 'POST' })
  })

  it('keeps auto-close disabled while recording is still active', () => {
    render(<QuickCaptureApp />)

    expect(useAutoCloseMock).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: false,
        idleTimeoutMs: 60_000,
      }),
    )
  })

  it('applies the desktop dark theme inside the capture webview', async () => {
    render(<QuickCaptureApp />)

    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
