import { MicSelector } from "@/components/ui/mic-selector";
import { Chip, Row, Section } from "@/components/settings/settings-primitives";
import type { ConfigurableMode } from "@/components/settings/settings-state";
import type { SpeechKitSettingsState } from "@/lib/speechkit";

const MODE_TOGGLE_ROWS: Array<{ mode: ConfigurableMode; label: string }> = [
  { mode: "dictate", label: "Transcribe Mode" },
  { mode: "assist", label: "Assist Mode" },
  { mode: "voice_agent", label: "Voice Agent Mode" },
];

export function GeneralSettingsPage({
  settings,
  updateSettings,
  toggleModeEnabled,
  hasSavedOverlaySpot,
  onSaveCurrentOverlaySpot,
  onResetOverlaySpot,
}: {
  settings: SpeechKitSettingsState;
  updateSettings: (patch: Partial<SpeechKitSettingsState>, delay?: number) => void;
  toggleModeEnabled: (mode: ConfigurableMode) => void;
  hasSavedOverlaySpot: boolean;
  onSaveCurrentOverlaySpot: () => Promise<void>;
  onResetOverlaySpot: () => Promise<void>;
}) {
  return (
    <div className="grid grid-cols-1 gap-y-5 xl:grid-cols-2 xl:gap-x-10">
      <div className="flex flex-col gap-5">
        <Section title="Startup">
          <Row
            label="Auto-start on app launch"
            on={settings.autoStartOnLaunch}
            onToggle={() =>
              updateSettings({
                autoStartOnLaunch: !settings.autoStartOnLaunch,
              })
            }
          />
          <p className="mt-2 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]/80">
            Starts the configured launch session automatically when SpeechKit
            opens. Keep mode-specific controls on their own tab.
          </p>
        </Section>

        <Section title="Mode Settings" testId="general-mode-settings">
          <div className="flex flex-col gap-3">
            {MODE_TOGGLE_ROWS.map((row) => (
              <Row
                key={row.mode}
                label={row.label}
                on={settings.modeEnabled[row.mode]}
                onToggle={() => toggleModeEnabled(row.mode)}
              />
            ))}
          </div>
        </Section>

        <Section title="Microphone">
          <MicSelector
            value={settings.selectedAudioDeviceId}
            onValueChange={(deviceId) =>
              updateSettings({ selectedAudioDeviceId: deviceId })
            }
            className="w-full"
          />
        </Section>
      </div>

      <div className="flex flex-col gap-5">
        <Section title="Overlay">
          <Row
            label="Show overlay"
            on={settings.overlayEnabled}
            onToggle={() =>
              updateSettings({ overlayEnabled: !settings.overlayEnabled })
            }
          />
          {settings.overlayEnabled && (
            <div className="mt-2 flex flex-col gap-2">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="mr-1 text-[11px] text-[color:var(--sk-text-muted)]">
                  Style
                </span>
                <Chip
                  active={settings.visualizer === "pill"}
                  onClick={() => updateSettings({ visualizer: "pill" })}
                >
                  Default{" "}
                  <span className="ml-1 text-[10px] opacity-50">(Pill)</span>
                </Chip>
                <Chip
                  active={settings.visualizer === "circle"}
                  onClick={() => updateSettings({ visualizer: "circle" })}
                >
                  Focus{" "}
                  <span className="ml-1 text-[10px] opacity-50">(Dot)</span>
                </Chip>
                {settings.visualizer === "pill" && (
                  <>
                    <span className="mx-1 text-[color:var(--sk-border)]">|</span>
                    <Chip
                      active={settings.design === "default"}
                      onClick={() => updateSettings({ design: "default" })}
                    >
                      Default
                    </Chip>
                    <Chip
                      active={settings.design === "kombify"}
                      onClick={() => updateSettings({ design: "kombify" })}
                    >
                      kombify
                    </Chip>
                  </>
                )}
              </div>
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="mr-1 text-[11px] text-[color:var(--sk-text-muted)]">
                  Position
                </span>
                {(["top", "bottom", "left", "right"] as const).map((pos) => (
                  <Chip
                    key={pos}
                    active={settings.overlayPosition === pos}
                    onClick={() => updateSettings({ overlayPosition: pos })}
                  >
                    {pos.charAt(0).toUpperCase() + pos.slice(1)}
                  </Chip>
                ))}
              </div>
              <Row
                label="Movable overlay"
                on={settings.overlayMovable}
                onToggle={() =>
                  updateSettings({
                    overlayMovable: !settings.overlayMovable,
                  })
                }
              />
              {settings.overlayMovable && (
                <div className="flex flex-col gap-2">
                  {settings.visualizer === "pill" && (
                    <p className="text-[11px] text-[color:var(--sk-text-muted)]/80">
                      Drag the center bubble inside the pill panel to place it
                      anywhere on the desktop.
                    </p>
                  )}
                  <div className="rounded-[18px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] px-3 py-2.5">
                    <p className="text-[11px] text-[color:var(--sk-text-muted)]">
                      {hasSavedOverlaySpot
                        ? `Saved spot: X ${settings.overlayFreeX}, Y ${settings.overlayFreeY}`
                        : "No custom spot saved yet. The overlay falls back to the selected edge until you save the current spot."}
                    </p>
                    <div className="mt-2 flex flex-wrap gap-2">
                      <button
                        type="button"
                        onClick={() => void onSaveCurrentOverlaySpot()}
                        className="sk-secondary-button rounded-full px-3 py-1.5 text-[11px] font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
                      >
                        Save current spot
                      </button>
                      <button
                        type="button"
                        onClick={() => void onResetOverlaySpot()}
                        disabled={!hasSavedOverlaySpot}
                        className={[
                          "rounded-full px-3 py-1.5 text-[11px] font-medium transition-colors",
                          hasSavedOverlaySpot
                            ? "sk-secondary-button hover:bg-[color:var(--sk-surface-3)]"
                            : "cursor-not-allowed border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] text-[color:var(--sk-text-subtle)]",
                        ].join(" ")}
                      >
                        Reset saved spot
                      </button>
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}
        </Section>
      </div>
    </div>
  );
}
