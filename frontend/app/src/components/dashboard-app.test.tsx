import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { DashboardApp } from '@/components/dashboard-app'
import type { LogEntry, QuickNote, TranscriptionRecord } from '@/lib/speechkit'

const {
  fetchHistoryMock,
  fetchQuickNotesMock,
  fetchLogsMock,
  fetchDashboardStatsMock,
  revealDashboardAudioMock,
  openQuickNoteCaptureMock,
  openQuickNoteEditorMock,
} = vi.hoisted(() => ({
  fetchHistoryMock: vi.fn<() => Promise<TranscriptionRecord[]>>(),
  fetchQuickNotesMock: vi.fn<() => Promise<QuickNote[]>>(),
  fetchLogsMock: vi.fn<() => Promise<LogEntry[]>>(),
  fetchDashboardStatsMock: vi.fn<() => Promise<import('@/lib/speechkit').DashboardStats>>(),
  revealDashboardAudioMock: vi.fn<() => Promise<string>>(),
  openQuickNoteCaptureMock: vi.fn<() => Promise<string>>(),
  openQuickNoteEditorMock: vi.fn<(noteId?: number) => Promise<string>>(),
}))

vi.mock('@/lib/speechkit', async () => {
  const actual = await vi.importActual<typeof import('@/lib/speechkit')>('@/lib/speechkit')
  return {
    ...actual,
    fetchHistory: fetchHistoryMock,
    fetchQuickNotes: fetchQuickNotesMock,
    fetchLogs: fetchLogsMock,
    fetchDashboardStats: fetchDashboardStatsMock,
    revealDashboardAudio: revealDashboardAudioMock,
    openQuickNoteCapture: openQuickNoteCaptureMock,
    openQuickNoteEditor: openQuickNoteEditorMock,
    dashboardAudioDownloadURL: (kind: 'transcription' | 'quicknote', id: number) =>
      `/dashboard/audio?kind=${kind}&id=${id}`,
  }
})

describe('DashboardApp', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn> | undefined

  beforeEach(() => {
    window.history.replaceState({}, '', '/')
    window.sessionStorage.clear()

    // Mock /app/setup-status to skip onboarding wizard in tests
    const originalFetch = window.fetch.bind(window)
    fetchSpy = vi.spyOn(window, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : (input as Request).url
      if (url === '/app/setup-status') {
        return new Response(JSON.stringify({ setupDone: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (url === '/app/version') {
        return new Response(JSON.stringify({ version: '0.1.3' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return originalFetch(input, init)
    })

    fetchHistoryMock.mockReset()
    fetchQuickNotesMock.mockReset()
    fetchLogsMock.mockReset()
    fetchDashboardStatsMock.mockReset()
    revealDashboardAudioMock.mockReset()
    openQuickNoteCaptureMock.mockReset()
    openQuickNoteEditorMock.mockReset()
    fetchHistoryMock.mockResolvedValue([])
    fetchQuickNotesMock.mockResolvedValue([])
    fetchLogsMock.mockResolvedValue([])
    fetchDashboardStatsMock.mockResolvedValue({
      transcriptions: 12,
      quickNotes: 3,
      totalWords: 248,
      totalAudioDurationMs: 180000,
      averageWordsPerMinute: 82.7,
      averageLatencyMs: 410,
    })
    revealDashboardAudioMock.mockResolvedValue('Opened')
    openQuickNoteCaptureMock.mockResolvedValue('Capture opened')
    openQuickNoteEditorMock.mockResolvedValue('Editor opened')
  })

  afterEach(() => {
    fetchSpy?.mockRestore()
    window.history.replaceState({}, '', '/')
    window.sessionStorage.clear()
  })

  it('opens on a welcome page and labels the library tab correctly', async () => {
    render(<DashboardApp />)

    expect(await screen.findByText(/welcome to speechkit/i)).toBeInTheDocument()
    expect(screen.getByTestId('welcome-scroll')).toHaveClass('overflow-y-auto')
    expect(screen.getByTestId('welcome-kpis')).toHaveClass('flex')
    expect(screen.getByRole('button', { name: 'Library' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Dashboard' })).not.toBeInTheDocument()
    expect(screen.getByText(/quick start/i)).toBeInTheDocument()
    expect(await screen.findByText('82.7')).toBeInTheDocument()
    expect(screen.getByText(/average wpm/i)).toBeInTheDocument()
    expect(screen.getByText(/hold windows alt to talk/i)).toBeInTheDocument()
    expect(screen.getByText(/hover over the pill/i)).toBeInTheDocument()
    expect(screen.getByText(/say summarize/i)).toBeInTheDocument()
  })

  it('replaces onboarding with an activity home once transcriptions exist', async () => {
    fetchHistoryMock.mockResolvedValue([
      {
        id: 2,
        text: 'newer transcription',
        language: 'de',
        provider: 'local',
        model: 'whisper.cpp',
        latencyMs: 110,
        createdAt: '2026-03-26T09:30:00',
      },
    ])
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 11,
        text: 'Call with AcmeOS team',
        language: 'de',
        provider: 'manual',
        latencyMs: 0,
        pinned: true,
        createdAt: '2026-03-26T09:30:00',
        updatedAt: '2026-03-26T09:30:00',
      },
    ])

    render(<DashboardApp />)

    expect(await screen.findByText(/recent activity/i)).toBeInTheDocument()
    expect(screen.getByText(/latest transcription/i)).toBeInTheDocument()
    expect(screen.getByText(/newer transcription/i)).toBeInTheDocument()
    expect(screen.getByText(/pinned notes/i)).toBeInTheDocument()
    expect(screen.getByText(/call with acmeos team/i)).toBeInTheDocument()
    expect(screen.queryByText(/quick start/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/hold windows alt to talk/i)).not.toBeInTheDocument()
  })

  it('shows library entries sorted by newest date and includes absolute timestamps', async () => {
    fetchHistoryMock.mockResolvedValue([
      {
        id: 1,
        text: 'older transcription',
        language: 'de',
        provider: 'hf',
        latencyMs: 120,
        createdAt: '2026-03-24T08:15:00',
      },
      {
        id: 2,
        text: 'newer transcription',
        language: 'de',
        provider: 'hf',
        latencyMs: 110,
        createdAt: '2026-03-26T09:30:00',
      },
    ])
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 10,
        text: 'older note',
        language: 'de',
        provider: 'manual',
        latencyMs: 0,
        pinned: false,
        createdAt: '2026-03-24T07:45:00',
        updatedAt: '2026-03-24T07:45:00',
      },
      {
        id: 11,
        text: 'newer note',
        language: 'de',
        provider: 'manual',
        latencyMs: 0,
        pinned: true,
        createdAt: '2026-03-26T09:30:00',
        updatedAt: '2026-03-26T09:30:00',
      },
    ])

    render(<DashboardApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Library' }))

    await waitFor(() => expect(fetchHistoryMock).toHaveBeenCalled())

    const transcriptionRows = await screen.findAllByTestId('transcription-row')
    expect(transcriptionRows[0]).toHaveTextContent('newer transcription')
    expect(transcriptionRows[1]).toHaveTextContent('older transcription')

    expect(screen.getByText(/pinned notes/i)).toBeInTheDocument()
    const quickNoteRows = await screen.findAllByTestId('quicknote-row')
    expect(quickNoteRows[0]).toHaveTextContent('newer note')
    expect(quickNoteRows[1]).toHaveTextContent('older note')
    expect(quickNoteRows[0]).toHaveTextContent(/26\/03\/2026.*09:30|09:30.*26\/03\/2026/)
    expect(quickNoteRows[0]).toHaveTextContent(/pinned/i)
    expect(screen.queryByText(/show all notes/i)).not.toBeInTheDocument()
  })

  it('shows audio actions for records with stored audio', async () => {
    fetchHistoryMock.mockResolvedValue([
      {
        id: 2,
        text: 'newer transcription',
        language: 'de',
        provider: 'hf',
        model: 'openai/whisper-large-v3',
        durationMs: 2400,
        latencyMs: 110,
        audio: {
          storageKind: 'local-file',
          mimeType: 'audio/wav',
          sizeBytes: 4096,
          durationMs: 2400,
        },
        createdAt: '2026-03-26T09:30:00',
      } as unknown as TranscriptionRecord,
    ])
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 11,
        text: 'newer note',
        language: 'de',
        provider: 'capture',
        durationMs: 1800,
        latencyMs: 0,
        audio: {
          storageKind: 'local-file',
          mimeType: 'audio/wav',
          sizeBytes: 2048,
          durationMs: 1800,
        },
        pinned: true,
        createdAt: '2026-03-26T09:30:00',
        updatedAt: '2026-03-26T09:30:00',
      },
    ])

    render(<DashboardApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Library' }))

    expect(await screen.findByText('2.4s')).toBeInTheDocument()
    expect(screen.getByText('1.8s')).toBeInTheDocument()
    expect(screen.queryByText(/wav 4 kb/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/wav 2 kb/i)).not.toBeInTheDocument()
    expect(screen.getByText('large-v3')).toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('button', { name: /show file/i })[0])

    await waitFor(() =>
      expect(revealDashboardAudioMock).toHaveBeenCalledWith('transcription', 2),
    )

    const downloadLinks = screen.getAllByRole('link', { name: /download audio/i })
    expect(downloadLinks[0]).toHaveAttribute('href', '/dashboard/audio?kind=transcription&id=2')
    expect(downloadLinks[1]).toHaveAttribute('href', '/dashboard/audio?kind=quicknote&id=11')
  })

  it('opens the requested tab from the location hash and persists the selection', async () => {
    window.history.replaceState({}, '', '/dashboard.html#logs')

    render(<DashboardApp />)

    expect(await screen.findByText(/application logs/i)).toBeInTheDocument()
    await waitFor(() => expect(fetchLogsMock).toHaveBeenCalled())
    expect(window.sessionStorage.getItem('speechkit.dashboard.tab')).toBe('logs')
  })

  it('restores the last selected tab from session storage when no hash is set', async () => {
    window.sessionStorage.setItem('speechkit.dashboard.tab', 'library')
    fetchHistoryMock.mockResolvedValue([
      {
        id: 2,
        text: 'restored transcription',
        language: 'de',
        provider: 'hf',
        latencyMs: 110,
        createdAt: '2026-03-26T09:30:00',
      },
    ])

    render(<DashboardApp />)

    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
    expect(await screen.findByText('restored transcription')).toBeInTheDocument()
  })

  it('opens quick capture from the welcome quick note action through the client wrapper', async () => {
    render(<DashboardApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Quick Note' }))

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith('/quicknotes/open-capture', { method: 'POST' }),
    )
  })

  it('opens quick note editor actions through the client wrapper', async () => {
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 11,
        text: 'newer note',
        language: 'de',
        provider: 'manual',
        latencyMs: 0,
        pinned: true,
        createdAt: '2026-03-26T09:30:00',
        updatedAt: '2026-03-26T09:30:00',
      },
    ])

    render(<DashboardApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Library' }))
    fireEvent.click(await screen.findByRole('button', { name: '+ New' }))

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith('/quicknotes/open-editor', { method: 'POST' }),
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith('/quicknotes/open-editor?id=11', { method: 'POST' }),
    )
  })
})
