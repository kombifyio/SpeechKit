import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { QuickNoteApp } from '@/components/quicknote-app'

const {
  createQuickNoteMock,
  updateQuickNoteMock,
  quickNoteSummaryMock,
  quickNoteEmailMock,
  armQuickNoteRecordingMock,
} = vi.hoisted(() => ({
  createQuickNoteMock: vi.fn<(text: string) => Promise<{ id: number; message: string }>>(),
  updateQuickNoteMock: vi.fn<(id: number, text: string) => Promise<string>>(),
  quickNoteSummaryMock: vi.fn<(id: number) => Promise<string>>(),
  quickNoteEmailMock: vi.fn<(id: number) => Promise<string>>(),
  armQuickNoteRecordingMock: vi.fn<(noteId?: number) => Promise<string>>(),
}))

vi.mock('@/lib/speechkit', () => ({
  createQuickNote: createQuickNoteMock,
  updateQuickNote: updateQuickNoteMock,
  quickNoteSummary: quickNoteSummaryMock,
  quickNoteEmail: quickNoteEmailMock,
  armQuickNoteRecording: armQuickNoteRecordingMock,
}))

describe('QuickNoteApp', () => {
  beforeEach(() => {
    createQuickNoteMock.mockReset()
    updateQuickNoteMock.mockReset()
    quickNoteSummaryMock.mockReset()
    quickNoteEmailMock.mockReset()
    armQuickNoteRecordingMock.mockReset()
    createQuickNoteMock.mockResolvedValue({ id: 42, message: 'saved' })
    updateQuickNoteMock.mockResolvedValue('updated')
    quickNoteSummaryMock.mockResolvedValue('summary')
    quickNoteEmailMock.mockResolvedValue('email')
    armQuickNoteRecordingMock.mockResolvedValue('armed')
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      json: () => Promise.resolve([]),
    })))
  })

  it('passes the created note id into the recording arm call', async () => {
    render(<QuickNoteApp />)

    fireEvent.change(screen.getByPlaceholderText(/start typing your note/i), {
      target: { value: 'Draft note' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Record' }))

    await waitFor(() => expect(createQuickNoteMock).toHaveBeenCalledWith('Draft note'))
    await waitFor(() => expect(armQuickNoteRecordingMock).toHaveBeenCalledWith(42))
  })
})
