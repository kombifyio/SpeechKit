import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { SettingsApp } from '@/components/settings-app'
import type { ModelProfile, SpeechKitSettingsState } from '@/lib/speechkit'

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const { fetchSettingsStateMock, fetchModelProfilesMock, saveSettingsStateMock, saveProviderCredentialMock, clearProviderCredentialMock, testProviderCredentialMock, fetchAudioDevicesMock, setAudioDeviceMock } =
  vi.hoisted(() => ({
    fetchSettingsStateMock: vi.fn<() => Promise<SpeechKitSettingsState>>(),
    fetchModelProfilesMock: vi.fn<() => Promise<ModelProfile[]>>(),
    saveSettingsStateMock: vi.fn<(state: SpeechKitSettingsState) => Promise<string>>(),
    saveProviderCredentialMock: vi.fn<(provider: string, secret: string) => Promise<{ message?: string }>>(),
    clearProviderCredentialMock: vi.fn<(provider: string) => Promise<{ message?: string }>>(),
    testProviderCredentialMock: vi.fn<(provider: string, secret: string) => Promise<{ message?: string }>>(),
    fetchAudioDevicesMock: vi.fn(),
    setAudioDeviceMock: vi.fn<(deviceId: string) => Promise<string>>(),
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

    const modelsSection = await screen.findByText('Models')
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

    await screen.findByText('Models')
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
    expect(screen.getByText('local runtime')).toBeInTheDocument()
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

  it('does not render a hugging face switch in provider settings', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    await screen.findByText('API Keys')
    expect(
      screen.queryByRole('switch', { name: 'Hugging Face Inference' }),
    ).not.toBeInTheDocument()
  })

  it('saves a user provider credential explicitly', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    fireEvent.change(await screen.findByLabelText('Hugging Face API key'), {
      target: { value: 'hf_user_token_123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(saveProviderCredentialMock).toHaveBeenCalledWith('huggingface', 'hf_user_token_123'),
    )
  })

  it('clears a stored provider credential explicitly', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
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
