import type { Dispatch, SetStateAction } from "react";

import { HotkeyPicker } from "@/components/settings/hotkeys-panel";
import { DictionaryListEditor } from "@/components/settings/dictionary-panel";
import {
  ModeGuidePanel,
  ModeSection,
} from "@/components/settings/modes-panel";
import {
  ModelPanel,
  type ModalityTabKey,
} from "@/components/settings/model-panel";
import {
  Chip,
  OverlayFeedbackModePicker,
  Row,
} from "@/components/settings/settings-primitives";

// NOTE: do NOT add cross-mode settings (e.g. wake-word, microphone,
// overlay) to this page. Mode pages are scoped to the *one* mode they're
// rendering. Anything that crosses modes lives in
// `general-settings-page.tsx`. See docs/settings-architecture.md.
import {
  MODE_DEFAULT_BASES,
  type ConfigurableMode,
} from "@/components/settings/settings-state";
import type {
  DownloadItem,
  DownloadJob,
  HotkeyBehavior,
  ModeModelSelectionState,
  ProviderCredentialState,
  SpeechKitSettingsState,
} from "@/lib/speechkit";

export function ModeSettingsPage({
  mode,
  settings,
  updateSettings,
  toggleModeEnabled,
  updateModeHotkey,
  updateModeHotkeyBehavior,
  showToast,
  providerTokens,
  setProviderTokens,
  providerBusy,
  tokenStatusLabel,
  onSaveCredential,
  onClearCredential,
  onTestCredential,
  onUpdateSelection,
  onRefreshSettings,
  dlCatalog,
  setDlCatalog,
  dlJobs,
  setConfirmItem,
}: {
  mode: ModalityTabKey;
  settings: SpeechKitSettingsState;
  updateSettings: (patch: Partial<SpeechKitSettingsState>, delay?: number) => void;
  toggleModeEnabled: (mode: ConfigurableMode) => void;
  updateModeHotkey: (mode: ConfigurableMode, value: string) => void;
  updateModeHotkeyBehavior: (
    mode: ConfigurableMode,
    value: HotkeyBehavior,
  ) => void;
  showToast: (message: string) => void;
  providerTokens: Record<string, string>;
  setProviderTokens: Dispatch<SetStateAction<Record<string, string>>>;
  providerBusy: Record<string, boolean>;
  tokenStatusLabel: (cred: ProviderCredentialState) => string;
  onSaveCredential: (provider: string) => void;
  onClearCredential: (provider: string) => void;
  onTestCredential: (provider: string) => void;
  onUpdateSelection: (
    mode: keyof SpeechKitSettingsState["modelSelections"],
    next: ModeModelSelectionState,
  ) => void;
  onRefreshSettings: () => Promise<void>;
  dlCatalog: DownloadItem[];
  setDlCatalog: Dispatch<SetStateAction<DownloadItem[]>>;
  dlJobs: DownloadJob[];
  setConfirmItem: Dispatch<SetStateAction<DownloadItem | null>>;
}) {
  return (
    <>
      {mode === "stt" && (
        <div className="mb-5">
          <ModeGuidePanel mode="stt" />
          <div className="grid gap-5 lg:grid-cols-2">
            <ModeSection
              title="Transcribe Controls"
              testId="transcribe-mode-controls"
            >
              <HotkeyPicker
                label="Dictate hotkey"
                enabled={settings.modeEnabled.dictate}
                value={settings.dictateHotkey}
                behavior={settings.dictateHotkeyBehavior}
                defaultBase={MODE_DEFAULT_BASES.dictate}
                onToggleEnabled={() => toggleModeEnabled("dictate")}
                onChange={(value) => updateModeHotkey("dictate", value)}
                onChangeBehavior={(value) =>
                  updateModeHotkeyBehavior("dictate", value)
                }
              />
            </ModeSection>
            <ModeSection title="User Dictionary">
              <DictionaryListEditor
                value={settings.vocabularyDictionary}
                onChange={(value) =>
                  updateSettings({ vocabularyDictionary: value }, 250)
                }
              />
            </ModeSection>
          </div>
        </div>
      )}

      {mode === "assist" && (
        <div className="mb-5">
          <ModeGuidePanel mode="assist" />
          <ModeSection title="Assist Controls" testId="assist-mode-controls">
            <HotkeyPicker
              label="Assist hotkey"
              enabled={settings.modeEnabled.assist}
              value={settings.assistHotkey}
              behavior={settings.assistHotkeyBehavior}
              defaultBase={MODE_DEFAULT_BASES.assist}
              onToggleEnabled={() => toggleModeEnabled("assist")}
              onChange={(value) => updateModeHotkey("assist", value)}
              onChangeBehavior={(value) =>
                updateModeHotkeyBehavior("assist", value)
              }
            />
            {settings.visualizer === "pill" && (
              <div className="mt-3 border-t border-[color:var(--sk-shell-divider)] pt-3">
                <OverlayFeedbackModePicker
                  label="Overlay feedback"
                  value={settings.assistOverlayMode}
                  onChange={(assistOverlayMode) =>
                    updateSettings({ assistOverlayMode })
                  }
                />
              </div>
            )}
          </ModeSection>
        </div>
      )}

      {mode === "realtime_voice" && (
        <div className="mb-5">
          <ModeGuidePanel mode="realtime_voice" />
          <div className="grid gap-5 lg:grid-cols-2">
            <ModeSection
              title="Voice Agent Controls"
              testId="voice-agent-mode-controls"
            >
              <HotkeyPicker
                label="Voice Agent hotkey"
                enabled={settings.modeEnabled.voice_agent}
                value={settings.voiceAgentHotkey}
                behavior={settings.voiceAgentHotkeyBehavior}
                defaultBase={MODE_DEFAULT_BASES.voice_agent}
                onToggleEnabled={() => toggleModeEnabled("voice_agent")}
                onChange={(value) => updateModeHotkey("voice_agent", value)}
                onChangeBehavior={(value) =>
                  updateModeHotkeyBehavior("voice_agent", value)
                }
              />
              {settings.visualizer === "pill" && (
                <div className="mt-3 border-t border-[color:var(--sk-shell-divider)] pt-3">
                  <OverlayFeedbackModePicker
                    label="Overlay feedback"
                    value={settings.voiceAgentOverlayMode}
                    onChange={(voiceAgentOverlayMode) =>
                      updateSettings({ voiceAgentOverlayMode })
                    }
                  />
                </div>
              )}
            </ModeSection>

            <ModeSection title="Conversation">
              <div className="flex flex-col gap-2">
                <p className="text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
                  Add your personal preferences on top of the built-in Voice
                  Agent framework, then choose what the transcript window close
                  button should do. Minimise always keeps the current
                  conversation available in the taskbar.
                </p>
                <div className="grid gap-3">
                  <label className="flex flex-col gap-1.5">
                    <span className="text-[11px] font-medium text-[color:var(--sk-text)]">
                      Agent profile
                    </span>
                    <select
                      aria-label="Voice Agent agent profile"
                      value={settings.voiceAgentProfileId}
                      onChange={(e) =>
                        updateSettings({ voiceAgentProfileId: e.target.value })
                      }
                      className="sk-input h-9 w-full rounded-[14px] px-3 text-xs"
                    >
                      {settings.voiceAgentProfiles.map((profile) => (
                        <option key={profile.id} value={profile.id}>
                          {profile.voice
                            ? `${profile.displayName} (${profile.voice})`
                            : profile.displayName}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="flex flex-col gap-1.5">
                    <span className="text-[11px] font-medium text-[color:var(--sk-text)]">
                      Personal refinement prompt
                    </span>
                    <textarea
                      aria-label="Voice Agent personal refinement prompt"
                      value={settings.voiceAgentRefinementPrompt}
                      onChange={(e) =>
                        updateSettings(
                          { voiceAgentRefinementPrompt: e.target.value },
                          250,
                        )
                      }
                      rows={4}
                      placeholder="Prefer concise answers, call me Mako, and keep follow-up questions short."
                      className="sk-input min-h-24 w-full rounded-[18px] px-3 py-2 text-xs leading-6"
                    />
                    <span className="text-[11px] text-[color:var(--sk-text-muted)]/80">
                      This adds personal preferences on top of the internal
                      framework prompt without replacing it.
                    </span>
                  </label>
                </div>
                <Row
                  label="Session summary"
                  on={settings.voiceAgentSessionSummary}
                  onToggle={() =>
                    updateSettings({
                      voiceAgentSessionSummary:
                        !settings.voiceAgentSessionSummary,
                    })
                  }
                />
                <div className="flex flex-wrap gap-1.5">
                  <Chip
                    active={settings.voiceAgentCloseBehavior === "continue"}
                    onClick={() =>
                      updateSettings({ voiceAgentCloseBehavior: "continue" })
                    }
                  >
                    Minimise and keep chat
                  </Chip>
                  <Chip
                    active={settings.voiceAgentCloseBehavior === "new_chat"}
                    onClick={() =>
                      updateSettings({ voiceAgentCloseBehavior: "new_chat" })
                    }
                  >
                    End chat on close
                  </Chip>
                </div>
                <p className="text-[11px] text-[color:var(--sk-text-muted)]/80">
                  Use the close button to either park the current session or
                  reset it cleanly before the next start.
                </p>
              </div>
            </ModeSection>
          </div>
        </div>
      )}

      <ModelPanel
        modality={mode}
        settings={settings}
        showToast={showToast}
        providerTokens={providerTokens}
        setProviderTokens={setProviderTokens}
        providerBusy={providerBusy}
        tokenStatusLabel={tokenStatusLabel}
        onSaveCredential={onSaveCredential}
        onClearCredential={onClearCredential}
        onTestCredential={onTestCredential}
        onUpdateSelection={onUpdateSelection}
        onRefreshSettings={onRefreshSettings}
        dlCatalog={dlCatalog}
        setDlCatalog={setDlCatalog}
        dlJobs={dlJobs}
        setConfirmItem={setConfirmItem}
      />
    </>
  );
}
