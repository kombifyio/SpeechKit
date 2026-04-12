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

type Tab = 'general' | 'stt' | 'assist' | 'realtime_voice'

const CTRL_SHIFT_SUFFIX_KEYS = ['d', 'j', 'k', 'space'] as const

function providerSecretNoun(provider?: string) {
  return provider === 'huggingface' ? 'token' : 'key'
}

function providerCredentialCopy(profileName: string, credential: ProviderCredentialState) {
  const noun = providerSecretNoun(credential.provider)
  const credentialLabel = `${credential.label} ${noun}`
  return {
    title: `Add ${credentialLabel}`,
    inputLabel: `${profileName} ${credentialLabel}`,
    placeholder: credential.envName || (noun === 'token' ? 'Token' : 'API key'),
    saveLabel: `Save ${noun}`,
    neededLabel: `${credentialLabel} needed`,
    unlockLabel: `Add the ${noun} above to unlock this model.`,
  }
}

export function SettingsApp() {
  const [settings, setSettings] = useState(defaultSettingsState)
  const [providerTokens, setProviderTokens] = useState<Record<string, string>>({})
  const [providerBusy, setProviderBusy] = useState<Record<string, boolean>>({})
  const [loaded, setLoaded] = useState(false)
  const [toast, setToast] = useState('')
  const [tab, setTab] = useState<Tab>('general')
  const [dlCatalog, setDlCatalog] = useState<DownloadItem[]>([])
  const [dlJobs, setDlJobs] = useState<DownloadJob[]>([])
  const [confirmItem, setConfirmItem] = useState<DownloadItem | null>(null)
  const [dlBusy, setDlBusy] = useState(false)
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
    fetchDownloadCatalog().then(setDlCatalog).catch(() => {})
    fetchDownloadJobs().then(setDlJobs).catch(() => {})
    return () => {
      active = false
      if (saveTimer.current) window.clearTimeout(saveTimer.current)
      if (toastTimer.current) window.clearTimeout(toastTimer.current)
    }
  }, [])

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
    if (waitingForPostgresDSN) return
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

  const applyFreshSettings = (fresh: SpeechKitSettingsState) => {
    setSettings((prev) => ({
      ...prev,
      ...fresh,
      profiles: fresh.profiles?.length ? fresh.profiles : prev.profiles,
    }))
  }

  const tokenStatusLabel = (cred: ProviderCredentialState) => {
    const noun = providerSecretNoun(cred.provider)
    switch (cred.source) {
      case 'user': return `User ${noun} active`
      case 'install': return `Install ${noun} active`
      case 'env': return `Environment ${noun} active`
      default: return `No ${noun} configured`
    }
  }

  const postgresReady =
    settings.postgresConfigured || settings.postgresDSN.trim().length > 0

  const handleSaveProviderCredential = async (provider: string) => {
    const token = (providerTokens[provider] ?? '').trim()
    const label = settings.providerCredentials?.[provider]?.label ?? 'API'
    const noun = providerSecretNoun(provider)
    if (!token) { showToast(`${label} ${noun} required`); return }
    setProviderBusy((b) => ({ ...b, [provider]: true }))
    try {
      const result = await saveProviderCredential(provider, token)
      setProviderTokens((t) => ({ ...t, [provider]: '' }))
      showToast(result.message ?? 'Saved')
      const fresh = await fetchSettingsState()
      applyFreshSettings(fresh)
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
      applyFreshSettings(fresh)
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
      showToast(`No ${providerSecretNoun(provider)} configured`)
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
    <div className="flex h-full bg-[#131318] text-[13px] text-[#e4e1e9]">
      {/* Settings sub-nav */}
      <div className="w-[180px] shrink-0 border-r border-[#35343a]/20 bg-[#1b1b20] py-6 px-3">
        <h2 className="px-3 mb-5 text-xs font-bold uppercase tracking-widest text-[#938ea1]">Settings</h2>
        <nav className="space-y-0.5">
          <TabBtn active={tab === 'general'} onClick={() => setTab('general')}>General</TabBtn>
          <TabBtn active={tab === 'stt'} onClick={() => setTab('stt')}>STT</TabBtn>
          <TabBtn active={tab === 'assist'} onClick={() => setTab('assist')}>Assist</TabBtn>
          <TabBtn active={tab === 'realtime_voice'} onClick={() => setTab('realtime_voice')}>Voice Agent</TabBtn>
        </nav>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-8 py-6">
        {/* General tab — two-column layout */}
        {tab === 'general' && (
          <div className="grid grid-cols-2 gap-x-10 gap-y-5">
            {/* Left column: Mode · Hotkeys · Microphone */}
            <div className="flex flex-col gap-5">
              <Section title="Mode">
                <div className="flex flex-wrap gap-1.5">
                  <Chip active={settings.activeMode === 'dictate'} onClick={() => updateSettings({ activeMode: 'dictate' })}>Dictate</Chip>
                  <Chip active={settings.activeMode === 'agent'} onClick={() => updateSettings({ activeMode: 'agent' })}>Assist</Chip>
                </div>
                <p className="mt-1 text-[11px] text-[#938ea1]/70">
                  Decides which action wins when hotkeys overlap.
                </p>
              </Section>

              <Section title="Hotkeys">
                <HotkeyPicker label="Dictate hotkey" value={settings.dictateHotkey} onChange={(value) => updateSettings({ dictateHotkey: value, hotkey: value })} />
                <div className="mt-3" />
                <HotkeyPicker label="Assist hotkey" value={settings.agentHotkey} onChange={(value) => updateSettings({ agentHotkey: value })} />
              </Section>

              <Section title="Microphone">
                <MicSelector
                  value={settings.selectedAudioDeviceId}
                  onValueChange={(deviceId) => updateSettings({ selectedAudioDeviceId: deviceId })}
                  className="w-full"
                />
              </Section>
            </div>

            {/* Right column: Overlay · Storage */}
            <div className="flex flex-col gap-5">
              <Section title="Overlay">
                <Row label="Show overlay" on={settings.overlayEnabled} onToggle={() => updateSettings({ overlayEnabled: !settings.overlayEnabled })} />
                {settings.overlayEnabled && (
                  <div className="mt-2 flex flex-col gap-2">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="mr-1 text-[11px] text-[#938ea1]">Style</span>
                      <Chip active={settings.visualizer === 'pill'} onClick={() => updateSettings({ visualizer: 'pill' })}>
                        Default <span className="ml-1 text-[10px] opacity-50">(Pill)</span>
                      </Chip>
                      <Chip active={settings.visualizer === 'circle'} onClick={() => updateSettings({ visualizer: 'circle' })}>
                        Focus <span className="ml-1 text-[10px] opacity-50">(Dot)</span>
                      </Chip>
                      {settings.visualizer === 'pill' && (
                        <>
                          <span className="mx-1 text-[#484555]">|</span>
                          <Chip active={settings.design === 'default'} onClick={() => updateSettings({ design: 'default' })}>Default</Chip>
                          <Chip active={settings.design === 'kombify'} onClick={() => updateSettings({ design: 'kombify' })}>kombify</Chip>
                        </>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="mr-1 text-[11px] text-[#938ea1]">Position</span>
                      {(['top', 'bottom', 'left', 'right'] as const).map((pos) => (
                        <Chip key={pos} active={settings.overlayPosition === pos} onClick={() => updateSettings({ overlayPosition: pos })}>
                          {pos.charAt(0).toUpperCase() + pos.slice(1)}
                        </Chip>
                      ))}
                    </div>
                    <Row label="Movable overlay" on={settings.overlayMovable} onToggle={() => updateSettings({ overlayMovable: !settings.overlayMovable })} />
                    {settings.overlayMovable && settings.visualizer === 'pill' && (
                      <p className="text-[11px] text-[#938ea1]/70">
                        Drag the center bubble inside the pill panel to place it anywhere on the desktop.
                      </p>
                    )}
                  </div>
                )}
              </Section>

              <Section title="Storage">
                <div className="flex flex-wrap gap-1.5">
                  <Chip active={settings.storeBackend === 'sqlite'} onClick={() => updateSettings({ storeBackend: 'sqlite' })}>SQLite</Chip>
                  <Chip active={settings.storeBackend === 'postgres'} onClick={() => updateSettings({ storeBackend: 'postgres' })}>PostgreSQL</Chip>
                </div>
                <p className={[
                  'mt-1.5 rounded border px-2.5 py-1.5 text-[11px]',
                  settings.storeBackend === 'postgres' && !postgresReady
                    ? 'border-orange-500/20 bg-orange-500/5 text-orange-200/70'
                    : 'border-[#484555]/50 bg-[#1f1f25] text-[#938ea1]',
                ].join(' ')}>
                  {settings.storeBackend === 'sqlite'
                    ? 'SQLite keeps metadata in the local SpeechKit app data folder.'
                    : postgresReady
                      ? 'PostgreSQL metadata backend is configured. Restart the app after changes.'
                      : 'Add a PostgreSQL connection string before switching the metadata backend.'}
                </p>
                {settings.storeBackend === 'sqlite' ? (
                  <input
                    id="sqlite-path-input"
                    aria-label="SQLite path"
                    value={settings.sqlitePath}
                    onChange={(e) => updateSettings({ sqlitePath: e.target.value }, 350)}
                    placeholder="%APPDATA%/SpeechKit/feedback.db"
                    className="mt-1.5 h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                  />
                ) : (
                  <input
                    id="postgres-dsn-input"
                    aria-label="PostgreSQL connection string"
                    value={settings.postgresDSN}
                    onChange={(e) => updateSettings({ postgresDSN: e.target.value }, 350)}
                    placeholder="postgres://user:password@host:5432/speechkit?sslmode=disable"
                    className="mt-1.5 h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                  />
                )}
                <div className="mt-2.5">
                  <Row label="Save raw audio locally" on={settings.saveAudio} onToggle={() => updateSettings({ saveAudio: !settings.saveAudio })} />
                </div>
                <div className="mt-2 grid grid-cols-2 gap-3">
                  <div>
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">Audio retention</div>
                    <select
                      id="audio-retention-select"
                      aria-label="Audio retention"
                      value={String(settings.audioRetentionDays)}
                      onChange={(e) => updateSettings({ audioRetentionDays: Number(e.target.value) })}
                      className="h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                    >
                      <option value="0">No automatic deletion</option>
                      <option value="1">1 day</option>
                      <option value="7">7 days</option>
                      <option value="30">30 days</option>
                      <option value="90">90 days</option>
                    </select>
                  </div>
                  <div>
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">Max storage (MB)</div>
                    <input
                      id="max-audio-storage-input"
                      aria-label="Max local audio storage (MB)"
                      type="number"
                      min="0"
                      value={String(settings.maxAudioStorageMB)}
                      onChange={(e) => {
                        const nextValue = Number.parseInt(e.target.value, 10)
                        if (Number.isNaN(nextValue) || nextValue < 0) return
                        updateSettings({ maxAudioStorageMB: nextValue }, 250)
                      }}
                      className="h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                    />
                  </div>
                </div>
              </Section>
            </div>

            {/* Vocabulary — full width */}
            <div className="col-span-2">
              <Section title="Vocabulary">
                <textarea
                  id="vocabulary-dictionary-input"
                  aria-label="Vocabulary dictionary"
                  value={settings.vocabularyDictionary}
                  onChange={(e) => updateSettings({ vocabularyDictionary: e.target.value }, 250)}
                  rows={3}
                  placeholder={'kombi fire => Kombify\nAcmeOS\nGemma'}
                  className="w-full rounded border border-[#484555] bg-[#0e0e13] px-3 py-2 text-xs leading-6 text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                />
                <p className="mt-1 text-[11px] text-[#938ea1]/70">
                  One term per line. Use <code>spoken =&gt; canonical</code> for corrections, or a bare term to bias transcription.
                </p>
              </Section>
            </div>
          </div>
        )}

        {(tab === 'stt' || tab === 'assist' || tab === 'realtime_voice') && (
          <ModelPanel
            modality={tab}
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
            dlCatalog={dlCatalog}
            dlJobs={dlJobs}
            setDlJobs={setDlJobs}
            confirmItem={confirmItem}
            setConfirmItem={setConfirmItem}
            dlBusy={dlBusy}
            setDlBusy={setDlBusy}
          />
        )}

        {/* Toast */}
        <div className={[
          'pointer-events-none fixed top-4 right-4 rounded-lg border border-emerald-400/20 bg-emerald-500/10 px-3 py-1.5 text-xs text-emerald-200 transition-all',
          toast ? 'translate-y-0 opacity-100' : '-translate-y-2 opacity-0',
        ].join(' ')}>
          {toast || '\u00A0'}
        </div>
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
        'w-full text-left px-3 py-2.5 rounded-lg text-sm transition-all',
        active
          ? 'bg-[#35343a] text-[#cabeff] font-semibold border-l-2 border-[#cabeff]'
          : 'text-[#938ea1] hover:text-[#e4e1e9] hover:bg-[#35343a]/50 border-l-2 border-transparent',
      ].join(' ')}
    >
      {children}
    </button>
  )
}

function Section({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <section>
      <div className="mb-1.5">
        <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">{title}</span>
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
      <div className="mb-1.5 text-xs font-medium text-[#c9c4d8]">{label}</div>
      <div className="flex flex-wrap gap-1.5">
        <Chip active={value === 'win+alt'} onClick={() => onChange('win+alt')}>Win + Alt</Chip>
        <Chip active={value === 'ctrl+win'} onClick={() => onChange('ctrl+win')}>Ctrl + Win</Chip>
        <span className="flex items-center gap-0">
          <Chip active={isCtrlShift} onClick={() => onChange(`ctrl+shift+${ctrlShiftSuffix}`)} className={isCtrlShift ? 'rounded-r-none' : ''}>
            Ctrl + Shift +
          </Chip>
          <select
            value={ctrlShiftSuffix}
            onChange={(e) => onChange(`ctrl+shift+${e.target.value}`)}
            className={[
              'h-8 rounded-r-lg border-y border-r px-2 text-xs font-medium uppercase outline-none',
              isCtrlShift
                ? 'border-[#947dff]/60 bg-[#947dff]/20 text-[#cabeff]'
                : 'border-[#484555] bg-[#0e0e13] text-[#938ea1]',
            ].join(' ')}
          >
            {CTRL_SHIFT_SUFFIX_KEYS.map((k) => (
              <option key={k} value={k}>{k === 'space' ? 'Space' : k.toUpperCase()}</option>
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

type ModalityTabKey = 'stt' | 'assist' | 'realtime_voice'

function limitProfilesToVisibleOptions(
  profiles: SpeechKitSettingsState['profiles'],
  activeProfileID?: string,
  maxOptions = 3,
) {
  const list = profiles ?? []
  if (list.length <= maxOptions) return list
  const next = list.slice(0, maxOptions)
  if (!activeProfileID || next.some((profile) => profile.id === activeProfileID)) return next
  const activeProfile = list.find((profile) => profile.id === activeProfileID)
  if (!activeProfile) return next
  return [...next.slice(0, maxOptions - 1), activeProfile]
}

function sourceBadge(profile: NonNullable<SpeechKitSettingsState['profiles']>[number]) {
  switch (profile.executionMode) {
    case 'local':
    case 'ollama_local':
      return { label: 'local', className: 'bg-[#35343a]/60 text-[#938ea1]' }
    case 'hf_routed':
      return { label: 'hugging face', className: 'bg-[#35343a]/60 text-[#938ea1]' }
    default:
      return { label: 'api key', className: 'bg-[#35343a]/60 text-[#938ea1]' }
  }
}

function ModelPanel({
  modality,
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
  dlCatalog,
  dlJobs,
  setDlJobs,
  confirmItem,
  setConfirmItem,
  dlBusy,
  setDlBusy,
}: {
  modality: ModalityTabKey
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
  dlCatalog: DownloadItem[]
  dlJobs: DownloadJob[]
  setDlJobs: React.Dispatch<React.SetStateAction<DownloadJob[]>>
  confirmItem: DownloadItem | null
  setConfirmItem: React.Dispatch<React.SetStateAction<DownloadItem | null>>
  dlBusy: boolean
  setDlBusy: React.Dispatch<React.SetStateAction<boolean>>
}) {
  const profiles = settings.profiles ?? []
  const filtered = profiles.filter((p) => p.modality === modality)
  const activeId = settings.activeProfiles?.[modality]
  const visibleProfiles = limitProfilesToVisibleOptions(filtered, activeId, 4)

  return (
    <>
      <div className="overflow-hidden rounded-xl border border-[#484555]/80">
        {/* Panel header */}
        <div className="flex items-center gap-3 border-b border-[#484555]/60 bg-[#1f1f25] px-4 py-2.5">
          <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">Model setup</span>
          <span className="text-[11px] text-[#484555]">
            {modality === 'stt' ? 'Speech-to-Text' : modality === 'assist' ? 'Assist LLM' : 'Voice Agent'}
          </span>
        </div>

        {visibleProfiles.length === 0 ? (
          <p className="px-4 py-4 text-[11px] text-[#938ea1]">No live-switchable model profiles are exposed in this build.</p>
        ) : (
          <div className="divide-y divide-[#484555]/50">
            {visibleProfiles.map((profile) => {
              const isActive = activeId === profile.id
              const badge = sourceBadge(profile)
              const providerKey = profile.executionMode ? PROVIDER_FOR_EXECUTION_MODE[profile.executionMode] : undefined
              const providerCredential = providerKey ? settings.providerCredentials?.[providerKey] : undefined
              const providerMissing = Boolean(providerKey && !providerCredential?.available)
              const providerCopy = providerCredential ? providerCredentialCopy(profile.name, providerCredential) : null
              const providerReady = Boolean(providerKey && providerCredential?.available)
              const providerIsBusy = providerKey ? (providerBusy[providerKey] ?? false) : false
              const downloadItems = dlCatalog.filter((item) => item.profileId === profile.id)
              const downloadActive = downloadItems.some((item) => {
                const job = dlJobs.find((candidate) => candidate.modelId === item.id)
                return job?.status === 'pending' || job?.status === 'running'
              })
              const downloadReady = downloadItems.some((item) => {
                const job = dlJobs.find((candidate) => candidate.modelId === item.id)
                return item.available || job?.status === 'done'
              })
              const needsDownload = downloadItems.length > 0 && !downloadReady && !downloadActive
              const readyToUse = !providerMissing && !needsDownload && !downloadActive
              const statusLabel = isActive
                ? 'Active'
                : providerMissing
                  ? providerCopy?.neededLabel ?? `${providerCredential?.label ?? 'API'} key needed`
                  : downloadActive
                    ? 'Downloading'
                    : needsDownload
                      ? 'Download required'
                      : 'Ready'
              const statusClassName = isActive
                ? 'border-[#947dff]/30 bg-[#947dff]/15 text-[#cabeff]'
                : 'border-[#484555] bg-transparent text-[#938ea1]'

              return (
                <div key={profile.id} className={isActive ? 'bg-[#947dff]/4' : undefined}>
                  {/* Main identity row */}
                  <div className="flex items-center gap-3 px-4 py-3">
                    <div className={['size-2 shrink-0 rounded-full', isActive ? 'bg-[#cabeff]' : 'bg-[#484555]'].join(' ')} />
                    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
                      <span className={['text-[13px] font-medium', isActive ? 'text-[#cabeff]' : 'text-[#e4e1e9]/85'].join(' ')}>
                        {profile.name}
                      </span>
                      <span className={['shrink-0 rounded px-1.5 py-px text-[9px]', badge.className].join(' ')}>{badge.label}</span>
                      <span className="text-[10px] text-[#938ea1]/60">{profile.source ?? profile.executionMode ?? 'local'}</span>
                      {profile.description && (
                        <span className="truncate text-[11px] text-[#938ea1]/70">{profile.description}</span>
                      )}
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <span className={['rounded-full border px-2 py-0.5 text-[10px]', statusClassName].join(' ')}>{statusLabel}</span>
                      {isActive ? (
                        <span className="w-20 text-right text-[11px] font-medium text-[#cabeff]/80">Currently active</span>
                      ) : readyToUse ? (
                        <button
                          type="button"
                          onClick={async () => {
                            try {
                              const body = new URLSearchParams({ modality: profile.modality, profile_id: profile.id })
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
                          className="w-20 rounded border border-[#484555] px-2 py-1 text-[11px] text-[#938ea1] hover:border-[#cabeff]/30 hover:text-[#e4e1e9]"
                        >
                          Use model
                        </button>
                      ) : (
                        <span className="w-20 text-right text-[10px] text-[#938ea1]">
                          {providerMissing
                            ? providerCopy?.unlockLabel ?? 'Add the key above.'
                            : needsDownload
                              ? 'Download required.'
                              : 'Downloading…'}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Provider missing: inline key entry */}
                  {providerMissing && providerKey && providerCredential && (
                    <div className="flex items-center gap-2 border-t border-amber-500/10 bg-amber-500/4 px-4 py-2">
                      <span className="shrink-0 text-[10px] font-medium text-amber-200/60">
                        {providerCopy?.title ?? `Add ${providerCredential.label} key`}
                      </span>
                      <input
                        aria-label={providerCopy?.inputLabel ?? `${profile.name} API key`}
                        type="password"
                        value={providerTokens[providerKey] ?? ''}
                        onChange={(e) => setProviderTokens((tokens) => ({ ...tokens, [providerKey]: e.target.value }))}
                        placeholder={providerCopy?.placeholder ?? (providerCredential.envName || 'API key')}
                        className="h-7 flex-1 rounded border border-amber-500/15 bg-black/20 px-2.5 text-[11px] text-[#e4e1e9]/80 outline-none focus:border-[#947dff]/50"
                      />
                      <button
                        type="button"
                        onClick={() => onSaveCredential(providerKey)}
                        disabled={providerIsBusy}
                        className={[
                          'shrink-0 rounded border px-3 py-1 text-[11px] font-medium',
                          providerIsBusy
                            ? 'border-[#484555] bg-[#35343a] text-[#938ea1]'
                            : 'border-[#947dff]/25 bg-[#947dff]/15 text-[#cabeff] hover:bg-[#947dff]/25',
                        ].join(' ')}
                      >
                        {providerCopy?.saveLabel ?? 'Save key'}
                      </button>
                    </div>
                  )}

                  {/* Provider ready: token management row */}
                  {providerReady && providerKey && providerCredential && (
                    <div className="flex items-center gap-2 border-t border-[#484555]/50 bg-[#0e0e13]/30 px-4 py-2">
                      <span className="shrink-0 text-[10px] text-[#938ea1]">{tokenStatusLabel(providerCredential)}</span>
                      <input
                        type="password"
                        aria-label={`Update ${providerCredential.label} ${providerSecretNoun(providerCredential.provider)}`}
                        placeholder={`Update ${providerSecretNoun(providerCredential.provider)}…`}
                        value={providerTokens[providerKey] ?? ''}
                        onChange={(e) => setProviderTokens((tokens) => ({ ...tokens, [providerKey]: e.target.value }))}
                        className="h-6 min-w-0 flex-1 rounded border border-[#484555]/80 bg-[#0e0e13] px-2 text-[11px] text-[#e4e1e9]/70 outline-none focus:border-[#947dff]/40"
                      />
                      {(providerTokens[providerKey] ?? '').trim().length > 0 && (
                        <button type="button" onClick={() => onSaveCredential(providerKey)} disabled={providerIsBusy} className="shrink-0 rounded px-2 py-0.5 text-[10px] text-[#cabeff]/80 hover:text-[#cabeff]">Save</button>
                      )}
                      <button type="button" onClick={() => onTestCredential(providerKey)} disabled={providerIsBusy} className="shrink-0 text-[10px] text-[#938ea1] hover:text-[#e4e1e9]">Test</button>
                      {providerCredential.hasStoredSecret && (
                        <button type="button" onClick={() => onClearCredential(providerKey)} disabled={providerIsBusy} className="shrink-0 text-[10px] text-[#938ea1] hover:text-red-300/75">Clear</button>
                      )}
                    </div>
                  )}

                  {/* Download items */}
                  {downloadItems.length > 0 && (
                    <div className="space-y-1.5 border-t border-[#484555]/50 px-4 py-2">
                      {downloadItems.map((item) => {
                        const itemJob = dlJobs.find((candidate) => candidate.modelId === item.id)
                        const itemDownloadActive = itemJob?.status === 'pending' || itemJob?.status === 'running'
                        const itemDownloadReady = Boolean(item.available || itemJob?.status === 'done')

                        return (
                          <div key={item.id} className="flex items-center gap-2">
                            <span className="text-[11px] text-[#c9c4d8]">{item.name}</span>
                            {item.recommended && (
                              <span className="rounded bg-[#947dff]/15 px-1.5 py-px text-[9px] text-[#cabeff]/70">recommended</span>
                            )}
                            <span className="text-[10px] text-[#938ea1]">{item.sizeLabel}</span>
                            {itemDownloadActive ? (
                              <>
                                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-[#484555]">
                                  <div className="h-full rounded-full bg-[#947dff]/60 transition-all duration-500" style={{ width: `${Math.round((itemJob?.progress ?? 0) * 100)}%` }} />
                                </div>
                                <span className="shrink-0 text-[10px] text-[#938ea1]">{itemJob?.statusText}</span>
                                <button type="button" onClick={() => { if (itemJob) cancelModelDownload(itemJob.id).catch(() => {}) }} className="shrink-0 text-[10px] text-[#938ea1] hover:text-red-300/75">Cancel download</button>
                              </>
                            ) : itemDownloadReady ? (
                              <span className="text-[10px] text-emerald-300/80">Ready on this device</span>
                            ) : (
                              <button type="button" onClick={() => setConfirmItem(item)} className="rounded border border-[#484555] px-2 py-0.5 text-[10px] text-[#938ea1] hover:border-[#cabeff]/30 hover:text-[#cabeff]">Download</button>
                            )}
                            {itemJob?.status === 'failed' && (
                              <span className="ml-1 text-[10px] text-red-400/70">{itemJob.error ?? 'Download failed'}</span>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Download confirmation modal */}
      {confirmItem && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="w-80 rounded-xl border border-[#484555] bg-[#131318] p-5 shadow-2xl">
            <h3 className="text-sm font-semibold text-[#e4e1e9]">Download Model</h3>
            <p className="mt-2 text-xs font-medium text-[#e4e1e9]/80">{confirmItem.name}</p>
            <p className="mt-1 text-[11px] leading-relaxed text-[#938ea1]">{confirmItem.description}</p>
            <div className="mt-3 flex flex-wrap gap-x-2 gap-y-0.5 text-[10px] text-[#938ea1]">
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
                className="flex-1 rounded-lg bg-[#947dff]/20 py-1.5 text-xs font-medium text-[#cabeff] hover:bg-[#947dff]/30 disabled:opacity-50"
              >
                {dlBusy ? 'Starting…' : 'Download'}
              </button>
              <button
                type="button"
                onClick={() => setConfirmItem(null)}
                className="flex-1 rounded-lg border border-[#484555] py-1.5 text-xs text-[#938ea1] hover:border-[#cabeff]/30 hover:text-[#e4e1e9]"
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
          ? 'border-[#947dff]/60 bg-[#947dff]/20 text-[#cabeff]'
          : 'border-[#484555] bg-[#0e0e13] text-[#938ea1] hover:border-[#cabeff]/30 hover:text-[#e4e1e9]',
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
      <span className="text-sm text-[#c9c4d8]">{label}</span>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={on}
        onClick={onToggle}
        className={[
          'relative inline-flex h-5.5 w-9.5 shrink-0 cursor-pointer items-center rounded-full transition-colors',
          on ? 'bg-[#947dff]' : 'bg-[#484555]',
        ].join(' ')}
      >
        <span
          className={[
            'inline-block h-4 w-4 rounded-full bg-white shadow transition-transform',
            on ? 'translate-x-4.75' : 'translate-x-0.75',
          ].join(' ')}
        />
      </button>
    </div>
  )
}
