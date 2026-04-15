import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { SettingsApp } from '@/components/settings-app'
import type { ModelProfile, SpeechKitSettingsState } from '@/lib/speechkit'

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const { fetchSettingsStateMock, fetchModelProfilesMock, saveSettingsStateMock, saveProviderCredentialMock, clearProviderCredentialMock, testProviderCredentialMock, fetchAudioDevicesMock, setAudioDeviceMock, fetchDownloadCatalogMock, fetchDownloadJobsMock, startModelDownloadMock, cancelModelDownloadMock, selectDownloadedModelMock } =
  vi.hoisted(() => ({
    fetchSettingsStateMock: vi.fn<() => Promise<SpeechKitSettingsState>>(),
    fetchModelProfilesMock: vi.fn<() => Promise<ModelProfile[]>>(),
    saveSettingsStateMock: vi.fn<(state: SpeechKitSettingsState) => Promise<string>>(),
    saveProviderCredentialMock: vi.fn<(provider: string, secret: string) => Promise<{ message?: string }>>(),
    clearProviderCredentialMock: vi.fn<(provider: string) => Promise<{ message?: string }>>(),
    testProviderCredentialMock: vi.fn<(provider: string, secret: string) => Promise<{ message?: string }>>(),
    fetchAudioDevicesMock: vi.fn(),
    setAudioDeviceMock: vi.fn<(deviceId: string) => Promise<string>>(),
    fetchDownloadCatalogMock: vi.fn(),
    fetchDownloadJobsMock: vi.fn(),
    startModelDownloadMock: vi.fn(),
    cancelModelDownloadMock: vi.fn(),
    selectDownloadedModelMock: vi.fn(),
  }))

vi.mock('@/lib/speechkit', () => ({
  defaultSettingsState: {
    overlayEnabled: true,
    storeBackend: 'sqlite',
    sqlitePath: '',
    postgresConfigured: false,
    postgresDSN: '',
    maxAudioStorageMB: 500,
    hfAvailable: true,
    hfEnabled: false,
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
    hfHasUserToken: false,
    hfHasInstallToken: false,
    hfTokenSource: 'none',
    activeProfiles: {},
    providerCredentials: {
      huggingface: {
        provider: 'huggingface',
        label: 'Hugging Face',
        envName: 'HF_TOKEN',
        available: true,
        hasStoredSecret: false,
        source: 'none',
      },
    },
  } satisfies SpeechKitSettingsState,
  fetchSettingsState: fetchSettingsStateMock,
  fetchModelProfiles: fetchModelProfilesMock,
  saveSettingsState: saveSettingsStateMock,
  saveProviderCredential: saveProviderCredentialMock,
  clearProviderCredential: clearProviderCredentialMock,
  testProviderCredential: testProviderCredentialMock,
  fetchAudioDevices: fetchAudioDevicesMock,
  setAudioDevice: setAudioDeviceMock,
  fetchDownloadCatalog: fetchDownloadCatalogMock,
  fetchDownloadJobs: fetchDownloadJobsMock,
  startModelDownload: startModelDownloadMock,
  cancelModelDownload: cancelModelDownloadMock,
  selectDownloadedModel: selectDownloadedModelMock,
}))

vi.mock('@/components/ui/mic-selector', () => ({
  MicSelector: ({
    value,
    onValueChange,
  }: {
    value?: string
    onValueChange?: (deviceId: string) => void
  }) => (
    <button
      type="button"
      aria-label={`Microphone ${value || 'Studio Mic'}`}
      onClick={() => {
        void setAudioDeviceMock('mic-1')
        onValueChange?.('mic-1')
      }}
    >
      Microphone {value || 'Studio Mic'}
    </button>
  ),
}))

const baseSettings: SpeechKitSettingsState = {
  overlayEnabled: true,
  storeBackend: 'sqlite',
  sqlitePath: 'C:/Users/testuser/AppData/Roaming/SpeechKit/feedback.db',
  postgresConfigured: false,
  postgresDSN: '',
  maxAudioStorageMB: 500,
  hfAvailable: true,
  hfEnabled: false,
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
  selectedAudioDeviceId: 'mic-1',
  hfHasUserToken: false,
  hfHasInstallToken: false,
  hfTokenSource: 'none',
  activeProfiles: { stt: 'stt-local' },
  profiles: [
    {
      id: 'stt-local',
      modality: 'stt',
      name: 'Qwen/Qwen3-ASR-1.7B',
      executionMode: 'local',
      description: 'Default local STT profile',
    },
  ],
  providerCredentials: {
    huggingface: {
      provider: 'huggingface',
      label: 'Hugging Face',
      envName: 'HF_TOKEN',
      available: true,
      hasStoredSecret: false,
      source: 'none',
    },
  },
}

describe('SettingsApp', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    fetchSettingsStateMock.mockReset()
    fetchModelProfilesMock.mockReset()
    saveSettingsStateMock.mockReset()
    saveProviderCredentialMock.mockReset()
    clearProviderCredentialMock.mockReset()
    testProviderCredentialMock.mockReset()
    fetchAudioDevicesMock.mockReset()
    setAudioDeviceMock.mockReset()
    fetchDownloadCatalogMock.mockReset()
    fetchDownloadJobsMock.mockReset()
    startModelDownloadMock.mockReset()
    cancelModelDownloadMock.mockReset()
    selectDownloadedModelMock.mockReset()

    fetchModelProfilesMock.mockResolvedValue([])
    fetchAudioDevicesMock.mockResolvedValue({
      devices: [
        { deviceId: 'mic-1', label: 'Studio Mic', groupId: 'group-1', isDefault: true },
        { deviceId: 'mic-2', label: 'Backup Mic', groupId: 'group-2' },
      ],
      selectedDeviceId: 'mic-1',
    })
    saveSettingsStateMock.mockResolvedValue('Gespeichert')
    saveProviderCredentialMock.mockResolvedValue({ message: 'Saved' })
    clearProviderCredentialMock.mockResolvedValue({ message: 'Cleared' })
    testProviderCredentialMock.mockResolvedValue({ message: 'Key valid' })
    setAudioDeviceMock.mockResolvedValue('Selected')
    fetchDownloadCatalogMock.mockResolvedValue([])
    fetchDownloadJobsMock.mockResolvedValue([])
    startModelDownloadMock.mockResolvedValue({ id: 'dl-1', modelId: 'test', profileId: 'test', status: 'pending', progress: 0, bytesDone: 0, totalBytes: 0, statusText: 'Starting…' })
    cancelModelDownloadMock.mockResolvedValue(undefined)
    selectDownloadedModelMock.mockResolvedValue({ message: 'Selected' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps model selection available while hugging face inference is off', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: 'openai/whisper-large-v3',
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Transcribe' }))

    const modelsSection = await screen.findByText('Model setup')
    expect(modelsSection).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Transcribe' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'TTS' })).not.toBeInTheDocument()
  })

  it('saves model changes even while hugging face inference is off', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: 'openai/whisper-large-v3',
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Transcribe' }))

    await screen.findByText('Model setup')
    const activeLabel = screen.getByText('Active')
    expect(activeLabel).toBeInTheDocument()
  })

  it('renders model descriptions for live-switchable profiles', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'assist.ollama.gemma4-e4b',
          modality: 'assist',
          name: 'Gemma 4 E4B (Local)',
          executionMode: 'ollama_local',
          description: 'Laptop-friendly local model for summaries and quick actions.',
        },
      ],
      activeProfiles: { assist: 'assist.ollama.gemma4-e4b' },
    })

    render(<SettingsApp />)

    const assistButtons = await screen.findAllByRole('button', { name: 'Assist' })
    fireEvent.click(assistButtons[0])

    expect(await screen.findByText(/laptop-friendly local model/i)).toBeInTheDocument()
    expect(screen.getByText('local')).toBeInTheDocument()
  })

  it('hides runtime mode selection and keeps only per-mode hotkeys in settings', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    await screen.findByText('Dictate hotkey')
    expect(screen.queryByText('Mode')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Active mode Assist' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Active mode None' })).not.toBeInTheDocument()
    expect(screen.getByText('Assist hotkey')).toBeInTheDocument()
    expect(screen.getByText('Voice Agent hotkey')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Dictate hotkey Ctrl + Win' }))

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          dictateHotkey: 'ctrl+win',
          hotkey: 'ctrl+win',
        }),
      ),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Voice Agent hotkey disabled' }))

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentHotkey: '',
        }),
      ),
    )
  })

  it('shows the selected microphone from the desktop selector', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    const micButton = await screen.findByRole('button', {
      name: /microphone studio mic/i,
    })
    expect(micButton).toBeInTheDocument()
  })

  it('saves vocabulary dictionary changes', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      vocabularyDictionary: 'kombi fire => Kombify',
    })

    render(<SettingsApp />)

    const input = await screen.findByLabelText('Vocabulary dictionary')
    fireEvent.change(input, { target: { value: 'kombi fire => Kombify\nAcmeOS' } })

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          vocabularyDictionary: 'kombi fire => Kombify\nAcmeOS',
        }),
      ),
    )
  })

  it('saves overlay design changes', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    const button = await screen.findByRole('button', { name: 'kombify' })

    fireEvent.click(button)

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ design: 'kombify' }),
      ),
    )
  })

  it('allows enabling movable overlay while keeping the position chips', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('switch', { name: 'Movable overlay' }))

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ overlayMovable: true, overlayPosition: 'top' }),
      ),
    )

    expect(
      screen.getByText(/drag the center bubble inside the pill panel/i),
    ).toBeInTheDocument()
  })

  it('saves local audio storage preferences', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    const saveAudioToggle = await screen.findByRole('switch', {
      name: 'Save raw audio locally',
    })
    fireEvent.click(saveAudioToggle)

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ saveAudio: false }),
      ),
    )

    const retentionSelect = await screen.findByLabelText('Audio retention')
    fireEvent.change(retentionSelect, { target: { value: '30' } })

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ audioRetentionDays: 30 }),
      ),
    )
  })

  it('persists the postgres connection string once the backend is configured locally', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'PostgreSQL' }))

    fireEvent.change(await screen.findByLabelText('PostgreSQL connection string'), {
      target: {
        value: 'postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable',
      },
    })

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          storeBackend: 'postgres',
          postgresDSN:
            'postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable',
        }),
      ),
    )
  })

  it('shows backend-specific storage copy and validation hints', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    expect(await screen.findByLabelText('SQLite path')).toBeInTheDocument()
    expect(screen.queryByLabelText('PostgreSQL connection string')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'PostgreSQL' }))

    expect(await screen.findByLabelText('PostgreSQL connection string')).toBeInTheDocument()
    expect(screen.queryByLabelText('SQLite path')).not.toBeInTheDocument()
    expect(
      screen.getByText(/add a postgresql connection string before switching the metadata backend/i),
    ).toBeInTheDocument()
  })

  it('does not persist the postgres backend switch until a connection string is present', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'PostgreSQL' }))

    await screen.findByLabelText('PostgreSQL connection string')
    expect(saveSettingsStateMock).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('PostgreSQL connection string'), {
      target: {
        value: 'postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable',
      },
    })

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          storeBackend: 'postgres',
          postgresDSN:
            'postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable',
        }),
      ),
    )
  })

  it('persists the local audio storage cap', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    const input = await screen.findByLabelText('Max local audio storage (MB)')
    fireEvent.change(input, {
      target: { value: '1024' },
    })

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ maxAudioStorageMB: 1024 }),
      ),
    )
  })

  it('shows stored token status for the provider', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'stt.routed.whisper-large-v3',
          modality: 'stt',
          name: 'Whisper Large v3 (Hugging Face)',
          executionMode: 'hf_routed',
          description: 'Managed STT profile',
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: 'huggingface',
          label: 'Hugging Face',
          envName: 'HF_TOKEN',
          available: true,
          hasStoredSecret: true,
          source: 'install',
        },
      },
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Transcribe' }))

    expect(await screen.findByText(/install token active/i)).toBeInTheDocument()
  })

  it('limits model choices to four cards per modality while keeping the active model visible', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    const assistProfiles = Array.from({ length: 6 }, (_, index) => ({
      id: `assist-model-${index + 1}`,
      modality: 'assist' as const,
      name: `Assist Model ${index + 1}`,
      executionMode: 'groq_api' as const,
      description: `Assist profile ${index + 1}`,
    }))

    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: assistProfiles,
      activeProfiles: { assist: 'assist-model-6' },
      providerCredentials: {
        groq: {
          provider: 'groq',
          label: 'Groq',
          envName: 'GROQ_API_KEY',
          available: false,
          hasStoredSecret: false,
          source: 'none',
        },
      },
    })

    render(<SettingsApp />)

    const assistNavButtons = await screen.findAllByRole('button', { name: 'Assist' })
    fireEvent.click(assistNavButtons[0])

    expect(await screen.findByText('Assist Model 1')).toBeInTheDocument()
    expect(screen.getByText('Assist Model 2')).toBeInTheDocument()
    expect(screen.getByText('Assist Model 3')).toBeInTheDocument()
    expect(screen.getByText('Assist Model 6')).toBeInTheDocument()
    expect(screen.queryByText('Assist Model 4')).not.toBeInTheDocument()
    expect(screen.queryByText('Assist Model 5')).not.toBeInTheDocument()
  })

  it('shows inline key entry on the model card when a provider key is missing', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'assist.groq.llama-3.1-8b',
          modality: 'assist',
          name: 'LLaMA 3.1 8B (Groq)',
          executionMode: 'groq_api',
          description: 'Fast cloud LLM',
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        groq: {
          provider: 'groq',
          label: 'Groq',
          envName: 'GROQ_API_KEY',
          available: false,
          hasStoredSecret: false,
          source: 'none',
        },
      },
    })

    render(<SettingsApp />)

    const assistNavButtons = await screen.findAllByRole('button', { name: 'Assist' })
    fireEvent.click(assistNavButtons[0])

    const keyInput = await screen.findByLabelText('LLaMA 3.1 8B (Groq) Groq key')
    expect(keyInput).toBeInTheDocument()

    fireEvent.change(keyInput, {
      target: { value: 'groq_user_token_123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save key' }))

    await waitFor(() =>
      expect(saveProviderCredentialMock).toHaveBeenCalledWith('groq', 'groq_user_token_123'),
    )
  })

  it('keeps Hugging Face token setup directly on the model card', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'stt.routed.whisper-large-v3',
          modality: 'stt',
          name: 'Whisper Large v3 (Hugging Face)',
          executionMode: 'hf_routed',
          description: 'Managed STT profile',
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        huggingface: {
          provider: 'huggingface',
          label: 'Hugging Face',
          envName: 'HF_TOKEN',
          available: false,
          hasStoredSecret: false,
          source: 'none',
        },
      },
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Transcribe' }))

    expect(await screen.findByText('Whisper Large v3 (Hugging Face)')).toBeInTheDocument()
    expect(screen.getByText('Add Hugging Face token')).toBeInTheDocument()
    expect(screen.getByLabelText('Whisper Large v3 (Hugging Face) Hugging Face token')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save token' })).toBeInTheDocument()
  })

  it('keeps Hugging Face-backed assist models selectable from the Assist tab', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'assist.hf.qwen3-coder',
          modality: 'assist',
          name: 'Qwen 3 Coder (Hugging Face)',
          executionMode: 'hf_inference',
          description: 'Managed coding assistant profile',
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        huggingface: {
          provider: 'huggingface',
          label: 'Hugging Face',
          envName: 'HF_TOKEN',
          available: true,
          hasStoredSecret: true,
          source: 'user',
        },
      },
    })

    render(<SettingsApp />)

    const assistNavButtons = await screen.findAllByRole('button', { name: 'Assist' })
    fireEvent.click(assistNavButtons[0])

    expect(await screen.findByText('Qwen 3 Coder (Hugging Face)')).toBeInTheDocument()
    expect(screen.getByText(/user token active/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Use model' })).toBeInTheDocument()
  })

  it('does not render the duplicated connected providers section', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'stt.routed.whisper-large-v3',
          modality: 'stt',
          name: 'Whisper Large v3 (Hugging Face)',
          executionMode: 'hf_routed',
          description: 'Managed STT profile',
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: 'huggingface',
          label: 'Hugging Face',
          envName: 'HF_TOKEN',
          available: true,
          hasStoredSecret: true,
          source: 'user',
        },
      },
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Transcribe' }))

    expect(await screen.findByText('Whisper Large v3 (Hugging Face)')).toBeInTheDocument()
    expect(screen.queryByText('Connected providers')).not.toBeInTheDocument()
  })

  it('clears a stored provider credential explicitly', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'stt.routed.whisper-large-v3',
          modality: 'stt',
          name: 'Whisper Large v3 (Hugging Face)',
          executionMode: 'hf_routed',
          description: 'Managed STT profile',
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: 'huggingface',
          label: 'Hugging Face',
          envName: 'HF_TOKEN',
          available: true,
          hasStoredSecret: true,
          source: 'user',
        },
      },
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Transcribe' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Clear' }))

    await waitFor(() => expect(clearProviderCredentialMock).toHaveBeenCalledWith('huggingface'))
  })

  it('renames the STT settings tab to Transcribe', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    expect(await screen.findByRole('button', { name: 'Transcribe' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'STT' })).not.toBeInTheDocument()
  })

  it('allows switching between available local whisper downloads', async () => {
    const initialSettings: SpeechKitSettingsState = {
      ...baseSettings,
      profiles: [
        {
          id: 'stt.local.whispercpp',
          modality: 'stt',
          name: 'Whisper.cpp (Local)',
          executionMode: 'local',
          description: 'Offline Windows dictation with Whisper.cpp.',
        },
      ],
      activeProfiles: { stt: 'stt.local.whispercpp' },
    }
    fetchSettingsStateMock
      .mockResolvedValueOnce(initialSettings)
      .mockResolvedValueOnce(initialSettings)
    fetchDownloadCatalogMock
      .mockResolvedValueOnce([
        {
          id: 'whisper.ggml-small',
          profileId: 'stt.local.whispercpp',
          name: 'Whisper Small Multilingual (466 MB)',
          description: 'Small local model',
          sizeLabel: '466 MB',
          sizeBytes: 484264096,
          kind: 'http',
          license: 'mit',
          available: true,
          selected: true,
        },
        {
          id: 'whisper.ggml-large-v3-turbo',
          profileId: 'stt.local.whispercpp',
          name: 'Whisper Large v3 Turbo',
          description: 'Turbo local model',
          sizeLabel: '~1.6 GB',
          sizeBytes: 1624555275,
          kind: 'http',
          license: 'mit',
          available: true,
          recommended: true,
          selected: false,
        },
        {
          id: 'whisper.ggml-large-v3',
          profileId: 'stt.local.whispercpp',
          name: 'Whisper Large v3 (Open Source)',
          description: 'Large local model',
          sizeLabel: '~3.1 GB',
          sizeBytes: 3100000000,
          kind: 'http',
          license: 'mit',
          available: true,
          selected: false,
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'whisper.ggml-small',
          profileId: 'stt.local.whispercpp',
          name: 'Whisper Small Multilingual (466 MB)',
          description: 'Small local model',
          sizeLabel: '466 MB',
          sizeBytes: 484264096,
          kind: 'http',
          license: 'mit',
          available: true,
          selected: false,
        },
        {
          id: 'whisper.ggml-large-v3-turbo',
          profileId: 'stt.local.whispercpp',
          name: 'Whisper Large v3 Turbo',
          description: 'Turbo local model',
          sizeLabel: '~1.6 GB',
          sizeBytes: 1624555275,
          kind: 'http',
          license: 'mit',
          available: true,
          recommended: true,
          selected: true,
        },
        {
          id: 'whisper.ggml-large-v3',
          profileId: 'stt.local.whispercpp',
          name: 'Whisper Large v3 (Open Source)',
          description: 'Large local model',
          sizeLabel: '~3.1 GB',
          sizeBytes: 3100000000,
          kind: 'http',
          license: 'mit',
          available: true,
          selected: false,
        },
      ])

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Transcribe' }))

    const switchButton = await screen.findByRole('button', {
      name: 'Use Whisper Large v3 Turbo',
    })
    fireEvent.click(switchButton)

    await waitFor(() =>
      expect(selectDownloadedModelMock).toHaveBeenCalledWith('whisper.ggml-large-v3-turbo'),
    )
    expect(await screen.findByLabelText('Use Whisper Small Multilingual (466 MB)')).toBeInTheDocument()
    expect(screen.getByText('Selected on this device')).toBeInTheDocument()
    expect(screen.getByText('recommended')).toBeInTheDocument()
  })
})
