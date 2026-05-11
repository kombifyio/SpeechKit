import type { SettingsTab } from "@/components/settings-app";

export type DashboardTab = "dashboard" | "library" | "settings" | "logs";

export type DashboardToast = {
  id: number;
  message: string;
  type: "error" | "warn" | "success";
};

export type SetupWizardCompletion = {
  dashboardTab?: DashboardTab;
  settingsTab?: SettingsTab;
};
