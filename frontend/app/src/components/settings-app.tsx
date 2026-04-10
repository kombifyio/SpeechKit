import { useEffect, useRef, useState } from 'react'

import { MicSelector } from '@/components/ui/mic-selector'
import {
  cancelModelDownload,
  clearProviderCredential,
  defaultSettingsState,
  fetchDownloadCatalog,
  fetchDownloadJobs,
  fetchModelProfiles,
  fetchSettingsState,
  saveProviderCredential,
  saveSettingsState,
  startModelDownload,
  testProviderCredential,
  type DownloadItem,
  type DownloadJob,
  type ProviderCredentialState,
  type SpeechKitSettingsState,
} from '@/lib/speechkit'

type Tab = 'general' | 'provider'

const CTRL_SHIFT_SUFFIX_KEYS = ['d', 'j', 'k', 'space'] as const

export function SettingsApp() {
  const [settings, setSettings] = useState(defaultSettingsState)
  const [providerTokens, setProviderTokens] = useState<Record<string, string>>({})
  const [providerBusy, setProviderBusy] = useState<Record<string, boolean>>({})
  const [loaded, setLoaded] = useState(false)
  const [toast, setToast] = useState('')
  const [tab, setTab] = useState<Tab>('general')
  const saveTimer = useRef<number | null>(null)
  const toastTimer = useRef<number | null>(null)

  const loadSettings = async () => {
    const [state, profiles] = await Promise.all([
      fetchSettingsState(),
      fetchModelProfiles().catch(() => []),
    ])

    setSettings({
      ...state,
      profiles: state.profiles?.length ? state.profiles : profiles,
    })
  }

  useEffect(() => {
    let active = true

    void loadSettings().then(() => {
      if (!active) return
      setLoaded(true)
    }).catch(() => {
      if (!active) return
      setLoaded(true)
    })

    return () => {
      active = false
      if (saveTimer.current) window.clearTimeout(saveTimer.current)
      if (toastTimer.current) window.clearTimeout(toastTimer.current)
    }
  }, [])

  const showToast = (message: string) => {
    if (toastTimer.current) window.clearTimeout(toastTimer.current)
    setToast(message)
    toastTimer.current = window.setTimeout(() => setToast(''), 1400)
  }

  const queueSave = (next: SpeechKitSettingsState, delay: number) => {
    setSettings(next)
    if (!loaded) return
    if (saveTimer.current) window.clearTimeout(saveTimer.current)
    const waitingForPostgresDSN =
      next.storeBackend === 'postgres' &&
      !next.postgresConfigured &&
      next.postgresDSN.trim().length === 0
    if (waitingForPostgresDSN) {
      return
    }
    saveTimer.current = window.setTimeout(async () => {
      try {
        const message = await saveSettingsState(next)
        showToast(message || 'Saved')
      } catch {
        showToast('Save failed')
      }
    }, delay)
  }

  const updateSettings = (patch: Partial<SpeechKitSettingsState>, delay = 0) => {
    queueSave({ ...settings, ...patch }, delay)
  }

  const tokenStatusLabel = (cred: ProviderCredentialState) => {
    switch (cred.source) {
      case 'user': return 'User key active'
      case 'install': return 'Install key active'
      case 'env': return 'Environment key active'
      default: return 'No key configured'
    }
  }

  const postgresReady =
    settings.postgresConfigured || settings.postgresDSN.trim().length > 0

  const handleSaveProviderCredential = async (provider: string) => {
    const token = (providerTokens[provider] ?? '').trim()
    if (!token) { showToast('API key required'); return }
    setProviderBusy((b) => ({ ...b, [provider]: true }))
    try {
      const result = await saveProviderCredential(provider, token)
      setProviderTokens((t) => ({ ...t, [provider]: '' }))
      showToast(result.message ?? 'Saved')
      const fresh = await fetchSettingsState()
      setSettings((prev) => ({ ...prev, providerCredentials: fresh.providerCredentials }))
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Save failed')
    } finally {
      setProviderBusy((b) => ({ ...b, [provider]: false }))
    }
  }

  const handleClearProviderCredential = async (provider: string) => {
    setProviderBusy((b) => ({ ...b, [provider]: true }))
    try {
      const result = await clearProviderCredential(provider)
      setProviderTokens((t) => ({ ...t, [provider]: '' }))
      showToast(result.message ?? 'Cleared')
      const fresh = await fetchSettingsState()
      setSettings((prev) => ({ ...prev, providerCredentials: fresh.providerCredentials }))
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Clear failed')
    } finally {
      setProviderBusy((b) => ({ ...b, [provider]: false }))
    }
  }

  const handleTestProviderCredential = async (provider: string) => {
    const token = (providerTokens[provider] ?? '').trim()
    const storedCredential = settings.providerCredentials?.[provider]
    if (!token && !storedCredential?.available) {
      showToast('No key configured')
      return
    }
    setProviderBusy((b) => ({ ...b, [provider]: true }))
    try {
      const result = await testProviderCredential(provider, token)
      showToast(result.message ?? 'Key valid')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Test failed')
    } finally {
      setProviderBusy((b) => ({ ...b, [provider]: false }))
    }
  }

  return (
    <div className="min-h-screen bg-[#0b0f14] px-5 py-5 text-[13px] text-white/90">
      <div className="mx-auto max-w-[560px]">
        <h1 className="text-base font-semibold tracking-tight">
          SpeechKit Settings
        </h1>

        <div className="mt-3 flex gap-px rounded-lg bg-white/5 p-0.5">
          <TabBtn active={tab === 'general'} onClick={() => setTab('general')}>
            General
          </TabBtn>
          <TabBtn
            active={tab === 'provider'}
            onClick={() => setTab('provider')}
          >
            Provider
          </TabBtn>
        </div>

        {tab === 'general' && (
          <div className="mt-4 flex flex-col gap-5">
            <Section title="Mode">
              <div className="flex flex-wrap gap-1.5">
                <Chip
                  active={settings.activeMode === 'dictate'}
                  onClick={() => updateSettings({ activeMode: 'dictate' })}
                >
                  Dictate
                </Chip>
                <Chip
                  active={settings.activeMode === 'agent'}
                  onClick={() => updateSettings({ activeMode: 'agent' })}
                >
                  Agent
                </Chip>
              </div>
              <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                The active mode decides which action wins when hotkeys overlap.
              </p>
            </Section>

            <Section title="Hotkeys">
              <HotkeyPicker
                label="Dictate hotkey"
                value={settings.dictateHotkey}
                onChange={(value) =>
                  updateSettings({ dictateHotkey: value, hotkey: value })
                }
              />
              <div className="mt-3" />
              <HotkeyPicker
                label="Agent hotkey"
                value={settings.agentHotkey}
                onChange={(value) => updateSettings({ agentHotkey: value })}
              />
            </Section>

            <Section title="Microphone">
              <MicSelector
                value={settings.selectedAudioDeviceId}
                onValueChange={(deviceId) =>
                  updateSettings({ selectedAudioDeviceId: deviceId })
                }
                className="w-full"
              />
              <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                The desktop backend applies the selected device immediately; no restart flow is needed.
              </p>
            </Section>

            <Section title="Vocabulary">
              <label
                htmlFor="vocabulary-dictionary-input"
                className="block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
              >
                Vocabulary dictionary
              </label>
              <textarea
                id="vocabulary-dictionary-input"
                aria-label="Vocabulary dictionary"
                value={settings.vocabularyDictionary}
                onChange={(e) =>
                  updateSettings({ vocabularyDictionary: e.target.value }, 250)
                }
                rows={5}
                placeholder={'kombi fire => Kombify\nAcmeOS\nGemma'}
                className="mt-2 w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-xs leading-6 outline-none focus:border-orange-400/50"
              />
              <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                Add one term per line. Use <code>spoken =&gt; canonical</code> for corrections, or a single term to bias transcription hints for names and product words.
              </p>
            </Section>

            <Section title="Overlay">
              <Row
                label="Show overlay"
                on={settings.overlayEnabled}
                onToggle={() =>
                  updateSettings({ overlayEnabled: !settings.overlayEnabled })
                }
              />
              {settings.overlayEnabled && (
                <>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <Chip
                      active={settings.visualizer === 'pill'}
                      onClick={() => updateSettings({ visualizer: 'pill' })}
                    >
                      Default <span className="ml-1 text-[10px] opacity-50">(Pill)</span>
                    </Chip>
                    <Chip
                      active={settings.visualizer === 'circle'}
                      onClick={() => updateSettings({ visualizer: 'circle' })}
                    >
                      Focus <span className="ml-1 text-[10px] opacity-50">(Dot)</span>
                    </Chip>
                    {settings.visualizer === 'pill' && (
                      <>
                        <span className="mx-1 self-center text-white/20">|</span>
                        <Chip
                          active={settings.design === 'default'}
                          onClick={() => updateSettings({ design: 'default' })}
                        >
                          Default
                        </Chip>
                        <Chip
                          active={settings.design === 'kombify'}
                          onClick={() => updateSettings({ design: 'kombify' })}
                        >
                          kombify
                        </Chip>
                      </>
                    )}
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <span className="mr-1 self-center text-[11px] text-white/35">
                      Position
                    </span>
                    {(['top', 'bottom', 'left', 'right'] as const).map((pos) => (
                      <Chip
                        key={pos}
                        active={settings.overlayPosition === pos}
                        onClick={() => updateSettings({ overlayPosition: pos })}
                      >
                        {pos.charAt(0).toUpperCase() + pos.slice(1)}
                      </Chip>
                    ))}
                  </div>
                  <div className="mt-2">
                    <Row
                      label="Movable overlay"
                      on={settings.overlayMovable}
                      onToggle={() =>
                        updateSettings({ overlayMovable: !settings.overlayMovable })
                      }
                    />
                  </div>
                  {settings.overlayMovable && settings.visualizer === 'pill' && (
                    <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                      Drag the center bubble inside the pill panel to place it anywhere on the desktop.
                    </p>
                  )}
                </>
              )}
            </Section>

            <Section title="Storage">
              <div>
                <div className="mb-1.5 text-xs font-medium text-white/65">Backend</div>
                <div className="flex flex-wrap gap-1.5">
                  <Chip
                    active={settings.storeBackend === 'sqlite'}
                    onClick={() => updateSettings({ storeBackend: 'sqlite' })}
                  >
                    SQLite
                  </Chip>
                  <Chip
                    active={settings.storeBackend === 'postgres'}
                    onClick={() => updateSettings({ storeBackend: 'postgres' })}
                  >
                    PostgreSQL
                  </Chip>
                </div>
              </div>
              <p
                className={[
                  'mt-2 rounded-lg border px-3 py-2 text-xs leading-relaxed',
                  settings.storeBackend === 'postgres' && !postgresReady
                    ? 'border-orange-500/20 bg-orange-500/5 text-orange-200/75'
                    : 'border-white/10 bg-white/[0.03] text-white/50',
                ].join(' ')}
              >
                {settings.storeBackend === 'sqlite'
                  ? 'SQLite keeps metadata in the local SpeechKit app data folder.'
                  : postgresReady
                    ? 'PostgreSQL metadata backend is configured. Restart the app after changes.'
                    : 'Add a PostgreSQL connection string before switching the metadata backend.'}
              </p>
              {settings.storeBackend === 'sqlite' ? (
                <>
                  <label
                    htmlFor="sqlite-path-input"
                    className="mt-2 block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
                  >
                    SQLite path
                  </label>
                  <input
                    id="sqlite-path-input"
                    aria-label="SQLite path"
                    value={settings.sqlitePath}
                    onChange={(e) =>
                      updateSettings({ sqlitePath: e.target.value }, 350)
                    }
                    placeholder="%APPDATA%/SpeechKit/feedback.db"
                    className="mt-2 h-9 w-full rounded-lg border border-white/10 bg-white/5 px-3 text-xs outline-none focus:border-orange-400/50"
                  />
                </>
              ) : (
                <>
                  <label
                    htmlFor="postgres-dsn-input"
                    className="mt-2 block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
                  >
                    PostgreSQL connection string
                  </label>
                  <input
                    id="postgres-dsn-input"
                    aria-label="PostgreSQL connection string"
                    value={settings.postgresDSN}
                    onChange={(e) =>
                      updateSettings({ postgresDSN: e.target.value }, 350)
                    }
                    placeholder="postgres://user:password@host:5432/speechkit?sslmode=disable"
                    className="mt-2 h-9 w-full rounded-lg border border-white/10 bg-white/5 px-3 text-xs outline-none focus:border-orange-400/50"
                  />
                  <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                    The desktop host stores the connection string in the local config.
                    Switching to PostgreSQL changes the backend on the next app start.
                  </p>
                </>
              )}
              <Row
                label="Save raw audio locally"
                on={settings.saveAudio}
                onToggle={() => updateSettings({ saveAudio: !settings.saveAudio })}
              />
              <label
                htmlFor="audio-retention-select"
                className="mt-2 block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
              >
                Audio retention
              </label>
              <select
                id="audio-retention-select"
                aria-label="Audio retention"
                value={String(settings.audioRetentionDays)}
                onChange={(e) =>
                  updateSettings({ audioRetentionDays: Number(e.target.value) })
                }
                className="mt-2 h-9 w-full rounded-lg border border-white/10 bg-white/5 px-3 text-xs outline-none focus:border-orange-400/50"
              >
                <option value="0">No automatic deletion</option>
                <option value="1">1 day</option>
                <option value="7">7 days</option>
                <option value="30">30 days</option>
                <option value="90">90 days</option>
              </select>
              <label
                htmlFor="max-audio-storage-input"
                className="mt-2 block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
              >
                Max local audio storage (MB)
              </label>
              <input
                id="max-audio-storage-input"
                aria-label="Max local audio storage (MB)"
                type="number"
                min="0"
                value={String(settings.maxAudioStorageMB)}
                onChange={(e) => {
                  const nextValue = Number.parseInt(e.target.value, 10)
                  if (Number.isNaN(nextValue) || nextValue < 0) {
                    return
                  }
                  updateSettings({ maxAudioStorageMB: nextValue }, 250)
                }}
                className="mt-2 h-9 w-full rounded-lg border border-white/10 bg-white/5 px-3 text-xs outline-none focus:border-orange-400/50"
              />
              <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                {settings.storeBackend === 'sqlite'
                  ? 'SQLite keeps metadata locally. Audio stays as raw WAV files on disk and can be turned off or pruned automatically.'
                  : 'PostgreSQL is configured as the metadata backend. Raw audio retention still applies locally until remote object storage is introduced.'}
              </p>
            </Section>
          </div>
        )}

        {tab === 'provider' && (
          <ProviderTab
            settings={settings}
            setSettings={setSettings}
            showToast={showToast}
            providerTokens={providerTokens}
            setProviderTokens={setProviderTokens}
            providerBusy={providerBusy}
            tokenStatusLabel={tokenStatusLabel}
            onSaveCredential={handleSaveProviderCredential}
            onClearCredential={handleClearProviderCredential}
            onTestCredential={handleTestProviderCredential}
          />
        )}
      </div>

      <div
        className={[
          'pointer-events-none fixed top-4 right-4 rounded-lg border border-emerald-400/20 bg-emerald-500/10 px-3 py-1.5 text-xs text-emerald-200 transition-all',
          toast ? 'translate-y-0 opacity-100' : '-translate-y-2 opacity-0',
        ].join(' ')}
      >
        {toast || '\u00A0'}
      </div>
    </div>
  )
}

function TabBtn({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'flex-1 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
        active
          ? 'bg-white/10 text-white'
          : 'text-white/40 hover:text-white/60',
      ].join(' ')}
    >
      {children}
    </button>
  )
}

function Section({
  title,
  badge,
  children,
}: {
  title: string
  badge?: string
  children: React.ReactNode
}) {
  return (
    <section>
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35">
          {title}
        </span>
        {badge && (
          <span className="rounded bg-white/6 px-1.5 py-0.5 text-[10px] text-white/25">
            {badge}
          </span>
        )}
      </div>
      {children}
    </section>
  )
}

function HotkeyPicker({
  label,
  value,
  onChange,
}: {
  label: string
  value: string
  onChange: (value: string) => void
}) {
  const isCtrlShift = value.startsWith('ctrl+shift+')
  const ctrlShiftSuffix = isCtrlShift ? value.replace('ctrl+shift+', '') : 'd'

  return (
    <div>
      <div className="mb-1.5 text-xs font-medium text-white/65">{label}</div>
      <div className="flex flex-wrap gap-1.5">
        <Chip active={value === 'win+alt'} onClick={() => onChange('win+alt')}>
          Win + Alt
        </Chip>
        <Chip
          active={value === 'ctrl+win'}
          onClick={() => onChange('ctrl+win')}
        >
          Ctrl + Win
        </Chip>
        <span className="flex items-center gap-0">
          <Chip
            active={isCtrlShift}
            onClick={() => onChange(`ctrl+shift+${ctrlShiftSuffix}`)}
            className={isCtrlShift ? 'rounded-r-none' : ''}
          >
            Ctrl + Shift +
          </Chip>
          <select
            value={ctrlShiftSuffix}
            onChange={(e) => onChange(`ctrl+shift+${e.target.value}`)}
            className={[
              'h-8 rounded-r-lg border-y border-r px-2 text-xs font-medium uppercase outline-none',
              isCtrlShift
                ? 'border-orange-500/60 bg-orange-500/20 text-orange-200'
                : 'border-white/12 bg-white/5 text-white/50',
            ].join(' ')}
          >
            {CTRL_SHIFT_SUFFIX_KEYS.map((k) => (
              <option key={k} value={k}>
                {k === 'space' ? 'Space' : k.toUpperCase()}
              </option>
            ))}
          </select>
        </span>
      </div>
    </div>
  )
}

const PROVIDER_FOR_EXECUTION_MODE: Record<string, string | undefined> = {
  hf_routed: 'huggingface',
  openai_api: 'openai',
  groq_api: 'groq',
  google_api: 'google',
  openrouter_api: 'openrouter',
}

const MODALITY_LABELS = {
  stt: 'STT',
  utility: 'Utility',
  agent: 'Agent',
  realtime_voice: 'Voice Agent',
} as const

type ModalityTabKey = keyof typeof MODALITY_LABELS

const MODALITY_ORDER: ModalityTabKey[] = ['stt', 'utility', 'agent', 'realtime_voice']

function limitProfilesToVisibleOptions(
  profiles: SpeechKitSettingsState['profiles'],
  activeProfileID?: string,
  maxOptions = 4,
) {
  const list = profiles ?? []
  if (list.length <= maxOptions) {
    return list
  }

  const next = list.slice(0, maxOptions)
  if (!activeProfileID || next.some((profile) => profile.id === activeProfileID)) {
    return next
  }

  const activeProfile = list.find((profile) => profile.id === activeProfileID)
  if (!activeProfile) {
    return next
  }

  return [...next.slice(0, maxOptions - 1), activeProfile]
}

function sourceBadge(profile: NonNullable<SpeechKitSettingsState['profiles']>[number]) {
  switch (profile.executionMode) {
    case 'local':
    case 'ollama_local':
      return {
        label: 'local',
        className: 'bg-emerald-500/15 text-emerald-300/80',
      }
    case 'hf_routed':
      return {
        label: 'managed',
        className: 'bg-sky-500/15 text-sky-300/80',
      }
    default:
      return {
        label: 'api key',
        className: 'bg-amber-500/15 text-amber-300/70',
      }
  }
}

function pickPrimaryDownloadItem(items: DownloadItem[]) {
  return (
    items.find((item) => item.available && item.recommended) ??
    items.find((item) => item.available) ??
    items.find((item) => item.recommended) ??
    items[0] ??
    null
  )
}

function ProviderTab({
  settings,
  setSettings,
  showToast,
  providerTokens,
  setProviderTokens,
  providerBusy,
  tokenStatusLabel,
  onSaveCredential,
  onClearCredential,
  onTestCredential,
}: {
  settings: SpeechKitSettingsState
  setSettings: React.Dispatch<React.SetStateAction<SpeechKitSettingsState>>
  showToast: (msg: string) => void
  providerTokens: Record<string, string>
  setProviderTokens: React.Dispatch<React.SetStateAction<Record<string, string>>>
  providerBusy: Record<string, boolean>
  tokenStatusLabel: (cred: ProviderCredentialState) => string
  onSaveCredential: (provider: string) => void
  onClearCredential: (provider: string) => void
  onTestCredential: (provider: string) => void
}) {
  const [modalityTab, setModalityTab] = useState<ModalityTabKey>('stt')
  const [dlCatalog, setDlCatalog] = useState<DownloadItem[]>([])
  const [dlJobs, setDlJobs] = useState<DownloadJob[]>([])
  const [confirmItem, setConfirmItem] = useState<DownloadItem | null>(null)
  const [dlBusy, setDlBusy] = useState(false)

  // Fetch download state once on mount.
  useEffect(() => {
    fetchDownloadCatalog().then(setDlCatalog).catch(() => {})
    fetchDownloadJobs().then(setDlJobs).catch(() => {})
  }, [])

  // Poll jobs every 2 seconds while any download is active; re-fetch catalog when done.
  useEffect(() => {
    const hasActive = dlJobs.some((j) => j.status === 'pending' || j.status === 'running')
    if (!hasActive) return
    const timer = setInterval(() => {
      fetchDownloadJobs()
        .then((jobs) => {
          setDlJobs(jobs)
          const wasRunning = dlJobs.some((j) => j.status === 'running' || j.status === 'pending')
          const nowDone = jobs.every((j) => j.status === 'done' || j.status === 'failed' || j.status === 'cancelled')
          if (wasRunning && nowDone) {
            fetchDownloadCatalog().then(setDlCatalog).catch(() => {})
          }
        })
        .catch(() => {})
    }, 2000)
    return () => clearInterval(timer)
  }, [dlJobs])

  const profiles = settings.profiles ?? []
  const availableTabs = MODALITY_ORDER
    .filter((key) => profiles.some((profile) => profile.modality === key))
    .map((key) => ({ key, label: MODALITY_LABELS[key] }))
  const selectedTab = availableTabs.some((tab) => tab.key === modalityTab)
    ? modalityTab
    : availableTabs[0]?.key
  const filtered = selectedTab ? profiles.filter((p) => p.modality === selectedTab) : []
  const activeId = selectedTab ? settings.activeProfiles?.[selectedTab] : undefined
  const visibleProfiles = limitProfilesToVisibleOptions(filtered, activeId)
  const visibleProviderKeys = new Set(
    visibleProfiles
      .map((profile) => (profile.executionMode ? PROVIDER_FOR_EXECUTION_MODE[profile.executionMode] : undefined))
      .filter((provider): provider is string => Boolean(provider)),
  )
  const visibleCredentials = settings.providerCredentials
    ? Object.values(settings.providerCredentials).filter((credential) => visibleProviderKeys.has(credential.provider))
    : []

  return (
  <>
    <div className="mt-4 flex flex-col gap-4">
      <Section title="Model setup">
        {availableTabs.length > 0 && (
          <div className="flex gap-px rounded-lg bg-white/5 p-0.5">
            {availableTabs.map((mt) => (
              <button
                key={mt.key}
                type="button"
                onClick={() => setModalityTab(mt.key)}
                  className={[
                    'flex-1 rounded-md px-2 py-1 text-[11px] font-medium transition-colors',
                    selectedTab === mt.key
                      ? 'bg-white/10 text-white'
                      : 'text-white/35 hover:text-white/55',
                  ].join(' ')}
              >
                {mt.label}
              </button>
            ))}
          </div>
        )}

        {visibleProfiles.length === 0 ? (
          <p className="mt-3 text-xs text-white/30">No live-switchable model profiles are exposed in this build.</p>
        ) : (
          <div className="mt-3 grid gap-2">
            {visibleProfiles.map((profile) => {
              const isActive = activeId === profile.id
              const badge = sourceBadge(profile)
              const providerKey = profile.executionMode ? PROVIDER_FOR_EXECUTION_MODE[profile.executionMode] : undefined
              const providerCredential = providerKey ? settings.providerCredentials?.[providerKey] : undefined
              const providerMissing = Boolean(providerKey && !providerCredential?.available)
              const downloadItems = dlCatalog.filter((item) => item.profileId === profile.id)
              const primaryDownload = pickPrimaryDownloadItem(downloadItems)
              const downloadJob = primaryDownload
                ? dlJobs.find((job) => job.modelId === primaryDownload.id)
                : undefined
              const downloadActive = downloadJob?.status === 'pending' || downloadJob?.status === 'running'
              const downloadReady = Boolean(primaryDownload?.available || downloadJob?.status === 'done')
              const needsDownload = Boolean(primaryDownload && !downloadReady && !downloadActive)
              const readyToUse = !providerMissing && !needsDownload && !downloadActive
              const statusLabel = isActive
                ? 'Active'
                : providerMissing
                  ? `${providerCredential?.label ?? 'API'} key needed`
                  : downloadActive
                    ? 'Downloading'
                    : needsDownload
                      ? 'Download required'
                      : 'Ready'
              const statusClassName = isActive
                ? 'border-orange-500/30 bg-orange-500/15 text-orange-200'
                : providerMissing
                  ? 'border-amber-500/20 bg-amber-500/10 text-amber-200/80'
                  : downloadActive
                    ? 'border-sky-500/20 bg-sky-500/10 text-sky-200/80'
                    : needsDownload
                      ? 'border-white/10 bg-white/5 text-white/60'
                      : 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200/80'

              return (
                <div
                  key={profile.id}
                  className={[
                    'rounded-xl border p-3 transition-colors',
                    isActive
                      ? 'border-orange-500/25 bg-orange-500/[0.07] text-orange-100'
                      : 'border-white/8 bg-white/[0.03] text-white/70',
                  ].join(' ')}
                >
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="truncate text-sm font-medium">{profile.name}</span>
                          <span className={['shrink-0 rounded px-1.5 py-px text-[9px]', badge.className].join(' ')}>
                            {badge.label}
                          </span>
                        </div>
                        <div className="mt-1 truncate text-[10px] uppercase tracking-[0.14em] text-white/25">
                          {profile.source ?? profile.executionMode ?? 'local'}
                        </div>
                      </div>
                      <span className={['shrink-0 rounded-full border px-2 py-0.5 text-[10px]', statusClassName].join(' ')}>
                        {statusLabel}
                      </span>
                    </div>
                    {profile.description && (
                      <div className="mt-2 text-[11px] leading-relaxed text-white/45">
                        {profile.description}
                      </div>
                    )}
                    {providerMissing && providerKey && providerCredential && (
                      <div className="mt-3 rounded-lg border border-amber-500/15 bg-amber-500/5 p-2.5">
                        <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-amber-200/70">
                          Add {providerCredential.label} key
                        </div>
                        <div className="mt-2 flex gap-2">
                          <input
                            aria-label={`${profile.name} API key`}
                            type="password"
                            value={providerTokens[providerKey] ?? ''}
                            onChange={(e) => setProviderTokens((tokens) => ({ ...tokens, [providerKey]: e.target.value }))}
                            placeholder={providerCredential.envName || 'API key'}
                            className="h-9 flex-1 rounded-lg border border-amber-500/15 bg-black/20 px-3 text-xs text-white/80 outline-none focus:border-orange-400/50"
                          />
                          <button
                            type="button"
                            onClick={() => onSaveCredential(providerKey)}
                            className={[
                              'shrink-0 rounded-lg border px-3 text-xs font-medium',
                              providerBusy[providerKey]
                                ? 'pointer-events-none border-white/10 bg-white/5 text-white/30'
                                : 'border-orange-500/25 bg-orange-500/15 text-orange-200 hover:bg-orange-500/25',
                            ].join(' ')}
                          >
                            Save key
                          </button>
                        </div>
                      </div>
                    )}

                    {primaryDownload && (
                      <div className="mt-3 rounded-lg border border-white/8 bg-black/10 p-2.5">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="text-[11px] text-white/70">{primaryDownload.name}</span>
                          {primaryDownload.recommended && (
                            <span className="rounded bg-orange-500/15 px-1.5 py-px text-[9px] text-orange-300/70">
                              recommended
                            </span>
                          )}
                          <span className="text-[10px] text-white/30">{primaryDownload.sizeLabel}</span>
                        </div>
                        <div className="mt-1 text-[10px] leading-relaxed text-white/35">
                          {primaryDownload.description}
                        </div>
                        {downloadActive && (
                          <div className="mt-2">
                            <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/10">
                              <div
                                className="h-full rounded-full bg-orange-400/60 transition-all duration-500"
                                style={{ width: `${Math.round((downloadJob?.progress ?? 0) * 100)}%` }}
                              />
                            </div>
                            <div className="mt-1 text-[10px] text-white/35">{downloadJob?.statusText}</div>
                          </div>
                        )}
                        {downloadJob?.status === 'failed' && (
                          <div className="mt-2 text-[10px] text-red-400/70">
                            {downloadJob.error ?? 'Download failed'}
                          </div>
                        )}
                        <div className="mt-2 flex items-center gap-2">
                          {downloadReady ? (
                            <span className="text-[10px] text-emerald-300/80">Ready on this device</span>
                          ) : downloadActive ? (
                            <button
                              type="button"
                              onClick={() => {
                                if (downloadJob) {
                                  cancelModelDownload(downloadJob.id).catch(() => {})
                                }
                              }}
                              className="rounded-lg border border-white/10 px-2.5 py-1 text-[10px] text-white/45 hover:border-red-400/25 hover:text-red-300/75"
                            >
                              Cancel download
                            </button>
                          ) : (
                            <button
                              type="button"
                              onClick={() => setConfirmItem(primaryDownload)}
                              className="rounded-lg border border-white/10 px-2.5 py-1 text-[10px] text-white/55 hover:border-orange-400/30 hover:text-orange-300/75"
                            >
                              Download
                            </button>
                          )}
                        </div>
                      </div>
                    )}

                    <div className="mt-3 flex items-center gap-2">
                      {isActive ? (
                        <span className="text-[11px] font-medium text-orange-300">Currently active</span>
                      ) : readyToUse ? (
                        <button
                          type="button"
                          onClick={async () => {
                            try {
                              const body = new URLSearchParams({
                                modality: profile.modality,
                                profile_id: profile.id,
                              })
                              const resp = await fetch('/models/profiles/activate', { method: 'POST', body })
                              if (!resp.ok) {
                                const err = await resp.text()
                                showToast(err || 'Switch failed')
                                return
                              }
                              setSettings((prev) => ({
                                ...prev,
                                activeProfiles: { ...prev.activeProfiles, [profile.modality]: profile.id },
                              }))
                              showToast(`${profile.name} activated`)
                            } catch {
                              showToast('Switch failed')
                            }
                          }}
                          className="rounded-lg border border-white/10 px-3 py-1.5 text-xs text-white/60 hover:border-white/20 hover:text-white/80"
                        >
                          Use model
                        </button>
                      ) : (
                        <span className="text-[10px] text-white/35">
                          {providerMissing
                            ? 'Add the key above to unlock this model.'
                            : needsDownload
                              ? 'Download the local runtime before switching.'
                              : 'Wait until the current download finishes.'}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </Section>

      {visibleCredentials.length > 0 && (
        <Section title="Connected providers">
          {visibleCredentials.map((cred) => {
            const busy = providerBusy[cred.provider] ?? false
            return (
              <div
                key={cred.provider}
                className="mt-2 flex items-center justify-between gap-3 rounded-lg border border-white/8 bg-white/[0.03] px-3 py-2 first:mt-0"
              >
                <div className="min-w-0">
                  <span className="text-xs text-white/60">{cred.label}</span>
                  <div className="mt-0.5 text-[10px] text-white/30">{tokenStatusLabel(cred)}</div>
                </div>
                <div className="flex shrink-0 gap-2">
                  {cred.available && (
                    <Chip
                      active={false}
                      onClick={() => { onTestCredential(cred.provider) }}
                      className={busy ? 'pointer-events-none opacity-60' : ''}
                    >
                      Test
                    </Chip>
                  )}
                  {cred.hasStoredSecret && (
                    <Chip
                      active={false}
                      onClick={() => { onClearCredential(cred.provider) }}
                      className={busy ? 'pointer-events-none opacity-60' : ''}
                    >
                      Clear
                    </Chip>
                  )}
                </div>
              </div>
            )
          })}
        </Section>
      )}

      <p className="text-[11px] leading-relaxed text-white/25">
        Only live-switchable model paths are shown here. Cloud models ask for the needed key directly on the card, and local models expose download actions in place.
      </p>
    </div>

    {/* ── Download confirmation modal ─────────────────────────────────────── */}
    {confirmItem && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
        <div className="w-80 rounded-xl border border-white/10 bg-[#0d1117] p-5 shadow-2xl">
          <h3 className="text-sm font-semibold text-white">Download Model</h3>
          <p className="mt-2 text-xs font-medium text-white/80">{confirmItem.name}</p>
          <p className="mt-1 text-[11px] leading-relaxed text-white/40">{confirmItem.description}</p>
          <div className="mt-3 flex flex-wrap gap-x-2 gap-y-0.5 text-[10px] text-white/30">
            <span>{confirmItem.sizeLabel}</span>
            <span>·</span>
            <span>License: {confirmItem.license}</span>
            {confirmItem.kind === 'ollama' && (
              <>
                <span>·</span>
                <span className="text-amber-300/50">Requires Ollama running locally</span>
              </>
            )}
          </div>
          <div className="mt-4 flex gap-2">
            <button
              type="button"
              disabled={dlBusy}
              onClick={async () => {
                setDlBusy(true)
                try {
                  const job = await startModelDownload(confirmItem.id)
                  setDlJobs((prev) => [...prev.filter((j) => j.modelId !== confirmItem.id), job])
                  setConfirmItem(null)
                } catch (e) {
                  showToast(e instanceof Error ? e.message : 'Download failed')
                } finally {
                  setDlBusy(false)
                }
              }}
              className="flex-1 rounded-lg bg-orange-500/20 py-1.5 text-xs font-medium text-orange-200 hover:bg-orange-500/30 disabled:opacity-50"
            >
              {dlBusy ? 'Starting…' : 'Download'}
            </button>
            <button
              type="button"
              onClick={() => setConfirmItem(null)}
              className="flex-1 rounded-lg border border-white/10 py-1.5 text-xs text-white/50 hover:border-white/20 hover:text-white/70"
            >
              Cancel
            </button>
          </div>
        </div>
      </div>
    )}
  </>
  )
}

function Chip({
  active,
  onClick,
  children,
  className = '',
}: {
  active: boolean
  onClick?: () => void
  children: React.ReactNode
  className?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'h-8 rounded-lg border px-3 text-xs font-medium transition-all',
        active
          ? 'border-orange-500/60 bg-orange-500/20 text-orange-200'
          : 'border-white/12 bg-white/5 text-white/50 hover:border-white/20 hover:text-white/70',
        className,
      ].join(' ')}
    >
      {children}
    </button>
  )
}

function Row({
  label,
  on,
  onToggle,
}: {
  label: string
  on: boolean
  onToggle: () => void
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-sm text-white/70">{label}</span>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={on}
        onClick={onToggle}
        className={[
          'relative inline-flex h-[22px] w-[38px] flex-shrink-0 cursor-pointer items-center rounded-full transition-colors',
          on ? 'bg-orange-500' : 'bg-white/15',
        ].join(' ')}
      >
        <span
          className={[
            'inline-block h-[16px] w-[16px] rounded-full bg-white shadow transition-transform',
            on ? 'translate-x-[19px]' : 'translate-x-[3px]',
          ].join(' ')}
        />
      </button>
    </div>
  )
}
