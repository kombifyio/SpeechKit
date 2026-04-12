import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

import { useLibrary } from '../use-library'

// Mock the speechkit API
vi.mock('@/lib/speechkit', () => ({
  fetchHistory: vi.fn().mockResolvedValue([
    { id: 1, text: 'hello world', language: 'en', provider: 'hf', createdAt: '2025-01-02T00:00:00Z', latencyMs: 100 },
    { id: 2, text: 'older text', language: 'en', provider: 'hf', createdAt: '2025-01-01T00:00:00Z', latencyMs: 120 },
  ]),
  fetchQuickNotes: vi.fn().mockResolvedValue([
    { id: 10, text: 'note a', language: 'en', provider: 'manual', pinned: true, createdAt: '2025-01-03T00:00:00Z', updatedAt: '2025-01-03T00:00:00Z', latencyMs: 0 },
    { id: 11, text: 'note b', language: 'en', provider: 'manual', pinned: false, createdAt: '2025-01-02T00:00:00Z', updatedAt: '2025-01-02T00:00:00Z', latencyMs: 0 },
  ]),
  pinQuickNote: vi.fn().mockResolvedValue('ok'),
  deleteQuickNote: vi.fn().mockResolvedValue('ok'),
  revealDashboardAudio: vi.fn().mockResolvedValue('ok'),
  dashboardAudioDownloadURL: vi.fn((kind: string, id: number) => `/dashboard/audio?kind=${kind}&id=${id}`),
}))

describe('useLibrary', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads history sorted newest-first', async () => {
    const { result } = renderHook(() => useLibrary())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.history).toHaveLength(2)
    expect(result.current.history[0].id).toBe(1) // newer
  })

  it('separates pinned and recent notes', async () => {
    const { result } = renderHook(() => useLibrary())
    await waitFor(() => expect(result.current.quickNotes.length).toBeGreaterThan(0))

    expect(result.current.pinnedQuickNotes).toHaveLength(1)
    expect(result.current.pinnedQuickNotes[0].id).toBe(10)
    expect(result.current.recentQuickNotes).toHaveLength(1)
    expect(result.current.recentQuickNotes[0].id).toBe(11)
  })
})
