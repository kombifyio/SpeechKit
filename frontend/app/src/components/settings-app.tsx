import { useEffect, useRef, useState } from 'react'

import { MicSelector } from '@/components/ui/mic-selector'
import {
  clearHuggingFaceToken,
  defaultSettingsState,
  fetchModelProfiles,
  fetchSettingsState,
  saveHuggingFaceToken,
  saveSettingsState,
  type SpeechKitSettingsState,
} from '@/lib/speechkit'

type Tab = 'general' | 'provider'

const CTRL_SHIFT_SUFFIX_KEYS = ['d', 'j', 'k', 'space'] as const

export function SettingsApp() {
  const [settings, setSettings] = useState(defaultSettingsState)
  const [hfToken, setHFToken] = useState('')
  const [hfTokenBusy, setHFTokenBusy] = useState(false)
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

  const tokenStatusLabel = () => {
    switch (settings.hfTokenSource) {
      case 'user':
        return 'User token active'
      case 'install':
        return 'Install token active'
      case 'env':
        return 'Environment token active'
      default:
        return 'No token configured'
    }
  }

  const postgresReady =
    settings.postgresConfigured || settings.postgresDSN.trim().length > 0

  const handleSaveHuggingFaceToken = async () => {
    const trimmed = hfToken.trim()
    if (!trimmed) {
      showToast('Token required')
      return
    }
    setHFTokenBusy(true)
    try {
      const message = await saveHuggingFaceToken(trimmed)
      await loadSettings()
      setHFToken('')
      showToast(message || 'Saved')
    } catch {
      showToast('Save failed')
    } finally {
      setHFTokenBusy(false)
    }
  }

  const handleClearHuggingFaceToken = async () => {
    setHFTokenBusy(true)
    try {
      const message = await clearHuggingFaceToken()
      await loadSettings()
      setHFToken('')
      showToast(message || 'Saved')
    } catch {
      showToast('Save failed')
    } finally {
      setHFTokenBusy(false)
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
            hfToken={hfToken}
            setHFToken={setHFToken}
            hfTokenBusy={hfTokenBusy}
            tokenStatusLabel={tokenStatusLabel()}
            onSaveHFToken={handleSaveHuggingFaceToken}
            onClearHFToken={handleClearHuggingFaceToken}
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

const MODALITY_LABELS = {
  stt: 'STT',
  utility: 'Utility',
  agent: 'Agent',
} as const

type ModalityTabKey = keyof typeof MODALITY_LABELS

const MODALITY_ORDER: ModalityTabKey[] = ['stt', 'utility', 'agent']

function ProviderTab({
  settings,
  setSettings,
  showToast,
  hfToken,
  setHFToken,
  hfTokenBusy,
  tokenStatusLabel,
  onSaveHFToken,
  onClearHFToken,
}: {
  settings: SpeechKitSettingsState
  setSettings: React.Dispatch<React.SetStateAction<SpeechKitSettingsState>>
  showToast: (msg: string) => void
  hfToken: string
  setHFToken: (v: string) => void
  hfTokenBusy: boolean
  tokenStatusLabel: string
  onSaveHFToken: () => void
  onClearHFToken: () => void
}) {
  const [modalityTab, setModalityTab] = useState<ModalityTabKey>('stt')

  const profiles = settings.profiles ?? []
  const availableTabs = MODALITY_ORDER
    .filter((key) => profiles.some((profile) => profile.modality === key))
    .map((key) => ({ key, label: MODALITY_LABELS[key] }))
  const selectedTab = availableTabs.some((tab) => tab.key === modalityTab)
    ? modalityTab
    : availableTabs[0]?.key
  const filtered = selectedTab ? profiles.filter((p) => p.modality === selectedTab) : []
  const activeId = selectedTab ? settings.activeProfiles?.[selectedTab] : undefined

  return (
    <div className="mt-4 flex flex-col gap-4">
      {settings.hfAvailable && (
        <Section title="API Tokens">
          <div className="text-[11px] text-white/40">{tokenStatusLabel}</div>
          <div className="mt-2 flex gap-2">
            <input
              aria-label="Hugging Face token"
              type="password"
              value={hfToken}
              onChange={(e) => setHFToken(e.target.value)}
              placeholder="hf_..."
              className="h-8 flex-1 rounded-lg border border-white/10 bg-white/5 px-3 text-xs outline-none focus:border-orange-400/50"
            />
            <Chip
              active={false}
              onClick={() => { void onSaveHFToken() }}
              className={hfTokenBusy ? 'pointer-events-none opacity-60' : ''}
            >
              Save
            </Chip>
            {settings.hfHasUserToken && (
              <Chip
                active={false}
                onClick={() => { void onClearHFToken() }}
                className={hfTokenBusy ? 'pointer-events-none opacity-60' : ''}
              >
                Clear
              </Chip>
            )}
          </div>
        </Section>
      )}

      <Section title="Models">
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

        {filtered.length === 0 ? (
          <p className="mt-3 text-xs text-white/30">No live-switchable model profiles are exposed in this build.</p>
        ) : (
          <div className="mt-2">
            {filtered.map((profile) => {
              const isActive = activeId === profile.id
              return (
                <div
                  key={profile.id}
                  className={[
                    'flex items-center justify-between border-b border-white/5 py-2 last:border-0',
                    isActive ? 'text-orange-200' : 'text-white/60',
                  ].join(' ')}
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-xs font-medium">{profile.name}</span>
                      {profile.executionMode === 'local' || profile.executionMode === 'ollama_local' ? (
                        <span className="shrink-0 rounded bg-emerald-500/15 px-1 py-px text-[9px] text-emerald-300/80">local runtime</span>
                      ) : profile.executionMode === 'hf_routed' ? (
                        <span className="shrink-0 rounded bg-sky-500/15 px-1 py-px text-[9px] text-sky-300/80">managed</span>
                      ) : (
                        <span className="shrink-0 rounded bg-amber-500/15 px-1 py-px text-[9px] text-amber-300/70">api key</span>
                      )}
                    </div>
                    <div className="truncate text-[10px] text-white/25">
                      {profile.source ?? profile.executionMode ?? 'local'}
                    </div>
                    {profile.description && (
                      <div className="mt-1 text-[10px] leading-relaxed text-white/35">
                        {profile.description}
                      </div>
                    )}
                  </div>
                  {isActive ? (
                    <span className="ml-2 shrink-0 text-[10px] font-medium text-orange-400">Active</span>
                  ) : (
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
                      className="ml-2 shrink-0 rounded border border-white/10 px-2 py-0.5 text-[10px] text-white/40 hover:border-white/20 hover:text-white/60"
                    >
                      Use
                    </button>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </Section>

      <p className="text-[11px] leading-relaxed text-white/25">
        Only provider paths that the desktop backend can switch live are listed here. Local Ollama profiles still require the local runtime and pulled model to be present.
      </p>
    </div>
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
