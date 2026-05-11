import { useEffect, type ReactNode } from "react";
import {
  AudioLines,
  Bot,
  Database,
  Headphones,
  Plug,
  ServerCog,
  SlidersHorizontal,
  type LucideIcon,
} from "lucide-react";

import { DownloadConfirmationDialog } from "@/components/settings/download-confirmation-dialog";
import { GeneralSettingsPage } from "@/components/settings/pages/general-settings-page";
import type { ReleaseFeatureFlags } from "@/lib/speechkit/release-flags";
import { IntegrationsSettingsPage } from "@/components/settings/pages/integrations-settings-page";
import { ModeSettingsPage } from "@/components/settings/pages/mode-settings-page";
import { SpeechKitServerSettingsPage } from "@/components/settings/pages/speechkit-server-settings-page";
import { StorageSettingsPage } from "@/components/settings/pages/storage-settings-page";
import {
  useSettingsController,
  type SettingsTab,
} from "@/components/settings/use-settings-controller";
import {
  MODE_SELECTION_KEYS,
  type ConfigurableMode,
} from "@/components/settings/settings-state";

type ModeTab = "stt" | "assist" | "realtime_voice";
type Tab = SettingsTab;
export type { SettingsTab };

type NavTab = {
  value: Tab;
  label: string;
  icon: LucideIcon;
  iconKey: string;
};

const GENERAL_NAV_TAB: NavTab = {
  value: "general",
  label: "General",
  icon: SlidersHorizontal,
  iconKey: "sliders-horizontal",
};

const AUDIO_NAV_TABS: NavTab[] = [
  { value: "integrations", label: "Integrations", icon: Plug, iconKey: "plug" },
  {
    value: "speechkit_server",
    label: "SpeechKit Server",
    icon: ServerCog,
    iconKey: "server-cog",
  },
  {
    value: "storage",
    label: "Storage & Data",
    icon: Database,
    iconKey: "database",
  },
];

const MODE_NAV_TABS: Array<NavTab & { value: ModeTab }> = [
  {
    value: "stt",
    label: "Transcribe Mode",
    icon: AudioLines,
    iconKey: "audio-lines",
  },
  { value: "assist", label: "Assist Mode", icon: Bot, iconKey: "bot" },
  {
    value: "realtime_voice",
    label: "Voice Agent Mode",
    icon: Headphones,
    iconKey: "headphones",
  },
];

const MODE_TAB_TO_MODE: Record<ModeTab, ConfigurableMode> =
  MODE_SELECTION_KEYS;

function isModeTab(tab: Tab): tab is ModeTab {
  return tab === "stt" || tab === "assist" || tab === "realtime_voice";
}

function modeForTab(tab: ModeTab): ConfigurableMode {
  return MODE_TAB_TO_MODE[tab];
}

export function SettingsApp({
  initialTab = "general",
  features,
}: {
  initialTab?: Tab;
  features?: ReleaseFeatureFlags;
}) {
  const {
    settings,
    providerTokens,
    setProviderTokens,
    providerBusy,
    toast,
    tab,
    setTab,
    dlCatalog,
    setDlCatalog,
    dlJobs,
    setDlJobs,
    confirmItem,
    setConfirmItem,
    dlBusy,
    setDlBusy,
    loadSettings,
    showToast,
    updateSettings,
    updateModeHotkey,
    updateModeHotkeyBehavior,
    updateModelSelection,
    toggleModeEnabled,
    tokenStatusLabel,
    postgresReady,
    handleSaveProviderCredential,
    handleClearProviderCredential,
    handleTestProviderCredential,
    handleUpdateProviderIntegration,
    handleSaveCurrentOverlaySpot,
    handleResetOverlaySpot,
    hasSavedOverlaySpot,
    handleChooseStorageFolder,
  } = useSettingsController({ initialTab });
  const modeEnabled = settings.modeEnabled;

  useEffect(() => {
    if (isModeTab(tab) && !modeEnabled[modeForTab(tab)]) {
      setTab("general");
    }
  }, [modeEnabled, setTab, tab]);

  const activeModeTab = isModeTab(tab) && modeEnabled[modeForTab(tab)] ? tab : null;

  const renderNavTab = (navTab: NavTab) => {
    const Icon = navTab.icon;
    const disabled =
      isModeTab(navTab.value) && !modeEnabled[modeForTab(navTab.value)];
    return (
      <TabBtn
        key={navTab.value}
        active={tab === navTab.value && !disabled}
        disabled={disabled}
        onClick={() => setTab(navTab.value)}
      >
        <span className="flex min-w-0 items-center gap-2">
          <Icon
            className="h-3.5 w-3.5 shrink-0"
            aria-hidden="true"
            data-testid={
              isModeTab(navTab.value)
                ? `settings-mode-nav-icon-${navTab.value}`
                : `settings-nav-icon-${navTab.value}`
            }
            data-icon={navTab.iconKey}
          />
          <span className="truncate">{navTab.label}</span>
        </span>
      </TabBtn>
    );
  };

  return (
    <div
      data-testid="settings-layout"
      className="flex h-full min-h-0 min-w-0 bg-(--sk-surface-0) text-[13px] text-(--sk-text)"
    >
      <div
        data-testid="settings-nav-panel"
        className="w-56 shrink-0 overflow-y-auto border-r border-(--sk-shell-divider) bg-(--sk-surface-1) px-3 py-6"
      >
        <h2 className="mb-5 px-3 text-xs font-bold uppercase tracking-widest text-(--sk-text-muted)">
          Settings
        </h2>
        <nav className="space-y-4">
          {renderNavTab(GENERAL_NAV_TAB)}

          <NavGroup title="Audio Settings" testId="settings-nav-group-audio">
            {AUDIO_NAV_TABS.map(renderNavTab)}
          </NavGroup>

          <NavGroup title="Mode Settings" testId="settings-nav-group-mode">
            {MODE_NAV_TABS.map(renderNavTab)}
          </NavGroup>
        </nav>
      </div>

      <div
        data-testid="settings-scroll-region"
        className="min-h-0 flex-1 overflow-y-auto px-8 py-6"
      >
        {tab === "general" && (
          <GeneralSettingsPage
            settings={settings}
            updateSettings={updateSettings}
            toggleModeEnabled={toggleModeEnabled}
            hasSavedOverlaySpot={hasSavedOverlaySpot}
            onSaveCurrentOverlaySpot={handleSaveCurrentOverlaySpot}
            onResetOverlaySpot={handleResetOverlaySpot}
            features={features}
          />
        )}
        {tab === "integrations" && (
          <IntegrationsSettingsPage
            settings={settings}
            providerTokens={providerTokens}
            providerBusy={providerBusy}
            setProviderTokens={setProviderTokens}
            onToggleIntegration={handleUpdateProviderIntegration}
            onSaveCredential={handleSaveProviderCredential}
            tokenStatusLabel={tokenStatusLabel}
          />
        )}
        {tab === "speechkit_server" && <SpeechKitServerSettingsPage />}
        {tab === "storage" && (
          <StorageSettingsPage
            settings={settings}
            postgresReady={postgresReady}
            updateSettings={updateSettings}
            onChooseStorageFolder={handleChooseStorageFolder}
          />
        )}
        {activeModeTab && (
          <ModeSettingsPage
            mode={activeModeTab}
            settings={settings}
            updateSettings={updateSettings}
            toggleModeEnabled={toggleModeEnabled}
            updateModeHotkey={updateModeHotkey}
            updateModeHotkeyBehavior={updateModeHotkeyBehavior}
            showToast={showToast}
            providerTokens={providerTokens}
            setProviderTokens={setProviderTokens}
            providerBusy={providerBusy}
            tokenStatusLabel={tokenStatusLabel}
            onSaveCredential={handleSaveProviderCredential}
            onClearCredential={handleClearProviderCredential}
            onTestCredential={handleTestProviderCredential}
            onUpdateSelection={updateModelSelection}
            onRefreshSettings={loadSettings}
            dlCatalog={dlCatalog}
            setDlCatalog={setDlCatalog}
            dlJobs={dlJobs}
            setConfirmItem={setConfirmItem}
          />
        )}

        <div
          className={[
            "pointer-events-none fixed top-4 right-4 rounded-lg border border-emerald-400/20 bg-emerald-500/10 px-3 py-1.5 text-xs text-emerald-300 transition-all",
            toast ? "translate-y-0 opacity-100" : "-translate-y-2 opacity-0",
          ].join(" ")}
        >
          {toast || "\u00A0"}
        </div>
        <DownloadConfirmationDialog
          confirmItem={confirmItem}
          dlBusy={dlBusy}
          setConfirmItem={setConfirmItem}
          setDlBusy={setDlBusy}
          setDlJobs={setDlJobs}
          showToast={showToast}
        />
      </div>
    </div>
  );
}

function NavGroup({
  title,
  testId,
  children,
}: {
  title: string;
  testId: string;
  children: ReactNode;
}) {
  return (
    <div data-testid={testId} className="pt-1">
      <div className="px-3">
        <div className="border-b border-(--sk-shell-divider)/80 pb-1.5">
          <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-(--sk-text-muted)">
            {title}
          </span>
        </div>
      </div>
      <div className="mt-1.5 space-y-0.5 pl-3">{children}</div>
    </div>
  );
}

function TabBtn({
  active,
  disabled = false,
  onClick,
  children,
}: {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={[
        "w-full rounded-2xl px-3 py-2.5 text-left text-sm transition-all",
        active
          ? "border border-(--sk-accent)/18 bg-(--sk-accent-soft) text-(--sk-accent) font-semibold"
          : disabled
            ? "cursor-not-allowed border border-transparent text-(--sk-text-subtle) opacity-45"
            : "border border-transparent text-(--sk-text-muted) hover:border-(--sk-panel-border) hover:bg-(--sk-surface-2) hover:text-(--sk-text)",
      ].join(" ")}
    >
      {children}
    </button>
  );
}
