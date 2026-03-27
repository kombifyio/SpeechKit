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
} = vi.hoisted(() => ({
  fetchHistoryMock: vi.fn<() => Promise<TranscriptionRecord[]>>(),
  fetchQuickNotesMock: vi.fn<() => Promise<QuickNote[]>>(),
  fetchLogsMock: vi.fn<() => Promise<LogEntry[]>>(),
  fetchDashboardStatsMock: vi.fn<() => Promise<import('@/lib/speechkit').DashboardStats>>(),
  revealDashboardAudioMock: vi.fn<() => Promise<string>>(),
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
    dashboardAudioDownloadURL: (kind: 'transcription' | 'quicknote', id: number) =>
      `/dashboard/audio?kind=${kind}&id=${id}`,
  }
})

describe('DashboardApp', () => {
  beforeEach(() => {
    fetchHistoryMock.mockReset()
    fetchQuickNotesMock.mockReset()
    fetchLogsMock.mockReset()
    fetchDashboardStatsMock.mockReset()
    revealDashboardAudioMock.mockReset()
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
  })

  it('opens on a welcome page and labels the library tab correctly', async () => {
    render(<DashboardApp />)

    expect(await screen.findByText(/welcome to speechkit/i)).toBeInTheDocument()
    expect(screen.getByTestId('welcome-scroll')).toHaveClass('overflow-y-auto')
    expect(screen.getByTestId('welcome-kpis')).toHaveClass('flex')
    expect(screen.getByRole('button', { name: 'Library' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Dashboard' })).not.toBeInTheDocument()
    expect(screen.getByText(/quick start/i)).toBeInTheDocument()
    expect(await screen.findByText(/average wpm/i)).toBeInTheDocument()
    expect(screen.getByText('82.7')).toBeInTheDocument()
    expect(screen.getByText(/hold windows alt to talk/i)).toBeInTheDocument()
    expect(screen.getByText(/hover over the pill/i)).toBeInTheDocument()
    expect(screen.getByText(/say summarize/i)).toBeInTheDocument()
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
})
