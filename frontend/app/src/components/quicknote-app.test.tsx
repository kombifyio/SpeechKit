import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { QuickNoteApp } from '@/components/quicknote-app'

function createMockStorage(): Storage {
  const store = new Map<string, string>()
  return {
    get length() {
      return store.size
    },
    clear: () => store.clear(),
    getItem: (key: string) => store.get(key) ?? null,
    key: (index: number) => Array.from(store.keys())[index] ?? null,
    removeItem: (key: string) => { store.delete(key) },
    setItem: (key: string, value: string) => { store.set(key, value) },
  }
}

const {
  windowMinimiseMock,
  windowCloseMock,
  windowIsMaximisedMock,
  createQuickNoteMock,
  updateQuickNoteMock,
  quickNoteSummaryMock,
  quickNoteEmailMock,
  armQuickNoteRecordingMock,
} = vi.hoisted(() => ({
  windowMinimiseMock: vi.fn<() => Promise<void>>(),
  windowCloseMock: vi.fn<() => Promise<void>>(),
  windowIsMaximisedMock: vi.fn<() => Promise<boolean>>(),
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

vi.mock('@wailsio/runtime', () => ({
  Window: {
    Minimise: windowMinimiseMock,
    Close: windowCloseMock,
    IsMaximised: windowIsMaximisedMock,
    Maximise: vi.fn(),
    Restore: vi.fn(),
  },
}))

describe('QuickNoteApp', () => {
  let storageMock: Storage

  beforeEach(() => {
    storageMock = createMockStorage()
    Object.defineProperty(window, 'localStorage', {
      value: storageMock,
      configurable: true,
    })
    storageMock.clear()
    windowMinimiseMock.mockReset()
    windowCloseMock.mockReset()
    windowIsMaximisedMock.mockReset()
    windowMinimiseMock.mockResolvedValue(undefined)
    windowCloseMock.mockResolvedValue(undefined)
    windowIsMaximisedMock.mockResolvedValue(false)
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

  afterEach(() => {
    storageMock.clear()
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

  it('renders the shared chrome with theme and window controls', async () => {
    render(<QuickNoteApp />)

    expect(screen.getByText('New Quick Note')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /switch to light mode/i }))

    await waitFor(() => {
      expect(document.documentElement.dataset.theme).toBe('light')
      expect(storageMock.getItem('speechkit.desktop.theme')).toBe('light')
    })

    fireEvent.click(screen.getByRole('button', { name: /minimise window/i }))
    fireEvent.click(screen.getByRole('button', { name: /close window/i }))

    await waitFor(() => expect(windowMinimiseMock).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(windowCloseMock).toHaveBeenCalledTimes(1))
  })
})
