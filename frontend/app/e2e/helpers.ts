import type { Page, Route } from '@playwright/test'

/**
 * Mock backend API routes so tests run without a Go process.
 *
 * Every surface fetches specific endpoints on mount. This helper intercepts
 * all of them and returns minimal valid JSON so the React trees render.
 */
export async function mockBackendRoutes(page: Page): Promise<void> {
  // Overlay state — polled every 90 ms by useOverlaySnapshot
  await page.route('**/overlay/state', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        state: 'idle',
        phase: 'idle',
        text: '',
        level: 0,
        visible: true,
        visualizer: 'pill',
        design: 'default',
        hotkey: 'win+alt',
        dictateHotkey: 'win+alt',
        assistHotkey: 'ctrl+win',
        voiceAgentHotkey: 'ctrl+shift',
        dictateHotkeyBehavior: 'push_to_talk',
        assistHotkeyBehavior: 'push_to_talk',
        voiceAgentHotkeyBehavior: 'toggle',
        modeEnabled: { dictate: true, assist: true, voice_agent: true },
        agentHotkey: 'ctrl+win',
        activeMode: 'none',
        availableModes: { dictate: true, assist: true, voice_agent: true },
        position: 'top',
        movable: false,
        positionFreeX: 0,
        positionFreeY: 0,
        lastTranscription: '',
        quickNoteMode: false,
        selectedAudioDeviceId: '',
        activeProfiles: {},
      }),
    }),
  )

  // Settings state
  await page.route('**/settings/state', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        overlayEnabled: true,
        storeBackend: 'sqlite',
        sqlitePath: 'C:\\SpeechKit\\speechkit.db',
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
        assistHotkey: 'ctrl+win',
        voiceAgentHotkey: 'ctrl+shift',
        dictateHotkeyBehavior: 'push_to_talk',
        assistHotkeyBehavior: 'push_to_talk',
        voiceAgentHotkeyBehavior: 'toggle',
        modeEnabled: { dictate: true, assist: true, voice_agent: true },
        agentHotkey: 'ctrl+win',
        agentMode: 'assist',
        activeMode: 'none',
        availableModes: { dictate: true, assist: true, voice_agent: true },
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
        profiles: [],
        providerCredentials: {},
      }),
    }),
  )

  // Audio devices
  await page.route('**/audio/devices', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ devices: [], selectedDeviceId: '' }),
    }),
  )

  // Model profiles
  await page.route('**/models/profiles', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    }),
  )

  // Download catalog + jobs
  await page.route('**/models/downloads/catalog', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    }),
  )

  await page.route('**/models/downloads/jobs', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    }),
  )

  // Quick note fetch (used when quicknote.html has ?id=N in query string)
  await page.route('**/quicknotes/get**', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ text: '' }),
    }),
  )

  // Catch-all POST routes (mutations) — return generic OK so buttons don't error
  await page.route('**/{quicknotes,settings,overlay,models}/**', (route: Route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ message: 'ok' }),
      })
    }
    return route.continue()
  })
}
