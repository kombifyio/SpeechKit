import { WakewordPanel } from "@/components/settings/wakeword-panel";
import { WakewordTrainingPanel } from "@/components/settings/wakeword-training-panel";
import type { SpeechKitSettingsState } from "@/lib/speechkit";

// Wake-word settings moved out of General → its own page under Audio
// Settings (v0.35.21). v0.37.6 adds the Training-data panel below the
// existing detector configuration so users can browse, label, and
// delete locally captured activation clips without leaving the SpeechKit
// window.
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
      <WakewordTrainingPanel />
    </div>
  );
}
