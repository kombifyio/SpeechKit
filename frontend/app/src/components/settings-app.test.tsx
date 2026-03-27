import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

import { SettingsApp } from '@/components/settings-app'
import type { ModelProfile, SpeechKitSettingsState } from '@/lib/speechkit'

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const { fetchSettingsStateMock, fetchModelProfilesMock, saveSettingsStateMock, saveHuggingFaceTokenMock, clearHuggingFaceTokenMock, fetchAudioDevicesMock, setAudioDeviceMock } =
  vi.hoisted(() => ({
    fetchSettingsStateMock: vi.fn<() => Promise<SpeechKitSettingsState>>(),
    fetchModelProfilesMock: vi.fn<() => Promise<ModelProfile[]>>(),
    saveSettingsStateMock: vi.fn<(state: SpeechKitSettingsState) => Promise<string>>(),
    saveHuggingFaceTokenMock: vi.fn<(token: string) => Promise<string>>(),
    clearHuggingFaceTokenMock: vi.fn<() => Promise<string>>(),
    fetchAudioDevicesMock: vi.fn(),
    setAudioDeviceMock: vi.fn<(deviceId: string) => Promise<string>>(),
  }))

vi.mock('@/lib/speechkit', () => ({
  defaultSettingsState: {
    overlayEnabled: true,
    hfEnabled: false,
    hotkey: 'win+alt',
    dictateHotkey: 'win+alt',
    agentHotkey: 'ctrl+shift+k',
    activeMode: 'dictate',
    hfModel: 'openai/whisper-large-v3-turbo',
    visualizer: 'pill',
    design: 'default',
    overlayPosition: 'top',
    saveAudio: true,
    audioRetentionDays: 7,
    selectedAudioDeviceId: '',
    hfHasUserToken: false,
    hfHasInstallToken: false,
    hfTokenSource: 'none',
    activeProfiles: {},
  } satisfies SpeechKitSettingsState,
  fetchSettingsState: fetchSettingsStateMock,
  fetchModelProfiles: fetchModelProfilesMock,
  saveSettingsState: saveSettingsStateMock,
  saveHuggingFaceToken: saveHuggingFaceTokenMock,
  clearHuggingFaceToken: clearHuggingFaceTokenMock,
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
  hfEnabled: false,
  hotkey: 'win+alt',
  dictateHotkey: 'win+alt',
  agentHotkey: 'ctrl+shift+k',
  activeMode: 'dictate',
  hfModel: 'openai/whisper-large-v3-turbo',
  visualizer: 'pill',
  design: 'default',
  overlayPosition: 'top',
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
}

describe('SettingsApp', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    fetchSettingsStateMock.mockReset()
    fetchModelProfilesMock.mockReset()
    saveSettingsStateMock.mockReset()
    saveHuggingFaceTokenMock.mockReset()
    clearHuggingFaceTokenMock.mockReset()
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
    saveHuggingFaceTokenMock.mockResolvedValue('Token gespeichert')
    clearHuggingFaceTokenMock.mockResolvedValue('Token entfernt')
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

    const modelSelect = await screen.findByLabelText('Model')
    expect(modelSelect).toBeEnabled()
  })

  it('saves model changes even while hugging face inference is off', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: 'openai/whisper-large-v3',
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    const modelSelect = await screen.findByLabelText('Model')
    fireEvent.change(modelSelect, {
      target: { value: 'openai/whisper-large-v3-turbo' },
    })

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          hfEnabled: false,
          hfModel: 'openai/whisper-large-v3-turbo',
        }),
      ),
    )
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

  it('shows stored token status for the provider', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfHasInstallToken: true,
      hfTokenSource: 'install',
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    expect(await screen.findByText(/install token active/i)).toBeInTheDocument()
  })

  it('saves a user hugging face token explicitly', async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings)

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))

    fireEvent.change(await screen.findByLabelText('Hugging Face token'), {
      target: { value: 'hf_user_token_123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save token' }))

    await waitFor(() =>
      expect(saveHuggingFaceTokenMock).toHaveBeenCalledWith('hf_user_token_123'),
    )
  })

  it('clears a stored user token explicitly', async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfHasUserToken: true,
      hfTokenSource: 'user',
    })

    render(<SettingsApp />)

    fireEvent.click(await screen.findByRole('button', { name: 'Provider' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Clear token' }))

    await waitFor(() => expect(clearHuggingFaceTokenMock).toHaveBeenCalled())
  })
})
