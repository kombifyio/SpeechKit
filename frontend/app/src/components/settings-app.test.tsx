import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { vi } from 'vitest'

import { SettingsApp } from '@/components/settings-app'
import type { ModelProfile, SpeechKitSettingsState } from '@/lib/speechkit'

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const { fetchSettingsStateMock, fetchModelProfilesMock, saveSettingsStateMock, saveProviderCredentialMock, clearProviderCredentialMock, testProviderCredentialMock, fetchAudioDevicesMock, setAudioDeviceMock, fetchDownloadCatalogMock, fetchDownloadJobsMock, startModelDownloadMock, cancelModelDownloadMock } =
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
    agentHotkey: 'ctrl+shift+k',
    activeMode: 'dictate',
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
  agentHotkey: 'ctrl+shift+k',
  activeMode: 'dictate',
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

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    const modelsSection = await screen.findByText('Model setup')
    expect(modelsSection).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'STT' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'TTS' })).not.toBeInTheDocument()
  })

  it('saves model changes even while hugging face inference is off', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: 'openai/whisper-large-v3',
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    await screen.findByText('Model setup')
    const activeLabel = screen.getByText('Active')
    expect(activeLabel).toBeInTheDocument()
  })

  it('renders model descriptions for live-switchable profiles', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'utility.ollama.gemma4-e4b',
          modality: 'utility',
          name: 'Gemma 4 E4B (Local)',
          executionMode: 'ollama_local',
          description: 'Laptop-friendly local model for summaries and quick actions.',
        },
      ],
      activeProfiles: { utility: 'utility.ollama.gemma4-e4b' },
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    expect(await screen.findByRole('button', { name: 'Utility' })).toBeInTheDocument()
    expect(screen.getByText(/laptop-friendly local model/i)).toBeInTheDocument()
    expect(screen.getByText('local')).toBeInTheDocument()
  })

  it('saves mode changes and separate hotkeys', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    const agentMode = await screen.findByRole('button', { name: 'Agent' })
    fireEvent.click(agentMode)

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ activeMode: 'agent' }),
      ),
    )

    await screen.findByText('Dictate hotkey')
    expect(screen.getByText('Agent hotkey')).toBeInTheDocument()
    const hotkeyButtons = screen.getAllByRole('button', { name: 'Ctrl + Win' })
    fireEvent.click(hotkeyButtons[0])

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          dictateHotkey: 'ctrl+win',
          hotkey: 'ctrl+win',
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
          name: 'Whisper Large v3 (HuggingFace)',
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

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    expect(await screen.findByText(/install key active/i)).toBeInTheDocument()
  })

  it('limits model choices to four cards per modality while keeping the active model visible', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    const utilityProfiles = Array.from({ length: 5 }, (_, index) => ({
      id: `utility-model-${index + 1}`,
      modality: 'utility' as const,
      name: `Utility Model ${index + 1}`,
      executionMode: 'groq_api' as const,
      description: `Utility profile ${index + 1}`,
    }))

    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: utilityProfiles,
      activeProfiles: { utility: 'utility-model-5' },
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

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Utility' }))

    expect(await screen.findByText('Utility Model 1')).toBeInTheDocument()
    expect(screen.getByText('Utility Model 2')).toBeInTheDocument()
    expect(screen.getByText('Utility Model 3')).toBeInTheDocument()
    expect(screen.getByText('Utility Model 5')).toBeInTheDocument()
    expect(screen.queryByText('Utility Model 4')).not.toBeInTheDocument()
  })

  it('shows inline key entry on the model card when a provider key is missing', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'utility.groq.llama-3.1-8b',
          modality: 'utility',
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

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Utility' }))

    const keyInput = await screen.findByLabelText('LLaMA 3.1 8B (Groq) API key')
    expect(keyInput).toBeInTheDocument()

    fireEvent.change(keyInput, {
      target: { value: 'groq_user_token_123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save key' }))

    await waitFor(() =>
      expect(saveProviderCredentialMock).toHaveBeenCalledWith('groq', 'groq_user_token_123'),
    )
  })

  it('shows only providers that are relevant to the visible model options', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'utility.groq.llama-3.1-8b',
          modality: 'utility',
          name: 'LLaMA 3.1 8B (Groq)',
          executionMode: 'groq_api',
          description: 'Fast cloud LLM',
        },
        {
          id: 'agent.openai.gpt-5.4-mini',
          modality: 'agent',
          name: 'GPT-5.4 Mini (OpenAI)',
          executionMode: 'openai_api',
          description: 'Agent profile',
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        openai: {
          provider: 'openai',
          label: 'OpenAI',
          envName: 'OPENAI_API_KEY',
          available: false,
          hasStoredSecret: false,
          source: 'none',
        },
        groq: {
          provider: 'groq',
          label: 'Groq',
          envName: 'GROQ_API_KEY',
          available: true,
          hasStoredSecret: true,
          source: 'user',
        },
      },
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    const providersSection = await screen.findByText('Connected providers')
    const section = providersSection.closest('section')
    expect(section).not.toBeNull()
    expect(within(section as HTMLElement).getByText('Groq')).toBeInTheDocument()
    expect(within(section as HTMLElement).queryByText('OpenAI')).not.toBeInTheDocument()
  })

  it('clears a stored provider credential explicitly', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: 'stt.routed.whisper-large-v3',
          modality: 'stt',
          name: 'Whisper Large v3 (HuggingFace)',
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

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Clear' }))

    await waitFor(() => expect(clearProviderCredentialMock).toHaveBeenCalledWith('huggingface'))
  })
})
