export type OverlayMode = 'pill' | 'circle'
export type OverlayDesign = 'default' | 'kombify'
export type RuntimeMode = 'none' | 'dictate' | 'assist' | 'voice_agent'
export type AgentMode = 'assist' | 'voice_agent'
export type StoreBackend = 'sqlite' | 'postgres'
export type Modality = 'stt' | 'tts' | 'realtime_voice' | 'utility' | 'assist' | 'embedding' | 'reranker'
export type ExecutionMode = 'local' | 'self_hosted_http' | 'hf_routed' | 'hf_inference' | 'openai_api' | 'groq_api' | 'google_api' | 'ollama_local' | 'openrouter_api'
export type LogType = 'info' | 'warn' | 'error' | 'success'
export type AvailableModes = Record<'dictate' | 'assist' | 'voice_agent', boolean>

export type AudioDevice = {
  deviceId: string
  label: string
  groupId?: string
  isDefault?: boolean
}

export type AudioAsset = {
  storageKind: string
  mimeType?: string
  sizeBytes: number
  durationMs: number
}

export type ModelProfile = {
  id: string
  modality: Modality
  name: string
  executionMode?: ExecutionMode
  source?: string
  provider?: string
  description?: string
  experimental?: boolean
}

export type SpeechKitOverlayState = {
  state: 'idle' | 'recording' | 'processing' | 'done'
  phase: 'idle' | 'listening' | 'speaking' | 'thinking' | 'done'
  text: string
  level: number
  visible: boolean
  visualizer: OverlayMode
  design: OverlayDesign
  hotkey: string
  dictateHotkey: string
  assistHotkey: string
  voiceAgentHotkey: string
  agentHotkey: string
  activeMode: RuntimeMode
  availableModes: AvailableModes
  position: 'top' | 'bottom' | 'left' | 'right'
  movable: boolean
  positionFreeX: number
  positionFreeY: number
  lastTranscription: string
  quickNoteMode: boolean
  selectedAudioDeviceId: string
  activeProfiles: Partial<Record<Modality, string>>
}

export type ProviderCredentialState = {
  provider: string
  label: string
  envName: string
  available: boolean
  hasStoredSecret: boolean
  source: 'none' | 'user' | 'install' | 'env'
}

export type SpeechKitSettingsState = {
  overlayEnabled: boolean
  storeBackend: StoreBackend
  sqlitePath: string
  postgresConfigured: boolean
  postgresDSN: string
  maxAudioStorageMB: number
  hfAvailable: boolean
  hfEnabled: boolean
  hfHasUserToken: boolean
  hfHasInstallToken: boolean
  hfTokenSource: 'none' | 'user' | 'install' | 'env'
  hotkey: string
  dictateHotkey: string
  assistHotkey: string
  voiceAgentHotkey: string
  agentHotkey: string
  agentMode: AgentMode
  activeMode: RuntimeMode
  availableModes: AvailableModes
  hfModel: 'openai/whisper-large-v3-turbo' | 'openai/whisper-large-v3'
  visualizer: OverlayMode
  design: OverlayDesign
  overlayPosition: 'top' | 'bottom' | 'left' | 'right'
  overlayMovable: boolean
  overlayFreeX: number
  overlayFreeY: number
  vocabularyDictionary: string
  saveAudio: boolean
  audioRetentionDays: number
  selectedAudioDeviceId: string
  profiles?: ModelProfile[]
  activeProfiles: Partial<Record<Modality, string>>
  providerCredentials?: Record<string, ProviderCredentialState>
}

export const defaultOverlayState: SpeechKitOverlayState = {
  state: 'idle',
  phase: 'idle',
  text: '',
  level: 0,
  visible: true,
  visualizer: 'pill',
  design: 'default',
  hotkey: 'win+alt',
  dictateHotkey: 'win+alt',
  assistHotkey: 'ctrl+shift+j',
  voiceAgentHotkey: 'ctrl+shift+k',
  agentHotkey: 'ctrl+shift+j',
  activeMode: 'none',
  availableModes: {
    dictate: true,
    assist: true,
    voice_agent: true,
  },
  position: 'top',
  movable: false,
  positionFreeX: 0,
  positionFreeY: 0,
  lastTranscription: '',
  quickNoteMode: false,
  selectedAudioDeviceId: '',
  activeProfiles: {},
}

export const defaultSettingsState: SpeechKitSettingsState = {
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
}

function readStringField(
  payload: Record<string, unknown> | null | undefined,
  key: string,
): string | undefined {
  if (!payload || !(key in payload)) {
    return undefined
  }
  const value = payload[key]
  if (typeof value !== 'string') {
    return ''
  }
  return value.trim()
}

function normalizeAvailableModes(
  payload: Record<string, unknown> | null | undefined,
  hotkeys: AvailableModes,
): AvailableModes {
  const raw = payload?.availableModes
  if (!raw || typeof raw !== 'object') {
    return hotkeys
  }

  const map = raw as Partial<Record<keyof AvailableModes, unknown>>
  return {
    dictate: typeof map.dictate === 'boolean' ? map.dictate : hotkeys.dictate,
    assist: typeof map.assist === 'boolean' ? map.assist : hotkeys.assist,
    voice_agent:
      typeof map.voice_agent === 'boolean'
        ? map.voice_agent
        : hotkeys.voice_agent,
  }
}

function resolveAssistHotkey(
  payload: Record<string, unknown> | null | undefined,
  fallback: string,
): string {
  if (!payload) {
    return fallback
  }

  const explicit = readStringField(payload, 'assistHotkey')
  if (explicit !== undefined) {
    return explicit
  }

  const legacy = readStringField(payload, 'agentHotkey')
  const legacyMode = readStringField(payload, 'agentMode')
  if (legacy !== undefined && legacyMode !== 'voice_agent') {
    return legacy
  }

  return ''
}

function resolveVoiceAgentHotkey(
  payload: Record<string, unknown> | null | undefined,
  fallback: string,
): string {
  if (!payload) {
    return fallback
  }

  const explicit = readStringField(payload, 'voiceAgentHotkey')
  if (explicit !== undefined) {
    return explicit
  }

  const legacy = readStringField(payload, 'agentHotkey')
  const legacyMode = readStringField(payload, 'agentMode')
  if (legacy !== undefined && legacyMode === 'voice_agent') {
    return legacy
  }

  return ''
}

function normalizeRuntimeMode(
  rawMode: string | undefined,
  availableModes: AvailableModes,
  agentMode: AgentMode = 'assist',
): RuntimeMode {
  let mode: RuntimeMode
  switch (rawMode) {
    case 'dictate':
    case 'assist':
    case 'voice_agent':
    case 'none':
      mode = rawMode
      break
    case 'agent':
      mode = agentMode === 'voice_agent' ? 'voice_agent' : 'assist'
      break
    default:
      mode = 'none'
      break
  }

  if (mode === 'dictate' && !availableModes.dictate) {
    return 'none'
  }
  if (mode === 'assist' && !availableModes.assist) {
    return 'none'
  }
  if (mode === 'voice_agent' && !availableModes.voice_agent) {
    return 'none'
  }
  return mode
}

function deriveLegacyAgentMode(
  assistHotkey: string,
  voiceAgentHotkey: string,
  activeMode: RuntimeMode,
  fallback: AgentMode = 'assist',
): AgentMode {
  if (activeMode === 'voice_agent' && voiceAgentHotkey) {
    return 'voice_agent'
  }
  if (activeMode === 'assist' && assistHotkey) {
    return 'assist'
  }
  if (assistHotkey) {
    return 'assist'
  }
  if (voiceAgentHotkey) {
    return 'voice_agent'
  }
  return fallback === 'voice_agent' ? 'voice_agent' : 'assist'
}

function deriveLegacyAgentHotkey(
  assistHotkey: string,
  voiceAgentHotkey: string,
  activeMode: RuntimeMode,
): string {
  if (activeMode === 'voice_agent' && voiceAgentHotkey) {
    return voiceAgentHotkey
  }
  if (activeMode === 'assist' && assistHotkey) {
    return assistHotkey
  }
  return assistHotkey || voiceAgentHotkey
}

function normalizeOverlayState(
  payload: Partial<SpeechKitOverlayState> | null | undefined,
): SpeechKitOverlayState {
  const base = { ...defaultOverlayState }
  const record = (payload ?? null) as Record<string, unknown> | null
  const hotkey =
    readStringField(record, 'hotkey') ??
    readStringField(record, 'dictateHotkey') ??
    base.hotkey
  const dictateHotkey =
    readStringField(record, 'dictateHotkey') ??
    readStringField(record, 'hotkey') ??
    base.dictateHotkey
  const assistHotkey = resolveAssistHotkey(record, base.assistHotkey)
  const voiceAgentHotkey = resolveVoiceAgentHotkey(
    record,
    base.voiceAgentHotkey,
  )
  const availableModes = normalizeAvailableModes(record, {
    dictate: dictateHotkey !== '',
    assist: assistHotkey !== '',
    voice_agent: voiceAgentHotkey !== '',
  })
  const activeMode = normalizeRuntimeMode(
    readStringField(record, 'activeMode'),
    availableModes,
  )
  const agentHotkey =
    readStringField(record, 'agentHotkey') ??
    deriveLegacyAgentHotkey(assistHotkey, voiceAgentHotkey, activeMode)

  return {
    ...base,
    ...(payload ?? {}),
    hotkey,
    dictateHotkey,
    assistHotkey,
    voiceAgentHotkey,
    agentHotkey,
    activeMode,
    availableModes,
    selectedAudioDeviceId:
      payload?.selectedAudioDeviceId ??
      (payload as { audioDeviceId?: string } | undefined)?.audioDeviceId ??
      base.selectedAudioDeviceId,
    activeProfiles: payload?.activeProfiles ?? base.activeProfiles,
  }
}

function normalizeSettingsState(
  payload: Partial<SpeechKitSettingsState> | null | undefined,
): SpeechKitSettingsState {
  const base = { ...defaultSettingsState }
  const record = (payload ?? null) as Record<string, unknown> | null
  const hotkey =
    readStringField(record, 'hotkey') ??
    readStringField(record, 'dictateHotkey') ??
    base.hotkey
  const dictateHotkey =
    readStringField(record, 'dictateHotkey') ??
    readStringField(record, 'hotkey') ??
    base.dictateHotkey
  const assistHotkey = resolveAssistHotkey(record, base.assistHotkey)
  const voiceAgentHotkey = resolveVoiceAgentHotkey(
    record,
    base.voiceAgentHotkey,
  )
  const availableModes = normalizeAvailableModes(record, {
    dictate: dictateHotkey !== '',
    assist: assistHotkey !== '',
    voice_agent: voiceAgentHotkey !== '',
  })
  const agentMode =
    readStringField(record, 'agentMode') === 'voice_agent'
      ? 'voice_agent'
      : deriveLegacyAgentMode(
          assistHotkey,
          voiceAgentHotkey,
          'none',
          base.agentMode,
        )
  const activeMode = normalizeRuntimeMode(
    readStringField(record, 'activeMode'),
    availableModes,
    agentMode,
  )
  const agentHotkey =
    readStringField(record, 'agentHotkey') ??
    deriveLegacyAgentHotkey(assistHotkey, voiceAgentHotkey, activeMode)
  const storeBackend =
    payload?.storeBackend === 'postgres' ? 'postgres' : base.storeBackend

  return {
    ...base,
    ...(payload ?? {}),
    storeBackend,
    sqlitePath: payload?.sqlitePath ?? base.sqlitePath,
    postgresConfigured: payload?.postgresConfigured ?? base.postgresConfigured,
    postgresDSN: payload?.postgresDSN ?? base.postgresDSN,
    maxAudioStorageMB: payload?.maxAudioStorageMB ?? base.maxAudioStorageMB,
    hotkey,
    dictateHotkey,
    assistHotkey,
    voiceAgentHotkey,
    agentHotkey,
    agentMode,
    activeMode,
    availableModes,
    selectedAudioDeviceId:
      payload?.selectedAudioDeviceId ??
      (payload as { audioDeviceId?: string } | undefined)?.audioDeviceId ??
      base.selectedAudioDeviceId,
    vocabularyDictionary: payload?.vocabularyDictionary ?? base.vocabularyDictionary,
    profiles: payload?.profiles ?? base.profiles,
    activeProfiles: payload?.activeProfiles ?? base.activeProfiles,
    providerCredentials: payload?.providerCredentials ?? base.providerCredentials,
  }
}

export async function fetchOverlayState() {
  const response = await fetch('/overlay/state', { cache: 'no-store' })
  if (!response.ok) {
    throw new Error(`overlay state request failed: ${response.status}`)
  }
  return normalizeOverlayState(
    (await response.json()) as Partial<SpeechKitOverlayState>,
  )
}

export async function fetchSettingsState() {
  const response = await fetch('/settings/state', { cache: 'no-store' })
  if (!response.ok) {
    throw new Error(`settings state request failed: ${response.status}`)
  }
  return normalizeSettingsState(
    (await response.json()) as Partial<SpeechKitSettingsState>,
  )
}

export type AudioDevicesResponse = {
  devices: AudioDevice[]
  selectedDeviceId: string
}

export type AudioDeviceUpdateResponse = {
  message?: string
  selectedDeviceId?: string
}

export async function fetchAudioDevices(): Promise<AudioDevicesResponse> {
  const response = await fetch('/audio/devices', { cache: 'no-store' })
  if (!response.ok) {
    throw new Error(`audio devices request failed: ${response.status}`)
  }

  const payload = (await response.json()) as
    | AudioDevicesResponse
    | AudioDevice[]
    | {
        devices?: AudioDevice[]
        selectedDeviceId?: string
        selectedAudioDeviceId?: string
        deviceId?: string
        currentDeviceId?: string
      }

  if (Array.isArray(payload)) {
    return {
      devices: payload,
      selectedDeviceId:
        payload.find((device) => device.isDefault)?.deviceId ??
        payload[0]?.deviceId ??
        '',
    }
  }

  const normalizedPayload = payload as {
    devices?: AudioDevice[]
    selectedDeviceId?: string
    selectedAudioDeviceId?: string
    deviceId?: string
    currentDeviceId?: string
  }

  return {
    devices: normalizedPayload.devices ?? [],
    selectedDeviceId:
      normalizedPayload.selectedDeviceId ??
      normalizedPayload.selectedAudioDeviceId ??
      normalizedPayload.currentDeviceId ??
      normalizedPayload.deviceId ??
      '',
  }
}

export async function setAudioDevice(deviceId: string): Promise<string> {
  const body = new URLSearchParams({
    device_id: deviceId,
    audio_device_id: deviceId,
    selected_audio_device_id: deviceId,
  })
  const response = await fetch('/audio/device', {
    method: 'POST',
    body,
  })

  if (!response.ok) {
    throw new Error(`set audio device failed: ${response.status}`)
  }

  const payload = (await response.json()) as AudioDeviceUpdateResponse
  return payload.message ?? ''
}

export async function setActiveMode(mode: RuntimeMode): Promise<string> {
  const body = new URLSearchParams({ mode })
  const response = await fetch('/mode/active', {
    method: 'POST',
    body,
  })

  if (!response.ok) {
    throw new Error(`set mode failed: ${response.status}`)
  }

  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}

export async function fetchModelProfiles(): Promise<ModelProfile[]> {
  const response = await fetch('/models/profiles', { cache: 'no-store' })
  if (!response.ok) {
    throw new Error(`model profiles request failed: ${response.status}`)
  }

  const payload = (await response.json()) as
    | ModelProfile[]
    | { profiles?: ModelProfile[] }
  return Array.isArray(payload) ? payload : payload.profiles ?? []
}

export type TranscriptionRecord = {
  id: number
  text: string
  language: string
  provider: string
  model?: string
  durationMs?: number
  latencyMs: number
  audio?: AudioAsset
  createdAt: string
}

export type QuickNote = {
  id: number
  text: string
  language: string
  provider: string
  durationMs?: number
  latencyMs: number
  audio?: AudioAsset
  pinned: boolean
  createdAt: string
  updatedAt: string
}

export type DashboardStats = {
  transcriptions: number
  quickNotes: number
  totalWords: number
  totalAudioDurationMs: number
  averageWordsPerMinute: number
  averageLatencyMs: number
}

export async function fetchHistory(): Promise<TranscriptionRecord[]> {
  const response = await fetch('/dashboard/history', { cache: 'no-store' })
  if (!response.ok) throw new Error(`history: ${response.status}`)
  return (await response.json()) as TranscriptionRecord[]
}

export async function fetchDashboardStats(): Promise<DashboardStats> {
  const response = await fetch('/dashboard/stats', { cache: 'no-store' })
  if (!response.ok) throw new Error(`dashboard stats: ${response.status}`)
  return (await response.json()) as DashboardStats
}

export type AppVersionInfo = {
  version: string
  latestVersion?: string
  updateURL?: string
  downloadURL?: string
  downloadSizeBytes?: number
  assetName?: string
}

export type AppUpdateStatus = 'pending' | 'running' | 'done' | 'failed' | 'cancelled'

export type AppUpdateJob = {
  id: string
  version: string
  assetName: string
  status: AppUpdateStatus
  progress: number
  bytesDone: number
  totalBytes: number
  statusText: string
  filePath?: string
  error?: string
}

export async function fetchAppVersion(): Promise<AppVersionInfo> {
  const response = await fetch('/app/version', { cache: 'no-store' })
  if (!response.ok) throw new Error(`app version: ${response.status}`)
  return (await response.json()) as AppVersionInfo
}

export async function fetchAppUpdateJobs(): Promise<AppUpdateJob[]> {
  const response = await fetch('/app/update/jobs', { cache: 'no-store' })
  if (!response.ok) throw new Error(`app update jobs: ${response.status}`)
  return (await response.json()) as AppUpdateJob[]
}

export async function startAppUpdateDownload(version: string): Promise<AppUpdateJob> {
  const body = new URLSearchParams({ version })
  const response = await fetch('/app/update/download', { method: 'POST', body })
  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(errorText || `app update download: ${response.status}`)
  }
  return (await response.json()) as AppUpdateJob
}

export async function cancelAppUpdateDownload(jobId: string): Promise<void> {
  const body = new URLSearchParams({ job_id: jobId })
  const response = await fetch('/app/update/cancel', { method: 'POST', body })
  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(errorText || `app update cancel: ${response.status}`)
  }
}

export async function openAppUpdateInstaller(jobId: string): Promise<{ message?: string; filePath?: string }> {
  const body = new URLSearchParams({ job_id: jobId })
  const response = await fetch('/app/update/open', { method: 'POST', body })
  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(errorText || `app update open: ${response.status}`)
  }
  return (await response.json()) as { message?: string; filePath?: string }
}

export type LogEntry = {
  message: string
  type: LogType
  timestamp: string
}

export async function fetchLogs(): Promise<LogEntry[]> {
  const response = await fetch('/dashboard/logs', { cache: 'no-store' })
  if (!response.ok) throw new Error(`logs: ${response.status}`)
  return (await response.json()) as LogEntry[]
}

export async function fetchQuickNotes(): Promise<QuickNote[]> {
  const response = await fetch('/dashboard/quicknotes', { cache: 'no-store' })
  if (!response.ok) throw new Error(`quicknotes: ${response.status}`)
  return (await response.json()) as QuickNote[]
}

export async function createQuickNote(
  text: string,
): Promise<{ id: number; message: string }> {
  const body = new URLSearchParams({ text })
  const response = await fetch('/quicknotes/create', { method: 'POST', body })
  if (!response.ok) throw new Error(`create quicknote: ${response.status}`)
  return (await response.json()) as { id: number; message: string }
}

export async function updateQuickNote(id: number, text: string): Promise<string> {
  const body = new URLSearchParams({ id: String(id), text })
  const response = await fetch('/quicknotes/update', { method: 'POST', body })
  if (!response.ok) throw new Error(`update quicknote: ${response.status}`)
  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}

export async function pinQuickNote(
  id: number,
  pinned: boolean,
): Promise<string> {
  const body = new URLSearchParams({ id: String(id), pinned: pinned ? '1' : '0' })
  const response = await fetch('/quicknotes/pin', { method: 'POST', body })
  if (!response.ok) throw new Error(`pin quicknote: ${response.status}`)
  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}

export async function deleteQuickNote(id: number): Promise<string> {
  const body = new URLSearchParams({ id: String(id) })
  const response = await fetch('/quicknotes/delete', { method: 'POST', body })
  if (!response.ok) throw new Error(`delete quicknote: ${response.status}`)
  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}

export async function quickNoteSummary(id: number): Promise<string> {
  const body = new URLSearchParams({ id: String(id) })
  const response = await fetch('/quicknotes/summary', { method: 'POST', body })
  if (!response.ok) throw new Error(`quicknote summary: ${response.status}`)
  const payload = (await response.json()) as { summary: string }
  return payload.summary
}

export async function quickNoteEmail(id: number): Promise<string> {
  const body = new URLSearchParams({ id: String(id) })
  const response = await fetch('/quicknotes/email', { method: 'POST', body })
  if (!response.ok) throw new Error(`quicknote email: ${response.status}`)
  const payload = (await response.json()) as { email: string }
  return payload.email
}

export async function armQuickNoteRecording(noteId?: number): Promise<string> {
  const suffix = typeof noteId === 'number' && noteId > 0 ? `?id=${noteId}` : ''
  const response = await fetch(`/quicknotes/record-mode${suffix}`, {
    method: 'POST',
  })
  if (!response.ok) throw new Error(`arm quicknote: ${response.status}`)
  const payload = (await response.json()) as { message: string }
  return payload.message
}

export function dashboardAudioDownloadURL(
  kind: 'transcription' | 'quicknote',
  id: number,
) {
  return `/dashboard/audio?kind=${kind}&id=${id}`
}

export async function revealDashboardAudio(
  kind: 'transcription' | 'quicknote',
  id: number,
): Promise<string> {
  const body = new URLSearchParams({
    kind,
    id: String(id),
  })
  const response = await fetch('/dashboard/audio/reveal', {
    method: 'POST',
    body,
  })
  if (!response.ok) {
    throw new Error(`audio reveal: ${response.status}`)
  }
  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}

export async function saveSettingsState(nextState: SpeechKitSettingsState) {
  const legacyAgentMode = deriveLegacyAgentMode(
    nextState.assistHotkey,
    nextState.voiceAgentHotkey,
    nextState.activeMode,
    nextState.agentMode,
  )
  const legacyAgentHotkey = deriveLegacyAgentHotkey(
    nextState.assistHotkey,
    nextState.voiceAgentHotkey,
    nextState.activeMode,
  )
  const body = new URLSearchParams({
    overlay_enabled: nextState.overlayEnabled ? '1' : '0',
    overlay_visualizer: nextState.visualizer,
    overlay_design: nextState.design,
    hotkey: nextState.dictateHotkey ?? nextState.hotkey,
    dictate_hotkey: nextState.dictateHotkey ?? nextState.hotkey,
    assist_hotkey: nextState.assistHotkey,
    voice_agent_hotkey: nextState.voiceAgentHotkey,
    agent_hotkey: legacyAgentHotkey,
    agent_mode: legacyAgentMode,
    active_mode: nextState.activeMode,
    hf_model: nextState.hfModel,
    overlay_position: nextState.overlayPosition,
    overlay_movable: nextState.overlayMovable ? '1' : '0',
    overlay_free_x: String(nextState.overlayFreeX),
    overlay_free_y: String(nextState.overlayFreeY),
    store_backend: nextState.storeBackend,
    store_sqlite_path: nextState.sqlitePath,
    store_postgres_dsn: nextState.postgresDSN,
    store_save_audio: nextState.saveAudio ? '1' : '0',
    store_audio_retention_days: String(nextState.audioRetentionDays),
    store_max_audio_storage_mb: String(nextState.maxAudioStorageMB),
    vocabulary_dictionary: nextState.vocabularyDictionary,
    selected_audio_device_id: nextState.selectedAudioDeviceId,
    audio_device_id: nextState.selectedAudioDeviceId,
  })

  const response = await fetch('/settings/update', {
    method: 'POST',
    body,
  })

  if (!response.ok) {
    throw new Error(`settings update failed: ${response.status}`)
  }

  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}

export async function saveProviderCredential(provider: string, secret: string) {
  const body = new URLSearchParams({ provider, credential: secret })
  const response = await fetch('/settings/provider-credentials/save', { method: 'POST', body })
  if (!response.ok) throw new Error(`provider credential save failed: ${response.status}`)
  return (await response.json()) as { message?: string }
}

export async function clearProviderCredential(provider: string) {
  const body = new URLSearchParams({ provider })
  const response = await fetch('/settings/provider-credentials/clear', { method: 'POST', body })
  if (!response.ok) throw new Error(`provider credential clear failed: ${response.status}`)
  return (await response.json()) as { message?: string }
}

export async function testProviderCredential(provider: string, secret: string) {
  const body = new URLSearchParams({ provider, credential: secret })
  const response = await fetch('/settings/provider-credentials/test', { method: 'POST', body })
  if (!response.ok) throw new Error(`provider credential test failed: ${response.status}`)
  return (await response.json()) as { message?: string }
}

// ── Model Downloads ──────────────────────────────────────────────────────────

export type DownloadKind = 'http' | 'ollama'
export type DownloadStatus = 'pending' | 'running' | 'done' | 'failed' | 'cancelled'

export type DownloadItem = {
  id: string
  profileId: string
  name: string
  description: string
  sizeLabel: string
  sizeBytes: number
  kind: DownloadKind
  url?: string
  ollamaModel?: string
  license: string
  available: boolean
  selected: boolean
  recommended?: boolean
}

export type DownloadJob = {
  id: string
  modelId: string
  profileId: string
  status: DownloadStatus
  progress: number
  bytesDone: number
  totalBytes: number
  statusText: string
  error?: string
}

export async function fetchDownloadCatalog(): Promise<DownloadItem[]> {
  const resp = await fetch('/models/downloads/catalog')
  if (!resp.ok) throw new Error(`catalog fetch failed: ${resp.status}`)
  return resp.json() as Promise<DownloadItem[]>
}

export async function fetchDownloadJobs(): Promise<DownloadJob[]> {
  const resp = await fetch('/models/downloads/jobs')
  if (!resp.ok) throw new Error(`jobs fetch failed: ${resp.status}`)
  return resp.json() as Promise<DownloadJob[]>
}

export async function startModelDownload(modelId: string): Promise<DownloadJob> {
  const body = new URLSearchParams({ model_id: modelId })
  const resp = await fetch('/models/downloads/start', { method: 'POST', body })
  if (!resp.ok) {
    const err = await resp.text()
    throw new Error(err || `start download failed: ${resp.status}`)
  }
  return resp.json() as Promise<DownloadJob>
}

export async function selectDownloadedModel(modelId: string): Promise<{ message?: string }> {
  const body = new URLSearchParams({ model_id: modelId })
  const resp = await fetch('/models/downloads/select', { method: 'POST', body })
  if (!resp.ok) {
    const err = await resp.text()
    throw new Error(err || `select model failed: ${resp.status}`)
  }
  return resp.json() as Promise<{ message?: string }>
}

export async function cancelModelDownload(jobId: string): Promise<void> {
  const body = new URLSearchParams({ job_id: jobId })
  await fetch('/models/downloads/cancel', { method: 'POST', body })
}
