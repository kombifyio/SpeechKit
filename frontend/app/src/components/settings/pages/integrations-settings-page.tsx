import type { Dispatch, SetStateAction } from "react";

import { IntegrationsSection } from "@/components/settings/providers-panel";
import { VoiceCompanionPanel } from "@/components/settings/voice-companion-panel";
import type {
  HomeAssistantSettings,
  PiperTTSSettings,
  ProviderCredentialState,
  SpeechKitSettingsState,
} from "@/lib/speechkit";

export function IntegrationsSettingsPage({
  settings,
  providerTokens,
  providerBusy,
  setProviderTokens,
  onToggleIntegration,
  onSaveCredential,
  tokenStatusLabel,
  updateSettings,
}: {
  settings: SpeechKitSettingsState;
  providerTokens: Record<string, string>;
  providerBusy: Record<string, boolean>;
  setProviderTokens: Dispatch<SetStateAction<Record<string, string>>>;
  onToggleIntegration: (provider: string, enabled: boolean) => void;
  onSaveCredential: (provider: string) => void;
  tokenStatusLabel: (cred: ProviderCredentialState) => string;
  updateSettings: (patch: Partial<SpeechKitSettingsState>, delay?: number) => void;
}) {
  return (
    <div className="grid grid-cols-1 gap-y-5 xl:grid-cols-2 xl:gap-x-10">
      <IntegrationsSection
        integrations={settings.providerIntegrations}
        credentials={settings.providerCredentials}
        providerTokens={providerTokens}
        providerBusy={providerBusy}
        setProviderTokens={setProviderTokens}
        onToggle={onToggleIntegration}
        onSaveCredential={onSaveCredential}
        tokenStatusLabel={tokenStatusLabel}
      />
      <VoiceCompanionPanel
        homeAssistant={settings.homeAssistant}
        piper={settings.piperTTS}
        onUpdateHomeAssistant={(next: HomeAssistantSettings) =>
          updateSettings({ homeAssistant: next })
        }
        onUpdatePiper={(next: PiperTTSSettings) =>
          updateSettings({ piperTTS: next })
        }
      />
    </div>
  );
}
