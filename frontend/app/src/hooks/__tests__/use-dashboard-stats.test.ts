import { describe, expect, it, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

import { useDashboardStats } from '../use-dashboard-stats'

vi.mock('@/lib/speechkit', () => ({
  fetchDashboardStats: vi.fn().mockResolvedValue({
    transcriptions: 42,
    quickNotes: 7,
    totalWords: 1200,
    totalAudioDurationMs: 180000,
    averageWordsPerMinute: 120.5,
    averageLatencyMs: 350,
  }),
  fetchHistory: vi.fn().mockResolvedValue([
    { id: 1, text: 'latest', language: 'en', provider: 'hf', createdAt: '2025-01-02T00:00:00Z', latencyMs: 100 },
    { id: 2, text: 'older', language: 'en', provider: 'hf', createdAt: '2025-01-01T00:00:00Z', latencyMs: 120 },
  ]),
  fetchQuickNotes: vi.fn().mockResolvedValue([
    { id: 10, text: 'pinned note', language: 'en', provider: 'manual', pinned: true, createdAt: '2025-01-01T00:00:00Z', updatedAt: '2025-01-01T00:00:00Z', latencyMs: 0 },
  ]),
}))

// Mock /app/version endpoint
const mockFetch = vi.fn().mockResolvedValue({
  json: () => Promise.resolve({ latestVersion: '1.2.0', updateURL: 'https://example.com' }),
  ok: true,
})
vi.stubGlobal('fetch', mockFetch)

describe('useDashboardStats', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads stats and derives latest transcription', async () => {
    const { result } = renderHook(() => useDashboardStats())
    await waitFor(() => expect(result.current.stats).not.toBeNull())

    expect(result.current.stats?.transcriptions).toBe(42)
    expect(result.current.latestTranscription?.text).toBe('latest')
    expect(result.current.hasHistory).toBe(true)
    expect(result.current.featuredNotes).toHaveLength(1)
  })
})
