import type { Dispatch, SetStateAction } from "react";

import {
  MODE_SELECTION_KEYS,
} from "@/components/settings/settings-state";
import {
  providerCredentialCopy,
  providerSecretNoun,
} from "@/components/settings/secrets-panel";
import {
  builtInPrimaryModelSelections,
  cancelModelDownload,
  fetchDownloadCatalog,
  selectDownloadedModel,
  type DownloadItem,
  type DownloadJob,
  type ModeModelSelectionState,
  type ProviderCredentialState,
  type ProviderKind,
  type SpeechKitSettingsState,
} from "@/lib/speechkit";

const PROVIDER_FOR_EXECUTION_MODE: Record<string, string | undefined> = {
  hf_routed: "huggingface",
  hf_inference: "huggingface",
  openai_api: "openai",
  groq_api: "groq",
  google_api: "google",
  ollama_local: "ollama",
  openrouter_api: "openrouter",
};

const PROVIDER_KIND_ORDER: ProviderKind[] = [
  "local_built_in",
  "local_provider",
  "cloud_provider",
  "direct_provider",
];
const PROVIDER_KIND_LABELS: Record<ProviderKind, string> = {
  local_built_in: "Local Built-in",
  local_provider: "Local Provider",
  cloud_provider: "Cloud Router / Gateway",
  direct_provider: "Direct Provider",
};

export type ModalityTabKey = "stt" | "assist" | "realtime_voice";
type SettingsModelProfile = NonNullable<
  SpeechKitSettingsState["profiles"]
>[number];

function providerKindForProfile(profile: SettingsModelProfile): ProviderKind {
  if (profile.providerKind) {
    return profile.providerKind;
  }
  switch (profile.executionMode) {
    case "local":
      return "local_built_in";
    case "ollama_local":
    case "self_hosted_http":
      return "local_provider";
    case "hf_routed":
    case "hf_inference":
      return "cloud_provider";
    default:
      return "direct_provider";
  }
}

function profileVisibleForIntegration(
  profile: SettingsModelProfile,
  settings: SpeechKitSettingsState,
) {
  if (providerKindForProfile(profile) === "local_built_in") {
    return true;
  }
  const providerKey = profile.executionMode
    ? PROVIDER_FOR_EXECUTION_MODE[profile.executionMode]
    : undefined;
  if (!providerKey) {
    return true;
  }
  const integrations = settings.providerIntegrations;
  if (!integrations) {
    return true;
  }
  return integrations[providerKey]?.enabled === true;
}

function sourceBadge(profile: SettingsModelProfile) {
  switch (profile.executionMode) {
    case "local":
      return {
        label: "built-in",
        className:
          "bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-muted)]",
      };
    case "ollama_local":
      return {
        label: "provider",
        className:
          "bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-muted)]",
      };
    case "hf_routed":
    case "hf_inference":
      return {
        label: "hugging face",
        className:
          "bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-muted)]",
      };
    default:
      return {
        label: "api key",
        className:
          "bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-muted)]",
      };
  }
}

function normalizeModeSelectionUpdate(
  current: ModeModelSelectionState,
  field: keyof ModeModelSelectionState,
  value: string,
) {
  const next = {
    ...current,
    [field]: value.trim(),
  };

  if (
    next.primaryProfileId !== "" &&
    next.primaryProfileId === next.fallbackProfileId
  ) {
    next.fallbackProfileId = "";
  }

  return next;
}

function resolvePrimaryProfileId(
  selectionMode: keyof SpeechKitSettingsState["modelSelections"],
  current: ModeModelSelectionState,
  activeId: string | undefined,
  profiles: SettingsModelProfile[],
) {
  const currentPrimary = current.primaryProfileId.trim();
  if (
    currentPrimary &&
    profiles.some((profile) => profile.id === currentPrimary)
  ) {
    return currentPrimary;
  }

  const builtInPrimary =
    builtInPrimaryModelSelections[selectionMode]?.primaryProfileId ?? "";
  if (
    builtInPrimary &&
    profiles.some((profile) => profile.id === builtInPrimary)
  ) {
    return builtInPrimary;
  }

  if (activeId && profiles.some((profile) => profile.id === activeId)) {
    return activeId;
  }

  return (
    profiles.find((profile) => profile.recommended)?.id ??
    profiles.find((profile) =>
      profile.variants?.some((variant) => variant.recommended),
    )?.id ??
    profiles[0]?.id ??
    currentPrimary
  );
}

export function ModelPanel({
  modality,
  settings,
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
  modality: ModalityTabKey;
  settings: SpeechKitSettingsState;
  showToast: (msg: string) => void;
  providerTokens: Record<string, string>;
  setProviderTokens: Dispatch<SetStateAction<Record<string, string>>
  >;
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
  const profiles = settings.profiles ?? [];
  const filtered = profiles
    .filter((p) => p.modality === modality)
    .filter((profile) => profileVisibleForIntegration(profile, settings));
  const activeId = settings.activeProfiles?.[modality];
  const selectionMode = MODE_SELECTION_KEYS[modality];
  const rawSelection = settings.modelSelections[selectionMode];
  const currentSelection = {
    ...rawSelection,
    primaryProfileId: resolvePrimaryProfileId(
      selectionMode,
      rawSelection,
      activeId,
      filtered,
    ),
  };
  const providerGroups = PROVIDER_KIND_ORDER.map((kind) => ({
    kind,
    label: PROVIDER_KIND_LABELS[kind],
    profiles: filtered.filter(
      (profile) => providerKindForProfile(profile) === kind,
    ),
  }));

  return (
    <>
      {filtered.length > 0 ? (
        <section
          data-testid="model-routing-controls"
          className="mb-4 border-b border-[color:var(--sk-shell-divider)] pb-4"
        >
          <div className="mb-3 flex items-end justify-between gap-3">
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
                Primary & fallback
              </p>
              <p className="mt-1 text-[11px] text-[color:var(--sk-text-muted)]">
                Choose the default model before tuning provider details below.
              </p>
            </div>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <div
              data-testid={
                modality === "assist" ? "assist-llm-selection" : undefined
              }
            >
              <SelectionField
                label={modality === "assist" ? "Assist LLM" : "Primary model"}
                value={currentSelection.primaryProfileId}
                options={filtered}
                onChange={(value) =>
                  onUpdateSelection(
                    selectionMode,
                    normalizeModeSelectionUpdate(
                      currentSelection,
                      "primaryProfileId",
                      value,
                    ),
                  )
                }
              />
            </div>
            <SelectionField
              label={
                modality === "assist" ? "Fallback Assist LLM" : "Fallback model"
              }
              value={currentSelection.fallbackProfileId}
              options={filtered}
              onChange={(value) =>
                onUpdateSelection(
                  selectionMode,
                  normalizeModeSelectionUpdate(
                    currentSelection,
                    "fallbackProfileId",
                    value,
                  ),
                )
              }
              allowEmptyLabel="No fallback"
            />
          </div>
        </section>
      ) : null}

      <div className="overflow-hidden rounded-[24px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)]">
        {/* Panel header */}
        <div className="flex items-center gap-3 border-b border-[color:var(--sk-shell-divider)] bg-[color:var(--sk-surface-2)] px-4 py-2.5">
          <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
            Model setup
          </span>
          <span className="text-[11px] text-[color:var(--sk-text-subtle)]">
            {modality === "stt"
              ? "Speech-to-Text"
              : modality === "assist"
                ? "Assist LLM"
                : "Voice Agent"}
          </span>
        </div>

        {filtered.length === 0 ? (
          <p className="px-4 py-4 text-[11px] text-[color:var(--sk-text-muted)]">
            No live-switchable model profiles are exposed in this build.
          </p>
        ) : (
          <div className="space-y-3 p-3">
            {providerGroups.map((group) => (
              <section
                key={group.kind}
                data-testid={`model-provider-group-${group.kind}`}
                className="overflow-hidden rounded-xl border border-[color:var(--sk-shell-divider)] bg-[color:var(--sk-surface-0)]/45"
              >
                <div className="flex items-center justify-between gap-3 border-b border-[color:var(--sk-shell-divider)] px-4 py-2">
                  <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
                    {group.label}
                  </span>
                  <span className="text-[10px] text-[color:var(--sk-text-muted)]">
                    {group.profiles.length}{" "}
                    {group.profiles.length === 1 ? "model" : "models"}
                  </span>
                </div>
                {group.profiles.length === 0 ? (
                  <p className="px-4 py-3 text-[11px] text-[color:var(--sk-text-muted)]">
                    No supported model in this provider group.
                  </p>
                ) : (
                  <div className="divide-y divide-[color:var(--sk-shell-divider)]">
                    {group.profiles.map((profile) => {
              const isActive = activeId === profile.id;
              const isPrimarySelected =
                currentSelection.primaryProfileId === profile.id;
              const isFallbackSelected =
                currentSelection.fallbackProfileId === profile.id;
              const badge = sourceBadge(profile);
              const providerKey = profile.executionMode
                ? PROVIDER_FOR_EXECUTION_MODE[profile.executionMode]
                : undefined;
              const providerIntegration = providerKey
                ? settings.providerIntegrations?.[providerKey]
                : undefined;
              const providerCredential = providerKey
                ? settings.providerCredentials?.[providerKey]
                : undefined;
              const providerRequiresCredential = Boolean(
                providerIntegration?.credentialRequired ?? providerCredential,
              );
              const providerMissing = Boolean(
                providerKey &&
                  providerRequiresCredential &&
                  !providerCredential?.available,
              );
              const providerCopy = providerCredential
                ? providerCredentialCopy(profile.name, providerCredential)
                : null;
              const providerReady = Boolean(
                providerKey && providerCredential?.available,
              );
              const providerIsBusy = providerKey
                ? (providerBusy[providerKey] ?? false)
                : false;
              const downloadItems = dlCatalog.filter(
                (item) => item.profileId === profile.id,
              );
              const localRuntimeIssue = downloadItems.find(
                (item) =>
                  item.kind === "http" &&
                  (item.runtimeReady === false ||
                    Boolean(item.runtimeProblem)),
              );
              const localRuntimeProblem = localRuntimeIssue?.runtimeProblem;
              const localRuntimeMissing = Boolean(localRuntimeIssue);
              const downloadActive = downloadItems.some((item) => {
                const job = dlJobs.find(
                  (candidate) => candidate.modelId === item.id,
                );
                return job?.status === "pending" || job?.status === "running";
              });
              const downloadReady = downloadItems.some((item) => {
                const job = dlJobs.find(
                  (candidate) => candidate.modelId === item.id,
                );
                return item.available || job?.status === "done";
              });
              const needsDownload =
                downloadItems.length > 0 && !downloadReady && !downloadActive;
              const readyToUse =
                !providerMissing &&
                !needsDownload &&
                !downloadActive &&
                !localRuntimeMissing;
              const statusLabel = isPrimarySelected
                ? "Primary"
                : isFallbackSelected
                  ? "Fallback"
                  : providerMissing
                    ? (providerCopy?.neededLabel ??
                      `${providerCredential?.label ?? "API"} key needed`)
                    : localRuntimeMissing
                      ? "Runtime missing"
                      : downloadActive
                        ? "Downloading"
                        : needsDownload
                          ? "Download required"
                          : "Ready";
              const statusClassName =
                isPrimarySelected || isFallbackSelected || isActive
                  ? "border-[color:var(--sk-accent)]/25 bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]"
                  : localRuntimeMissing
                    ? "border-amber-500/25 bg-amber-500/10 text-amber-200/80"
                    : "border-[color:var(--sk-border)] bg-transparent text-[color:var(--sk-text-muted)]";

              return (
                <div
                  key={profile.id}
                  className={
                    isPrimarySelected || isFallbackSelected || isActive
                      ? "bg-[color:var(--sk-accent)]/4"
                      : undefined
                  }
                >
                  {/* Main identity row */}
                  <div className="flex items-center gap-3 px-4 py-3">
                    <div
                      className={[
                        "size-2 shrink-0 rounded-full",
                        isPrimarySelected || isFallbackSelected || isActive
                          ? "bg-[color:var(--sk-accent)]"
                          : "bg-[color:var(--sk-border)]",
                      ].join(" ")}
                    />
                    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
                      <span
                        className={[
                          "text-[13px] font-medium",
                          isPrimarySelected || isFallbackSelected || isActive
                            ? "text-[color:var(--sk-accent)]"
                            : "text-[color:var(--sk-text)]/85",
                        ].join(" ")}
                      >
                        {profile.name}
                      </span>
                      <span
                        className={[
                          "shrink-0 rounded px-1.5 py-px text-[9px]",
                          badge.className,
                        ].join(" ")}
                      >
                        {badge.label}
                      </span>
                      <span className="text-[10px] text-[color:var(--sk-text-muted)]/70">
                        {profile.source ?? profile.executionMode ?? "local"}
                      </span>
                      {profile.description && (
                        <span className="truncate text-[11px] text-[color:var(--sk-text-muted)]/80">
                          {profile.description}
                        </span>
                      )}
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <span
                        className={[
                          "rounded-full border px-2 py-0.5 text-[10px]",
                          statusClassName,
                        ].join(" ")}
                      >
                        {statusLabel}
                      </span>
                      {isPrimarySelected ? (
                        <span className="w-24 text-right text-[11px] font-medium text-[color:var(--sk-accent)]/80">
                          Primary model
                        </span>
                      ) : isFallbackSelected ? (
                        <span className="w-24 text-right text-[11px] font-medium text-[color:var(--sk-accent)]/80">
                          Fallback model
                        </span>
                      ) : readyToUse ? (
                        <span className="w-24 text-right text-[10px] text-[color:var(--sk-text-muted)]">
                          Selectable below
                        </span>
                      ) : (
                        <span className="w-24 text-right text-[10px] text-[color:var(--sk-text-muted)]">
                          {providerMissing
                            ? (providerCopy?.unlockLabel ??
                              "Add the key above.")
                            : localRuntimeMissing
                              ? "Runtime missing."
                              : needsDownload
                                ? "Download required."
                                : "Downloading…"}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Provider missing: inline key entry */}
                  {providerMissing && providerKey && providerCredential && (
                    <div className="flex items-center gap-2 border-t border-amber-500/10 bg-amber-500/4 px-4 py-2">
                      <span className="shrink-0 text-[10px] font-medium text-amber-200/60">
                        {providerCopy?.title ??
                          `Add ${providerCredential.label} key`}
                      </span>
                      <input
                        aria-label={
                          providerCopy?.inputLabel ?? `${profile.name} API key`
                        }
                        type="password"
                        value={providerTokens[providerKey] ?? ""}
                        onChange={(e) =>
                          setProviderTokens((tokens) => ({
                            ...tokens,
                            [providerKey]: e.target.value,
                          }))
                        }
                        placeholder={
                          providerCopy?.placeholder ??
                          (providerCredential.envName || "API key")
                        }
                        className="h-7 flex-1 rounded border border-amber-500/15 bg-black/20 px-2.5 text-[11px] text-[#e4e1e9]/80 outline-none focus:border-[#947dff]/50"
                      />
                      <button
                        type="button"
                        onClick={() => onSaveCredential(providerKey)}
                        disabled={providerIsBusy}
                        className={[
                          "shrink-0 rounded border px-3 py-1 text-[11px] font-medium",
                          providerIsBusy
                            ? "border-[#484555] bg-[#35343a] text-[#938ea1]"
                            : "border-[#947dff]/25 bg-[#947dff]/15 text-[#cabeff] hover:bg-[#947dff]/25",
                        ].join(" ")}
                      >
                        {providerCopy?.saveLabel ?? "Save key"}
                      </button>
                    </div>
                  )}

                  {/* Provider ready: token management row */}
                  {providerReady && providerKey && providerCredential && (
                    <div className="flex items-center gap-2 border-t border-[color:var(--sk-shell-divider)] bg-[color:var(--sk-surface-0)]/65 px-4 py-2">
                      <span className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)]">
                        {tokenStatusLabel(providerCredential)}
                      </span>
                      <input
                        type="password"
                        aria-label={`Update ${providerCredential.label} ${providerSecretNoun(providerCredential.provider)}`}
                        placeholder={`Update ${providerSecretNoun(providerCredential.provider)}…`}
                        value={providerTokens[providerKey] ?? ""}
                        onChange={(e) =>
                          setProviderTokens((tokens) => ({
                            ...tokens,
                            [providerKey]: e.target.value,
                          }))
                        }
                        className="sk-input h-6 min-w-0 flex-1 rounded px-2 text-[11px]"
                      />
                      {(providerTokens[providerKey] ?? "").trim().length >
                        0 && (
                        <button
                          type="button"
                          onClick={() => onSaveCredential(providerKey)}
                          disabled={providerIsBusy}
                          className="shrink-0 rounded px-2 py-0.5 text-[10px] text-[color:var(--sk-accent)]/80 hover:text-[color:var(--sk-accent)]"
                        >
                          Save
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => onTestCredential(providerKey)}
                        disabled={providerIsBusy}
                        className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)] hover:text-[color:var(--sk-text)]"
                      >
                        Test
                      </button>
                      {providerCredential.hasStoredSecret && (
                        <button
                          type="button"
                          onClick={() => onClearCredential(providerKey)}
                          disabled={providerIsBusy}
                          className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)] hover:text-red-300/75"
                        >
                          Clear
                        </button>
                      )}
                    </div>
                  )}

                  {localRuntimeMissing && (
                    <div className="flex items-center gap-2 border-t border-amber-500/10 bg-amber-500/4 px-4 py-2">
                      <span className="text-[10px] leading-relaxed text-amber-200/75">
                        {localRuntimeProblem}
                      </span>
                    </div>
                  )}

                  {/* Download items */}
                  {downloadItems.length > 0 && (
                    <div className="space-y-1.5 border-t border-[color:var(--sk-shell-divider)] px-4 py-2">
                      {downloadItems.map((item) => {
                        const itemJob = dlJobs.find(
                          (candidate) => candidate.modelId === item.id,
                        );
                        const itemDownloadActive =
                          itemJob?.status === "pending" ||
                          itemJob?.status === "running";
                        const itemDownloadReady = Boolean(
                          item.available || itemJob?.status === "done",
                        );
                        const itemSelected = Boolean(item.selected);
                        const itemRuntimeMissing =
                          itemDownloadReady &&
                          (item.runtimeReady === false ||
                            Boolean(item.runtimeProblem));

                        return (
                          <div
                            key={item.id}
                            className="flex flex-wrap items-center gap-2"
                          >
                            <span className="text-[11px] text-[color:var(--sk-text)]">
                              {item.name}
                            </span>
                            {item.recommended && (
                              <span className="rounded bg-[color:var(--sk-accent-soft)] px-1.5 py-px text-[9px] text-[color:var(--sk-accent)]/80">
                                recommended
                              </span>
                            )}
                            <span className="text-[10px] text-[color:var(--sk-text-muted)]">
                              {item.sizeLabel}
                            </span>
                            {itemDownloadActive ? (
                              <>
                                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-[color:var(--sk-surface-2)]">
                                  <div
                                    className="h-full rounded-full bg-[color:var(--sk-accent)]/70 transition-all duration-500"
                                    style={{
                                      width: `${Math.round((itemJob?.progress ?? 0) * 100)}%`,
                                    }}
                                  />
                                </div>
                                <span className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)]">
                                  {itemJob?.statusText}
                                </span>
                                <button
                                  type="button"
                                  onClick={() => {
                                    if (itemJob)
                                      cancelModelDownload(itemJob.id).catch(
                                        () => {},
                                      );
                                  }}
                                  className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)] hover:text-red-300/75"
                                >
                                  Cancel download
                                </button>
                              </>
                            ) : itemDownloadReady ? (
                              itemSelected ? (
                                <span className="text-[10px] text-emerald-300/80">
                                  Selected on this device
                                </span>
                              ) : itemRuntimeMissing ? (
                                <>
                                  <span className="text-[10px] text-amber-300/80">
                                    Model ready on this device
                                  </span>
                                  <span className="text-[10px] text-amber-200/70">
                                    {item.runtimeProblem ??
                                      "Local runtime missing."}
                                  </span>
                                </>
                              ) : (
                                <>
                                  <span className="text-[10px] text-emerald-300/80">
                                    Ready on this device
                                  </span>
                                  <button
                                    type="button"
                                    aria-label={`Use ${item.name}`}
                                    onClick={async () => {
                                      try {
                                        const result =
                                          await selectDownloadedModel(item.id);
                                        const [, freshCatalog] =
                                          await Promise.all([
                                            onRefreshSettings(),
                                            fetchDownloadCatalog(),
                                          ]);
                                        setDlCatalog(freshCatalog);
                                        showToast(
                                          result.message ??
                                            `${item.name} selected`,
                                        );
                                      } catch (error) {
                                        showToast(
                                          error instanceof Error
                                            ? error.message
                                            : "Switch failed",
                                        );
                                      }
                                    }}
                                    className="rounded border border-[color:var(--sk-border)] px-2 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-accent)]"
                                  >
                                    Use model
                                  </button>
                                </>
                              )
                            ) : (
                              <button
                                type="button"
                                onClick={() => setConfirmItem(item)}
                                className="rounded border border-[color:var(--sk-border)] px-2 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-accent)]"
                              >
                                Download
                              </button>
                            )}
                            {itemJob?.status === "failed" && (
                              <span className="ml-1 text-[10px] text-red-400/70">
                                {itemJob.error ?? "Download failed"}
                              </span>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
                    })}
                  </div>
                )}
              </section>
            ))}
          </div>
        )}
      </div>

    </>
  );
}

function SelectionField({
  label,
  value,
  options,
  onChange,
  allowEmptyLabel,
  hint,
}: {
  label: string;
  value: string;
  options: NonNullable<SpeechKitSettingsState["profiles"]>;
  onChange: (value: string) => void;
  allowEmptyLabel?: string;
  hint?: string;
}) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-[11px] font-medium text-[color:var(--sk-text)]">
        {label}
      </span>
      <select
        aria-label={label}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="sk-input h-10 rounded-xl px-3 text-sm"
      >
        {allowEmptyLabel ? <option value="">{allowEmptyLabel}</option> : null}
        {options.map((profile) => (
          <option key={profile.id} value={profile.id}>
            {profile.name}
          </option>
        ))}
      </select>
      {hint ? (
        <span className="text-[10px] text-[color:var(--sk-text-muted)]">
          {hint}
        </span>
      ) : null}
    </label>
  );
}
