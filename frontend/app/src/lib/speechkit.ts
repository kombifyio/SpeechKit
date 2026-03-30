export type OverlayMode = 'pill' | 'circle'
export type OverlayDesign = 'default' | 'kombify'
export type RuntimeMode = 'dictate' | 'agent'
export type StoreBackend = 'sqlite' | 'postgres'
export type Modality = 'stt' | 'tts' | 'realtime_voice' | 'utility' | 'agent' | 'embedding' | 'reranker'
export type ExecutionMode = 'local' | 'self_hosted_http' | 'hf_routed' | 'hf_inference' | 'openai_api' | 'groq_api' | 'google_api' | 'ollama_local'
export type LogType = 'info' | 'warn' | 'error' | 'success'

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
  agentHotkey: string
  activeMode: RuntimeMode
  position: 'top' | 'bottom' | 'left' | 'right'
  lastTranscription: string
  quickNoteMode: boolean
  selectedAudioDeviceId: string
  activeProfiles: Partial<Record<Modality, string>>
}

export type SpeechKitSettingsState = {
  overlayEnabled: boolean
  storeBackend: StoreBackend
  sqlitePath: string
  postgresConfigured: boolean
  postgresDSN: string
  maxAudioStorageMB: number
  hfEnabled: boolean
  hfHasUserToken: boolean
  hfHasInstallToken: boolean
  hfTokenSource: 'none' | 'user' | 'install' | 'env'
  hotkey: string
  dictateHotkey: string
  agentHotkey: string
  activeMode: RuntimeMode
  hfModel: 'openai/whisper-large-v3-turbo' | 'openai/whisper-large-v3'
  visualizer: OverlayMode
  design: OverlayDesign
  overlayPosition: 'top' | 'bottom' | 'left' | 'right'
  saveAudio: boolean
  audioRetentionDays: number
  selectedAudioDeviceId: string
  profiles?: ModelProfile[]
  activeProfiles: Partial<Record<Modality, string>>
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
  agentHotkey: 'ctrl+shift+k',
  activeMode: 'dictate',
  position: 'top',
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
  hfEnabled: false,
  hfHasUserToken: false,
  hfHasInstallToken: false,
  hfTokenSource: 'none',
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
  activeProfiles: {},
}

function normalizeOverlayState(
  payload: Partial<SpeechKitOverlayState> | null | undefined,
): SpeechKitOverlayState {
  const base = { ...defaultOverlayState }
  const hotkey = payload?.hotkey ?? payload?.dictateHotkey ?? base.hotkey
  const dictateHotkey = payload?.dictateHotkey ?? hotkey

  return {
    ...base,
    ...(payload ?? {}),
    hotkey,
    dictateHotkey,
    agentHotkey: payload?.agentHotkey ?? base.agentHotkey,
    activeMode: payload?.activeMode ?? base.activeMode,
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
  const hotkey = payload?.hotkey ?? payload?.dictateHotkey ?? base.hotkey
  const dictateHotkey = payload?.dictateHotkey ?? hotkey
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
    agentHotkey: payload?.agentHotkey ?? base.agentHotkey,
    activeMode: payload?.activeMode ?? base.activeMode,
    selectedAudioDeviceId:
      payload?.selectedAudioDeviceId ??
      (payload as { audioDeviceId?: string } | undefined)?.audioDeviceId ??
      base.selectedAudioDeviceId,
    profiles: payload?.profiles ?? base.profiles,
    activeProfiles: payload?.activeProfiles ?? base.activeProfiles,
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
  const body = new URLSearchParams({
    overlay_enabled: nextState.overlayEnabled ? '1' : '0',
    hf_enabled: nextState.hfEnabled ? '1' : '0',
    overlay_visualizer: nextState.visualizer,
    overlay_design: nextState.design,
    hotkey: nextState.dictateHotkey ?? nextState.hotkey,
    dictate_hotkey: nextState.dictateHotkey ?? nextState.hotkey,
    agent_hotkey: nextState.agentHotkey,
    active_mode: nextState.activeMode,
    hf_model: nextState.hfModel,
    overlay_position: nextState.overlayPosition,
    store_backend: nextState.storeBackend,
    store_sqlite_path: nextState.sqlitePath,
    store_postgres_dsn: nextState.postgresDSN,
    store_save_audio: nextState.saveAudio ? '1' : '0',
    store_audio_retention_days: String(nextState.audioRetentionDays),
    store_max_audio_storage_mb: String(nextState.maxAudioStorageMB),
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

export async function saveHuggingFaceToken(token: string) {
  const body = new URLSearchParams({
    hf_token: token,
  })

  const response = await fetch('/settings/huggingface/token', {
    method: 'POST',
    body,
  })

  if (!response.ok) {
    throw new Error(`hugging face token save failed: ${response.status}`)
  }

  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}

export async function clearHuggingFaceToken() {
  const response = await fetch('/settings/huggingface/token/clear', {
    method: 'POST',
  })

  if (!response.ok) {
    throw new Error(`hugging face token clear failed: ${response.status}`)
  }

  const payload = (await response.json()) as { message?: string }
  return payload.message ?? ''
}
