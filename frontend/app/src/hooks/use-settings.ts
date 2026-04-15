import { useCallback, useEffect, useRef, useState } from 'react'

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

type SettingsTab = 'general' | 'stt' | 'assist' | 'realtime_voice'

function providerSecretNoun(provider?: string) {
  return provider === 'huggingface' ? 'token' : 'key'
}

export function providerCredentialCopy(
  profileName: string,
  credential: ProviderCredentialState,
) {
  const noun = providerSecretNoun(credential.provider)
  const credentialLabel = `${credential.label} ${noun}`
  return {
    title: `Add ${credentialLabel}`,
    inputLabel: `${profileName} ${credentialLabel}`,
    placeholder:
      credential.envName || (noun === 'token' ? 'Token' : 'API key'),
    saveLabel: `Save ${noun}`,
    neededLabel: `${credentialLabel} needed`,
    unlockLabel: `Add the ${noun} above to unlock this model.`,
  }
}

export function useSettings(showToast: (msg: string) => void) {
  const [settings, setSettings] = useState(defaultSettingsState)
  const [providerTokens, setProviderTokens] = useState<
    Record<string, string>
  >({})
  const [providerBusy, setProviderBusy] = useState<Record<string, boolean>>(
    {},
  )
  const [loaded, setLoaded] = useState(false)
  const [tab, setTab] = useState<SettingsTab>('general')

  // Downloads
  const [dlCatalog, setDlCatalog] = useState<DownloadItem[]>([])
  const [dlJobs, setDlJobs] = useState<DownloadJob[]>([])
  const [confirmItem, setConfirmItem] = useState<DownloadItem | null>(null)
  const [dlBusy, setDlBusy] = useState(false)

  const saveTimer = useRef<number | null>(null)

  // ── Load ─────────────────────────────────────────────────────────

  const loadSettings = useCallback(async () => {
    const [state, profiles] = await Promise.all([
      fetchSettingsState(),
      fetchModelProfiles().catch(() => []),
    ])
    setSettings({
      ...state,
      profiles: state.profiles?.length ? state.profiles : profiles,
    })
  }, [])

  useEffect(() => {
    let active = true
    void loadSettings()
      .then(() => {
        if (active) setLoaded(true)
      })
      .catch(() => {
        if (active) setLoaded(true)
      })
    fetchDownloadCatalog().then(setDlCatalog).catch(() => {})
    fetchDownloadJobs().then(setDlJobs).catch(() => {})
    return () => {
      active = false
      if (saveTimer.current) window.clearTimeout(saveTimer.current)
    }
  }, [loadSettings])

  // ── Download job polling ─────────────────────────────────────────

  useEffect(() => {
    const hasActive = dlJobs.some(
      (j) => j.status === 'pending' || j.status === 'running',
    )
    if (!hasActive) return

    const timer = setInterval(() => {
      fetchDownloadJobs()
        .then((jobs) => {
          setDlJobs(jobs)
          const wasRunning = dlJobs.some(
            (j) => j.status === 'running' || j.status === 'pending',
          )
          const nowDone = jobs.every(
            (j) =>
              j.status === 'done' ||
              j.status === 'failed' ||
              j.status === 'cancelled',
          )
          if (wasRunning && nowDone) {
            fetchDownloadCatalog().then(setDlCatalog).catch(() => {})
          }
        })
        .catch(() => {})
    }, 2000)
    return () => clearInterval(timer)
  }, [dlJobs])

  // ── Save ─────────────────────────────────────────────────────────

  const queueSave = useCallback(
    (next: SpeechKitSettingsState, delay: number) => {
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
    },
    [loaded, showToast],
  )

  const updateSettings = useCallback(
    (patch: Partial<SpeechKitSettingsState>, delay = 0) => {
      setSettings((prev) => {
        const next = { ...prev, ...patch }
        queueSave(next, delay)
        return next
      })
    },
    [queueSave],
  )

  // ── Provider credentials ─────────────────────────────────────────

  const applyFreshSettings = useCallback((fresh: SpeechKitSettingsState) => {
    setSettings((prev) => ({
      ...prev,
      ...fresh,
      profiles: fresh.profiles?.length ? fresh.profiles : prev.profiles,
    }))
  }, [])

  const handleSaveProviderCredential = useCallback(
    async (provider: string) => {
      const token = (providerTokens[provider] ?? '').trim()
      const label =
        settings.providerCredentials?.[provider]?.label ?? 'API'
      const noun = providerSecretNoun(provider)
      if (!token) {
        showToast(`${label} ${noun} required`)
        return
      }
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
    },
    [providerTokens, settings.providerCredentials, showToast, applyFreshSettings],
  )

  const handleClearProviderCredential = useCallback(
    async (provider: string) => {
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
    },
    [showToast, applyFreshSettings],
  )

  const handleTestProviderCredential = useCallback(
    async (provider: string) => {
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
    },
    [providerTokens, settings.providerCredentials, showToast],
  )

  // ── Model profile activation ─────────────────────────────────────

  const activateProfile = useCallback(
    async (modality: string, profileId: string, profileName: string) => {
      try {
        const body = new URLSearchParams({
          modality,
          profile_id: profileId,
        })
        const resp = await fetch('/models/profiles/activate', {
          method: 'POST',
          body,
        })
        if (!resp.ok) {
          const err = await resp.text()
          showToast(err || 'Switch failed')
          return
        }
        setSettings((prev) => ({
          ...prev,
          activeProfiles: { ...prev.activeProfiles, [modality]: profileId },
        }))
        showToast(`${profileName} activated`)
      } catch {
        showToast('Switch failed')
      }
    },
    [showToast],
  )

  // ── Download actions ─────────────────────────────────────────────

  const startDownload = useCallback(
    async (item: DownloadItem) => {
      setDlBusy(true)
      try {
        const job = await startModelDownload(item.id)
        setDlJobs((prev) => [
          ...prev.filter((j) => j.modelId !== item.id),
          job,
        ])
        setConfirmItem(null)
      } catch (e) {
        showToast(e instanceof Error ? e.message : 'Download failed')
      } finally {
        setDlBusy(false)
      }
    },
    [showToast],
  )

  const cancelDownload = useCallback(async (jobId: string) => {
    await cancelModelDownload(jobId).catch(() => {})
  }, [])

  // ── Derived ──────────────────────────────────────────────────────

  const postgresReady =
    settings.postgresConfigured || settings.postgresDSN.trim().length > 0

  const tokenStatusLabel = useCallback((cred: ProviderCredentialState) => {
    const noun = providerSecretNoun(cred.provider)
    switch (cred.source) {
      case 'user':
        return `User ${noun} active`
      case 'install':
        return `Install ${noun} active`
      case 'env':
        return `Environment ${noun} active`
      default:
        return `No ${noun} configured`
    }
  }, [])

  return {
    settings,
    loaded,
    tab,
    setTab,
    updateSettings,
    postgresReady,

    // Provider credentials
    providerTokens,
    setProviderTokens,
    providerBusy,
    tokenStatusLabel,
    handleSaveProviderCredential,
    handleClearProviderCredential,
    handleTestProviderCredential,

    // Model profiles
    activateProfile,
    providerCredentialCopy,

    // Downloads
    dlCatalog,
    dlJobs,
    setDlJobs,
    confirmItem,
    setConfirmItem,
    dlBusy,
    startDownload,
    cancelDownload,
  }
}
