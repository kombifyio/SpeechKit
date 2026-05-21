import { WakewordPanel } from "@/components/settings/wakeword-panel";
import type { SpeechKitSettingsState } from "@/lib/speechkit";

// Wake-word settings moved out of General → its own page under Audio
// Settings (v0.35.21). The panel itself has not changed — it just lives
// in a dedicated tab now because the General page was overloaded.
export function WakewordSettingsPage({
  settings,
  updateSettings,
}: {
  settings: SpeechKitSettingsState;
  updateSettings: (
    patch: Partial<SpeechKitSettingsState>,
    delay?: number,
  ) => void;
}) {
  return (
    <div className="grid grid-cols-1 gap-y-5">
      <WakewordPanel
        settings={settings}
        onChange={(next) => updateSettings({ wakeword: next.wakeword })}
      />
    </div>
  );
}
