import { describe, expect, it, vi, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'

import { useSettings } from '../use-settings'

vi.mock('@/lib/speechkit', () => ({
  defaultSettingsState: {
    overlayEnabled: true,
    storeBackend: 'sqlite',
    sqlitePath: '',
    postgresConfigured: false,
    postgresDSN: '',
    maxAudioStorageMB: 500,
    hfAvailable: false,
    hfEnabled: false,
    hfHasUserToken: false,
    hfHasInstallToken: false,
    hfTokenSource: 'none',
    hotkey: 'win+alt',
    dictateHotkey: 'win+alt',
    assistHotkey: 'ctrl+shift+j',
    voiceAgentHotkey: 'ctrl+shift+k',
    agentHotkey: 'ctrl+shift+j',
    agentMode: 'assist',
    activeMode: 'none',
    availableModes: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
    hfModel: 'openai/whisper-large-v3-turbo',
    visualizer: 'pill',
    design: 'default',
    overlayPosition: 'top',
    overlayMovable: false,
    overlayFreeX: 0,
    overlayFreeY: 0,
    vocabularyDictionary: '',
    saveAudio: true,
    audioRetentionDays: 7,
    selectedAudioDeviceId: '',
    activeProfiles: {},
  },
  fetchSettingsState: vi.fn().mockResolvedValue({
    overlayEnabled: true,
    storeBackend: 'sqlite',
    sqlitePath: '',
    postgresConfigured: false,
    postgresDSN: '',
    maxAudioStorageMB: 500,
    hfAvailable: false,
    hfEnabled: false,
    hfHasUserToken: false,
    hfHasInstallToken: false,
    hfTokenSource: 'none',
    hotkey: 'win+alt',
    dictateHotkey: 'win+alt',
    assistHotkey: 'ctrl+shift+j',
    voiceAgentHotkey: 'ctrl+shift+k',
    agentHotkey: 'ctrl+shift+j',
    agentMode: 'assist',
    activeMode: 'none',
    availableModes: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
    hfModel: 'openai/whisper-large-v3-turbo',
    visualizer: 'pill',
    design: 'default',
    overlayPosition: 'top',
    overlayMovable: false,
    overlayFreeX: 0,
    overlayFreeY: 0,
    vocabularyDictionary: '',
    saveAudio: true,
    audioRetentionDays: 7,
    selectedAudioDeviceId: '',
    activeProfiles: {},
    profiles: [
      { id: 'p1', name: 'Turbo', modality: 'stt', executionMode: 'hf_routed' },
    ],
  }),
  fetchModelProfiles: vi.fn().mockResolvedValue([]),
  fetchDownloadCatalog: vi.fn().mockResolvedValue([]),
  fetchDownloadJobs: vi.fn().mockResolvedValue([]),
  saveSettingsState: vi.fn().mockResolvedValue('Saved'),
  saveProviderCredential: vi.fn().mockResolvedValue({ message: 'Token saved' }),
  clearProviderCredential: vi.fn().mockResolvedValue({ message: 'Token cleared' }),
  testProviderCredential: vi.fn().mockResolvedValue({ message: 'Token valid' }),
  cancelModelDownload: vi.fn().mockResolvedValue(undefined),
  startModelDownload: vi.fn().mockResolvedValue({ id: 'j1', modelId: 'm1', profileId: 'p1', status: 'pending', progress: 0, bytesDone: 0, totalBytes: 1000, statusText: 'Starting' }),
}))

describe('useSettings', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads settings on mount', async () => {
    const showToast = vi.fn()
    const { result } = renderHook(() => useSettings(showToast))

    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.settings.dictateHotkey).toBe('win+alt')
    expect(result.current.settings.profiles).toHaveLength(1)
  })

  it('switches settings tab', async () => {
    const showToast = vi.fn()
    const { result } = renderHook(() => useSettings(showToast))

    act(() => result.current.setTab('stt'))
    expect(result.current.tab).toBe('stt')
  })

  it('updates settings optimistically', async () => {
    const showToast = vi.fn()
    const { result } = renderHook(() => useSettings(showToast))

    await waitFor(() => expect(result.current.loaded).toBe(true))

    act(() => result.current.updateSettings({ activeMode: 'assist' }))
    expect(result.current.settings.activeMode).toBe('assist')
  })

  it('computes postgresReady', async () => {
    const showToast = vi.fn()
    const { result } = renderHook(() => useSettings(showToast))

    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.postgresReady).toBe(false)
  })
})
