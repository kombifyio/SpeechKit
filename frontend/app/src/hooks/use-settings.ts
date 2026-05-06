import { useCallback } from "react";

import {
  useSettingsController,
  type SettingsTab,
} from "@/components/settings/use-settings-controller";
import {
  cancelModelDownload,
  startModelDownload,
  type DownloadItem,
} from "@/lib/speechkit";

import { providerCredentialCopy } from "@/components/settings/secrets-panel";

export { providerCredentialCopy };
export type { SettingsTab };

export function useSettings(showToast?: (message: string) => void) {
  const controller = useSettingsController({ onToast: showToast });

  const activateProfile = useCallback(
    async (modality: string, profileId: string, profileName: string) => {
      try {
        const body = new URLSearchParams({
          modality,
          profile_id: profileId,
        });
        const response = await fetch("/models/profiles/activate", {
          method: "POST",
          body,
        });
        if (!response.ok) {
          const errorText = await response.text();
          controller.showToast(errorText || "Switch failed");
          return;
        }
        controller.setSettings((previous) => ({
          ...previous,
          activeProfiles: {
            ...previous.activeProfiles,
            [modality]: profileId,
          },
        }));
        controller.showToast(`${profileName} activated`);
      } catch {
        controller.showToast("Switch failed");
      }
    },
    [controller],
  );

  const startDownload = useCallback(
    async (item: DownloadItem) => {
      controller.setDlBusy(true);
      try {
        const job = await startModelDownload(item.id);
        controller.setDlJobs((prev) => [
          ...prev.filter((existing) => existing.modelId !== item.id),
          job,
        ]);
        controller.setConfirmItem(null);
      } catch (err) {
        controller.showToast(
          err instanceof Error ? err.message : "Download failed",
        );
      } finally {
        controller.setDlBusy(false);
      }
    },
    [controller],
  );

  const cancelDownload = useCallback(async (jobId: string) => {
    await cancelModelDownload(jobId).catch(() => {});
  }, []);

  return {
    ...controller,
    activateProfile,
    startDownload,
    cancelDownload,
    providerCredentialCopy,
  };
}
