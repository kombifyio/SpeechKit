import { useEffect, useMemo, useState } from "react";

import { pickLatestModelDownloadJob } from "@/components/dashboard/state-hooks/use-model-download-state";
import {
  builtInPrimaryModelSelections,
  defaultSettingsState,
  fetchAudioDevices,
  saveProviderCredential,
  setAudioDevice,
  updateProviderIntegration,
  type AudioDevice,
  type DownloadItem,
  type DownloadJob,
} from "@/lib/speechkit";

import type { SetupWizardCompletion } from "./dashboard-types";

const onboardingHotkeys = {
  dictate: defaultSettingsState.dictateHotkey,
  assist: defaultSettingsState.assistHotkey,
  voiceAgent: defaultSettingsState.voiceAgentHotkey,
} as const;

const onboardingHotkeyLabels = {
  dictate: "Ctrl+Win",
  assist: "Win+Alt",
  voiceAgent: "Ctrl+Shift",
} as const;

type WizardStep = "welcome" | "local_model" | "integrations" | "done";
type OnboardingIntegration = {
  provider: string;
  label: string;
  category: "Cloud Router / Gateway" | "Direct Provider" | "Local Provider";
  modes: IntegrationMode[];
  credentialLabel?: string;
  setupUrl: string;
  setupLabel: string;
  headline: string;
  summary: string;
};

type IntegrationMode = "dictate" | "assist" | "voice_agent";

const integrationModeOrder: IntegrationMode[] = [
  "dictate",
  "assist",
  "voice_agent",
];
const integrationModeLabels: Record<IntegrationMode, string> = {
  dictate: "Dictation",
  assist: "Assist",
  voice_agent: "Voice Agent",
};

const onboardingIntegrations: OnboardingIntegration[] = [
  {
    provider: "huggingface",
    label: "Hugging Face",
    category: "Cloud Router / Gateway",
    modes: ["dictate", "assist", "voice_agent"],
    credentialLabel: "Hugging Face token",
    setupUrl: "https://huggingface.co/settings/tokens",
    setupLabel: "Create Hugging Face token",
    headline: "One key for every mode.",
    summary: "All modes with one gateway key.",
  },
  {
    provider: "openrouter",
    label: "OpenRouter",
    category: "Cloud Router / Gateway",
    modes: ["dictate", "assist", "voice_agent"],
    credentialLabel: "OpenRouter API key",
    setupUrl: "https://openrouter.ai/settings/keys",
    setupLabel: "Create OpenRouter API key",
    headline: "One router for every mode.",
    summary: "Dictation, Assist, and Voice Agent through OpenRouter.",
  },
  {
    provider: "openai",
    label: "OpenAI",
    category: "Direct Provider",
    modes: ["dictate", "assist"],
    credentialLabel: "OpenAI API key",
    setupUrl: "https://platform.openai.com/api-keys",
    setupLabel: "Create OpenAI API key",
    headline: "Use your OpenAI key.",
    summary: "Use OpenAI for Dictation and Assist.",
  },
  {
    provider: "google",
    label: "Gemini / Google AI",
    category: "Direct Provider",
    modes: ["dictate", "assist", "voice_agent"],
    credentialLabel: "Gemini / Google AI API key",
    setupUrl: "https://aistudio.google.com/apikey",
    setupLabel: "Create Gemini API key",
    headline: "Gemini for Voice Agent.",
    summary: "Native Voice Agent plus Google profiles.",
  },
  {
    provider: "groq",
    label: "Groq",
    category: "Direct Provider",
    modes: ["dictate", "assist"],
    credentialLabel: "Groq API key",
    setupUrl: "https://console.groq.com/keys",
    setupLabel: "Create Groq API key",
    headline: "Groq for fast turns.",
    summary: "Fast Direct Provider for Dictation and Assist.",
  },
  {
    provider: "ollama",
    label: "Ollama",
    category: "Local Provider",
    modes: ["dictate", "assist", "voice_agent"],
    setupUrl: "https://ollama.com/download",
    setupLabel: "Download Ollama",
    headline: "Run models locally.",
    summary: "Run optional local models yourself.",
  },
];

const onboardingIntegrationGroups = [
  {
    id: "gateways",
    headline: "Want one key for everything? Start with a gateway.",
    summary:
      "Hugging Face and OpenRouter unlock more model choice without managing every vendor.",
    providers: ["huggingface", "openrouter"],
    gridClass: "md:grid-cols-2",
  },
  {
    id: "direct",
    headline: "Already use a provider? Plug in your key.",
    summary:
      "OpenAI, Gemini, and Groq are best when your team already has an API account.",
    providers: ["openai", "google", "groq"],
    gridClass: "md:grid-cols-3",
  },
  {
    id: "local",
    headline: "Want to test models on your own machine? Use Ollama.",
    summary: "Keep experimenting locally while SpeechKit stays local-first by default.",
    providers: ["ollama"],
    gridClass: "md:grid-cols-2",
  },
];

const integrationLogoSrc: Record<string, string> = {
  huggingface: "/integrations/huggingface.svg",
  openrouter: "/integrations/openrouter.svg",
  openai: "/integrations/openai.svg",
  google: "/integrations/gemini.svg",
  groq: "/integrations/groq.svg",
  ollama: "/integrations/ollama.svg",
};

const logoFrameClass: Record<string, string> = {
  huggingface: "bg-[#ffd84d]",
  openrouter: "bg-[#0f1117]",
  openai: "bg-white",
  google: "bg-white",
  groq: "bg-white",
  ollama: "bg-white",
};

function orderedIntegrationModeLabels(modes: IntegrationMode[]) {
  const enabledModes = new Set(modes);
  return integrationModeOrder
    .filter((mode) => enabledModes.has(mode))
    .map((mode) => integrationModeLabels[mode]);
}

function setupDisplayUrl(setupUrl: string) {
  try {
    const parsed = new URL(setupUrl);
    return `${parsed.host}${parsed.pathname}`.replace(/\/$/, "");
  } catch {
    return setupUrl;
  }
}

export function SetupWizard({
  catalog,
  jobs,
  onStartDownload,
  onCancelDownload,
  onSelectDownloadedModel,
  onComplete,
}: {
  catalog: DownloadItem[];
  jobs: DownloadJob[];
  onStartDownload: (itemId: string) => Promise<DownloadJob>;
  onCancelDownload: (jobId: string) => Promise<void>;
  onSelectDownloadedModel: (itemId: string) => Promise<{ message?: string }>;
  onComplete: (next?: SetupWizardCompletion) => void;
}) {
  const [step, setStep] = useState<WizardStep>("welcome");
  const [devices, setDevices] = useState<AudioDevice[]>([]);
  const [selectedDevice, setSelectedDevice] = useState("");
  const [loading, setLoading] = useState(false);
  const [busyModelAction, setBusyModelAction] = useState<string | null>(null);
  const [modelActionError, setModelActionError] = useState<string | null>(null);
  const [preferredLocalModelId, setPreferredLocalModelId] = useState<
    string | null
  >(null);
  const [selectedIntegrations, setSelectedIntegrations] = useState<
    Record<string, boolean>
  >({});
  const [integrationTokens, setIntegrationTokens] = useState<
    Record<string, string>
  >({});

  useEffect(() => {
    void fetchAudioDevices()
      .then((res) => {
        setDevices(res.devices);
        setSelectedDevice(
          res.selectedDeviceId || res.devices[0]?.deviceId || "",
        );
      })
      .catch(() => {});
  }, []);

  const handleDeviceSelect = (deviceId: string) => {
    setSelectedDevice(deviceId);
    void setAudioDevice(deviceId).catch(() => {});
  };

  const localCatalogItems = useMemo(
    () => catalog.filter((item) => item.profileId === "stt.local.whispercpp"),
    [catalog],
  );

  const localModelChoices = useMemo(() => {
    const onboardingIDs = ["whisper.ggml-large-v3-turbo", "whisper.ggml-small"];
    return onboardingIDs
      .map((id) => catalog.find((item) => item.id === id))
      .filter((item): item is DownloadItem => Boolean(item));
  }, [catalog]);

  useEffect(() => {
    if (preferredLocalModelId) {
      return;
    }
    const selectedLocalModel = localCatalogItems.find((item) => item.selected);
    const recommendedLocalModel =
      localModelChoices.find((item) => item.recommended) ??
      localModelChoices.find((item) => item.id === "whisper.ggml-large-v3-turbo") ??
      localModelChoices[0];
    const defaultLocalModel = selectedLocalModel ?? recommendedLocalModel;
    if (defaultLocalModel) {
      setPreferredLocalModelId(defaultLocalModel.id);
    }
  }, [localCatalogItems, localModelChoices, preferredLocalModelId]);

  const localModelIDs = useMemo(
    () => new Set(localModelChoices.map((item) => item.id)),
    [localModelChoices],
  );

  const chosenLocalModel = useMemo(
    () =>
      localCatalogItems.find((item) => item.id === preferredLocalModelId) ??
      null,
    [localCatalogItems, preferredLocalModelId],
  );

  const activeLocalJob = useMemo(
    () =>
      pickLatestModelDownloadJob(
        jobs.filter(
          (job) =>
            localModelIDs.has(job.modelId) &&
            (job.status === "pending" || job.status === "running"),
        ),
      ),
    [jobs, localModelIDs],
  );

  const continueLabel = activeLocalJob
    ? "Continue while model downloads"
    : "Continue";

  const handleModelDownload = async (item: DownloadItem) => {
    setBusyModelAction(`download:${item.id}`);
    setModelActionError(null);
    setPreferredLocalModelId(item.id);
    try {
      await onStartDownload(item.id);
    } catch (error) {
      setModelActionError(
        error instanceof Error ? error.message : "Download failed",
      );
    } finally {
      setBusyModelAction(null);
    }
  };

  const handleModelSelect = async (item: DownloadItem) => {
    setBusyModelAction(`select:${item.id}`);
    setModelActionError(null);
    setPreferredLocalModelId(item.id);
    try {
      await onSelectDownloadedModel(item.id);
    } catch (error) {
      setModelActionError(
        error instanceof Error ? error.message : "Model switch failed",
      );
    } finally {
      setBusyModelAction(null);
    }
  };

  const handleFinish = async () => {
    setLoading(true);
    try {
      setModelActionError(null);
      if (chosenLocalModel?.available && !chosenLocalModel.selected) {
        await onSelectDownloadedModel(chosenLocalModel.id);
      }
      const body = new URLSearchParams();
      body.set("dictate_hotkey", onboardingHotkeys.dictate);
      body.set("assist_hotkey", onboardingHotkeys.assist);
      body.set("voice_agent_hotkey", onboardingHotkeys.voiceAgent);
      body.set("audio_device_id", selectedDevice);
      body.set(
        "dictate_primary_profile_id",
        builtInPrimaryModelSelections.dictate.primaryProfileId,
      );
      body.set(
        "assist_primary_profile_id",
        builtInPrimaryModelSelections.assist.primaryProfileId,
      );
      body.set(
        "voice_primary_profile_id",
        builtInPrimaryModelSelections.voice_agent.primaryProfileId,
      );
      await fetch("/settings/update", { method: "POST", body });
      for (const integration of onboardingIntegrations) {
        if (!selectedIntegrations[integration.provider]) {
          continue;
        }
        const token = (integrationTokens[integration.provider] ?? "").trim();
        if (integration.credentialLabel && token) {
          await saveProviderCredential(integration.provider, token);
          continue;
        }
        await updateProviderIntegration(integration.provider, true);
      }
    } catch (error) {
      setLoading(false);
      setModelActionError(
        error instanceof Error ? error.message : "Setup could not be completed",
      );
      return;
    }
    onComplete();
  };

  const handleChooseModel = (itemId: string) => {
    setPreferredLocalModelId(itemId);
    setModelActionError(null);
  };

  const handleSkipSetup = () => {
    setModelActionError(null);
    onComplete();
  };

  const toggleIntegration = (provider: string) => {
    setSelectedIntegrations((current) => ({
      ...current,
      [provider]: !current[provider],
    }));
    setModelActionError(null);
  };

  const STEPS: WizardStep[] = [
    "welcome",
    "local_model",
    "integrations",
    "done",
  ];

  return (
    <div
      className={[
        "flex h-screen flex-col items-center bg-[#131318] text-[#e4e1e9] px-6 relative",
        step === "local_model" || step === "integrations"
          ? "justify-start overflow-hidden py-0"
          : "justify-center overflow-hidden",
      ].join(" ")}
    >
      {/* Ambient glow */}
      <div className="fixed top-[-10%] left-[-10%] w-[40%] h-[40%] rounded-full bg-[#cabeff]/5 blur-[120px] pointer-events-none" />
      <div className="fixed bottom-[-10%] right-[-10%] w-[40%] h-[40%] rounded-full bg-[#cabeff]/5 blur-[120px] pointer-events-none" />

      {step === "welcome" && (
        <div className="flex flex-col items-center text-center max-w-2xl w-full z-10">
          {/* Logo */}
          <div className="w-20 h-20 bg-[#2a292f] rounded-full flex items-center justify-center ambient-glow border border-white/5 mb-12">
            <svg
              className="w-10 h-10 text-[#947dff]"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M12 14c1.66 0 3-1.34 3-3V5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3zm5.91-3c-.49 0-.9.36-.98.85C16.52 14.2 14.47 16 12 16s-4.52-1.8-4.93-4.15a.998.998 0 0 0-.98-.85c-.61 0-1.09.54-1 1.14.49 3 2.89 5.35 5.91 5.78V20c0 .55.45 1 1 1s1-.45 1-1v-2.08a6.993 6.993 0 0 0 5.91-5.78c.1-.6-.39-1.14-1-1.14z" />
            </svg>
          </div>

          <h1 className="text-4xl md:text-5xl font-extrabold tracking-tight text-[#e4e1e9]">
            Welcome to SpeechKit
          </h1>
          <p className="mt-4 text-lg text-[#b5b3c4] font-light leading-relaxed">
            Your voice, your workflow. Dictate, assist, and automate —
            hands-free.
          </p>

          {/* Feature cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6 w-full max-w-4xl mt-16">
            <FeatureCard
              icon="dictate"
              title="Dictation"
              desc="Exact speech-to-text into the focused app"
            />
            <FeatureCard
              icon="sparkle"
              title="AI Assist"
              desc="One-shot rewrites, summaries, and drafts"
            />
            <FeatureCard
              icon="wave"
              title="Voice Agent"
              desc="Live follow-ups with session summaries"
            />
          </div>

          <div className="flex flex-col items-center space-y-4 mt-16">
            <button
              type="button"
              onClick={() => setStep("local_model")}
              className="signature-gradient ambient-glow text-[#2b0088] font-bold text-lg px-12 h-14 rounded-full transition-all active:scale-95 hover:opacity-90"
            >
              Get Started
            </button>
            <button
              type="button"
              onClick={handleSkipSetup}
              className="text-[#b5b3c4] text-sm font-medium hover:text-[#e4e1e9] transition-colors"
            >
              Skip setup
            </button>
          </div>
        </div>
      )}

      {step === "local_model" && (
        <div className="z-10 flex h-full w-full max-w-128 flex-col py-6">
          <div className="min-h-0 flex-1 space-y-5 overflow-y-auto pb-4">
            {/* Progress dots */}
            <div className="flex justify-center items-center gap-3">
              {STEPS.map((s, i) => (
                <div
                  key={s}
                  className={[
                    "w-2.5 h-2.5 rounded-full transition-all",
                    i <= STEPS.indexOf(step)
                      ? "bg-[#cabeff] shadow-[0_0_8px_rgba(202,190,255,0.4)]"
                      : "bg-[#484555]",
                  ].join(" ")}
                />
              ))}
            </div>

            <div className="text-center space-y-2">
              <h2 className="text-2xl font-extrabold tracking-tight">
                Local Dictation Setup
              </h2>
              <p className="text-[#b5b3c4] text-[13px] max-w-xl mx-auto leading-6">
                Choose the local Whisper model that SpeechKit should download
                first. You can always switch models later in Settings.
              </p>
            </div>

            <div className="space-y-2.5">
              {localModelChoices.map((item) => {
                const itemJob = jobs.find((job) => job.modelId === item.id);
                const itemDownloading =
                  itemJob?.status === "pending" ||
                  itemJob?.status === "running";
                const itemBusy =
                  busyModelAction === `download:${item.id}` ||
                  busyModelAction === `select:${item.id}`;
                return (
                  <WizardLocalModelCard
                    key={item.id}
                    item={item}
                    job={itemJob}
                    busy={itemBusy}
                    selectedForSetup={
                      preferredLocalModelId === item.id ||
                      (!preferredLocalModelId && item.selected)
                    }
                    onDownload={() => void handleModelDownload(item)}
                    onCancel={() =>
                      itemJob ? void onCancelDownload(itemJob.id) : undefined
                    }
                    onChooseModel={() => handleChooseModel(item.id)}
                    onUseModel={() => void handleModelSelect(item)}
                    downloading={itemDownloading}
                  />
                );
              })}
            </div>

            {modelActionError && (
              <div className="rounded-xl border border-red-400/15 bg-red-500/8 px-4 py-3 text-xs text-red-200/85">
                {modelActionError}
              </div>
            )}

            <div className="bg-[#1b1b20] rounded-xl px-4 py-3 flex items-start gap-3">
              <svg
                className="w-5 h-5 text-[#947dff] shrink-0 mt-0.5"
                viewBox="0 0 24 24"
                fill="currentColor"
              >
                <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z" />
              </svg>
              <p className="text-[#938ea1] text-xs leading-relaxed">
                SpeechKit downloads local models in the background and
                automatically wires them into Whisper.cpp as soon as the
                transfer finishes. You can continue immediately and keep using
                the app while the overlay tracks progress.
              </p>
            </div>

            {/* Mic selection (compact) */}
            {devices.length > 0 && (
              <div className="space-y-2">
                <label className="text-xs font-bold text-[#938ea1] uppercase tracking-widest mb-2 block">
                  Microphone
                </label>
                <select
                  value={selectedDevice}
                  onChange={(e) => handleDeviceSelect(e.target.value)}
                  className="w-full bg-[#0e0e13] border-none rounded-lg px-4 py-2.5 text-sm text-[#e4e1e9] focus:ring-1 focus:ring-[#cabeff]/40 appearance-none"
                >
                  {devices.map((d) => (
                    <option key={d.deviceId} value={d.deviceId}>
                      {d.label}
                      {d.isDefault ? " (Default)" : ""}
                    </option>
                  ))}
                </select>
              </div>
            )}
          </div>

          <div
            data-testid="onboarding-navigation"
            className="shrink-0 space-y-3 border-t border-[#35343a]/50 bg-[#131318]/95 pt-4 backdrop-blur-xl"
          >
            <button
              type="button"
              onClick={() => setStep("integrations")}
              className={[
                "w-full h-12 rounded-full font-bold transition-all flex items-center justify-center gap-2",
                "signature-gradient text-[#2b0088] ambient-glow active:scale-[0.98]",
              ].join(" ")}
            >
              {continueLabel}
            </button>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <button
                type="button"
                onClick={() => setStep("welcome")}
                className="text-[#b5b3c4] hover:text-[#e4e1e9] text-sm font-medium transition-colors"
              >
                Back
              </button>
              <span className="text-[11px] text-[#938ea1]">
                Optional integrations are next.
              </span>
            </div>
          </div>
        </div>
      )}

      {step === "integrations" && (
        <div className="z-10 flex h-full w-full max-w-5xl flex-col pb-6 pt-5">
          <div className="min-h-0 flex-1 overflow-y-auto pb-3">
            <div className="flex justify-center items-center gap-3">
              {STEPS.map((s, i) => (
                <div
                  key={s}
                  className={[
                    "w-2.5 h-2.5 rounded-full transition-all",
                    i <= STEPS.indexOf(step)
                      ? "bg-[#cabeff] shadow-[0_0_8px_rgba(202,190,255,0.4)]"
                      : "bg-[#484555]",
                  ].join(" ")}
                />
              ))}
            </div>

            <div className="mx-auto mt-3 max-w-2xl text-center">
              <h2 className="text-3xl font-extrabold tracking-tight">
                Integrations
              </h2>
              <p className="mt-1.5 text-base font-bold leading-6 text-[#e4e1e9]">
                Want more performance than local models? Add a provider.
              </p>
            </div>

            <div className="mt-4 space-y-4">
              {onboardingIntegrationGroups.map((group) => (
                <section key={group.id} className="space-y-2">
                  <div>
                    <h3 className="text-base font-extrabold tracking-tight text-[#e4e1e9]">
                      {group.headline}
                    </h3>
                    <p className="mt-0.5 text-xs leading-4 text-[#b5b3c4]">
                      {group.summary}
                    </p>
                  </div>
                  <div className={["grid gap-3", group.gridClass].join(" ")}>
                    {group.providers.map((provider) => {
                      const integration = onboardingIntegrations.find(
                        (item) => item.provider === provider,
                      );
                      if (!integration) {
                        return null;
                      }
                      const selected = Boolean(
                        selectedIntegrations[integration.provider],
                      );
                      const setupTarget = setupDisplayUrl(integration.setupUrl);
                      return (
                        <div
                          key={integration.provider}
                          data-testid="onboarding-integration-card"
                          className={[
                            "rounded-xl border bg-[#1b1b20] p-2.5 transition-colors",
                            selected
                              ? "border-[#cabeff]/40"
                              : "border-[#35343a]/80",
                          ].join(" ")}
                        >
                          <div className="flex items-start justify-between gap-3">
                            <ProviderLogo provider={integration.provider} />
                            <button
                              type="button"
                              role="switch"
                              aria-label={`Enable ${integration.label} integration`}
                              aria-checked={selected}
                              onClick={() =>
                                toggleIntegration(integration.provider)
                              }
                              className={[
                                "relative inline-flex h-5.5 w-9.5 shrink-0 cursor-pointer items-center rounded-full transition-colors",
                                selected ? "bg-[#cabeff]" : "bg-[#484555]",
                              ].join(" ")}
                            >
                              <span
                                className={[
                                  "inline-block h-4 w-4 rounded-full bg-white shadow transition-transform",
                                  selected
                                    ? "translate-x-4.75"
                                    : "translate-x-0.75",
                                ].join(" ")}
                              />
                            </button>
                          </div>

                          <p className="mt-1.5 text-sm font-extrabold leading-5 text-[#e4e1e9]">
                            {integration.headline}
                          </p>
                          <p className="mt-0.5 text-xs leading-4 text-[#b5b3c4]">
                            {integration.summary}
                          </p>
                          <div className="mt-1.5 flex flex-wrap gap-1.5">
                            <span className="rounded border border-[#484555] px-2 py-0.5 text-[9px] font-semibold uppercase tracking-[0.12em] text-[#cabeff]">
                              {integration.category}
                            </span>
                            {orderedIntegrationModeLabels(
                              integration.modes,
                            ).map((mode) => (
                              <span
                                key={mode}
                                data-testid="integration-mode-tag"
                                className="rounded bg-[#2a292f] px-2 py-0.5 text-[9px] text-[#b5b3c4]"
                              >
                                {mode}
                              </span>
                            ))}
                          </div>

                          {selected && integration.credentialLabel && (
                            <label className="mt-3 block space-y-1.5">
                              <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">
                                {integration.credentialLabel}
                              </span>
                              <input
                                aria-label={integration.credentialLabel}
                                type="password"
                                value={
                                  integrationTokens[integration.provider] ?? ""
                                }
                                onChange={(event) =>
                                  setIntegrationTokens((tokens) => ({
                                    ...tokens,
                                    [integration.provider]: event.target.value,
                                  }))
                                }
                                placeholder="Paste key or continue without it"
                                className="w-full rounded-lg border border-[#35343a] bg-[#0e0e13] px-3 py-2 text-xs text-[#e4e1e9] outline-none focus:border-[#cabeff]/50"
                              />
                            </label>
                          )}

                          <a
                            href={integration.setupUrl}
                            target="_blank"
                            rel="noreferrer"
                            title={integration.setupUrl}
                            className="group/setup mt-1.5 inline-flex max-w-full flex-wrap items-center gap-x-1 text-xs font-semibold text-[#cabeff] hover:text-[#e4e1e9]"
                          >
                            <span>{integration.setupLabel}</span>
                            <span className="hidden max-w-full truncate text-xs font-medium text-[#938ea1] group-hover/setup:inline">
                              {setupTarget}
                            </span>
                          </a>
                        </div>
                      );
                    })}
                  </div>
                </section>
              ))}
            </div>

            {modelActionError && (
              <div className="mt-4 rounded-xl border border-red-400/15 bg-red-500/8 px-4 py-3 text-xs text-red-200/85">
                {modelActionError}
              </div>
            )}
          </div>

          <div
            data-testid="onboarding-navigation"
            className="shrink-0 flex items-center justify-between gap-3 border-t border-[#35343a]/50 bg-[#131318]/95 pt-4 backdrop-blur-xl"
          >
            <button
              type="button"
              onClick={() => setStep("local_model")}
              className="text-[#b5b3c4] hover:text-[#e4e1e9] text-sm font-medium transition-colors"
            >
              Back
            </button>
            <button
              type="button"
              onClick={() => setStep("done")}
              className="signature-gradient text-[#2b0088] h-12 rounded-full px-8 font-bold transition-all ambient-glow active:scale-[0.98]"
            >
              Continue
            </button>
          </div>
        </div>
      )}

      {step === "done" && (
        <div className="flex flex-col items-center text-center max-w-4xl w-full z-10">
          {/* Progress dots */}
          <div className="flex gap-3 mb-12">
            {STEPS.map((s) => (
              <div key={s} className="w-2.5 h-2.5 rounded-full bg-[#cabeff]" />
            ))}
          </div>

          {/* Success icon */}
          <div className="w-24 h-24 rounded-full signature-gradient flex items-center justify-center ambient-glow ring-8 ring-[#cabeff]/10 mb-8">
            <svg
              className="w-12 h-12 text-[#2b0088]"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={2.5}
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M5 13l4 4L19 7"
              />
            </svg>
          </div>

          <h2 className="text-4xl font-extrabold tracking-tight mb-4">
            You're all set!
          </h2>
          <p className="text-lg text-[#b5b3c4] max-w-md leading-relaxed">
            SpeechKit is ready. Here is how to get started:
          </p>

          {/* Quick-start cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6 w-full mt-12 mb-12">
            <WizardFeatureCard
              title={onboardingHotkeyLabels.dictate}
              desc="Open a text field, speak one sentence, then check the output"
            />
            <WizardFeatureCard
              title={onboardingHotkeyLabels.assist}
              desc="Select text and say summarize this or rewrite shorter"
            />
            <WizardFeatureCard
              title={onboardingHotkeyLabels.voiceAgent}
              desc="Ask a follow-up question and end the session for a summary"
            />
          </div>

          <button
            type="button"
            onClick={() => void handleFinish()}
            disabled={loading}
            className="signature-gradient text-[#2b0088] h-14 rounded-full font-bold text-lg px-12 ambient-glow hover:scale-[1.02] active:scale-[0.98] transition-all disabled:opacity-50"
          >
            {loading ? "Setting up..." : "Start Using SpeechKit"}
          </button>
        </div>
      )}
    </div>
  );
}

/* ── Shared Components ── */

function ProviderLogo({ provider }: { provider: string }) {
  const label =
    onboardingIntegrations.find(
      (integration) => integration.provider === provider,
    )?.label ?? "Provider";
  const src = integrationLogoSrc[provider] ?? "";
  const frameClass = logoFrameClass[provider] ?? "bg-white";

  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <span
        className={[
          "flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-[#35343a] p-1",
          frameClass,
        ].join(" ")}
      >
        <img
          src={src}
          alt={`${label} logo`}
          className="h-full w-full object-contain"
        />
      </span>
      <span
        data-testid="provider-brand-name"
        className="min-w-0 truncate text-[13px] font-bold text-[#e4e1e9]"
      >
        {label}
      </span>
    </div>
  );
}

function FeatureCard({
  icon,
  title,
  desc,
}: {
  icon: string;
  title: string;
  desc: string;
}) {
  const iconPath =
    icon === "dictate"
      ? "M4 11h2v8H4zm4-4h2v12H8zm4-3h2v15h-2zm4 5h2v10h-2z"
      : icon === "sparkle"
        ? "M12 2L9.19 8.63 2 9.24l5.46 4.73L5.82 21 12 17.27 18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2z"
        : "M7 18h2V6H7v12zm4-12v12h2V6h-2zm-8 8h2v-4H3v4zm12-6v8h2V8h-2zm4 2v4h2v-4h-2z";
  return (
    <div className="sk-panel rounded-[24px] p-8 text-center transition-all group hover:bg-[color:var(--sk-surface-2)]">
      <div className="mb-6 flex h-12 w-12 items-center justify-center rounded-xl bg-[color:var(--sk-surface-0)] text-[color:var(--sk-accent)] transition-transform group-hover:scale-110">
        <svg className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
          <path d={iconPath} />
        </svg>
      </div>
      <h3 className="mb-2 text-xl font-bold text-[color:var(--sk-text)]">
        {title}
      </h3>
      <p className="text-sm text-[color:var(--sk-text-muted)]">{desc}</p>
    </div>
  );
}
function WizardLocalModelCard({
  item,
  job,
  busy,
  selectedForSetup,
  downloading,
  onDownload,
  onCancel,
  onChooseModel,
  onUseModel,
}: {
  item: DownloadItem;
  job?: DownloadJob;
  busy: boolean;
  selectedForSetup: boolean;
  downloading: boolean;
  onDownload: () => void;
  onCancel: () => void;
  onChooseModel: () => void;
  onUseModel: () => void;
}) {
  const selected = item.selected;
  const ready = item.available || job?.status === "done";
  const runtimeMissing =
    ready && (item.runtimeReady === false || Boolean(item.runtimeProblem));
  const readyToUse = ready && !runtimeMissing;
  const chooseButtonLabel = selectedForSetup
    ? "Chosen for setup"
    : readyToUse
      ? "Use after setup"
      : "Choose";
  const statusLabel = downloading
    ? "Downloading"
    : selected
      ? "Selected"
      : runtimeMissing
        ? "Runtime missing"
        : ready
          ? "Ready"
          : "Not downloaded";

  return (
    <div
      data-testid={`local-model-card-${item.id}`}
      className={[
        "rounded-2xl border px-4 py-3.5 transition-all",
        selectedForSetup
          ? "border-[#cabeff]/35 bg-[#cabeff]/8"
          : "border-[#35343a] bg-[#1b1b20]",
      ].join(" ")}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-[#e4e1e9] font-semibold text-base leading-6">
              {item.name}
            </h3>
            {item.recommended && (
              <span className="rounded-full bg-[#cabeff]/15 px-2 py-1 text-[10px] font-bold uppercase tracking-widest text-[#cabeff]">
                Recommended
              </span>
            )}
            {selectedForSetup && (
              <span className="rounded-full bg-emerald-500/10 px-2 py-1 text-[10px] font-bold uppercase tracking-widest text-emerald-200/85">
                Starter model
              </span>
            )}
          </div>
          <p className="mt-1 text-[13px] leading-6 text-[#b5b3c4]">
            {item.description}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-2 text-[10px] text-[#938ea1]">
            <span>{item.sizeLabel}</span>
            <span>License: {item.license}</span>
            {selected && (
              <span className="text-emerald-200/85">
                Selected on this device
              </span>
            )}
            {!selected && readyToUse && (
              <span className="text-emerald-200/85">Ready on this device</span>
            )}
            {runtimeMissing && (
              <span className="text-amber-200/85">
                {item.runtimeProblem ?? "Local runtime missing."}
              </span>
            )}
          </div>
          {downloading && job && (
            <div className="mt-3 space-y-1.5">
              <div className="h-1.5 overflow-hidden rounded-full bg-[#0e0e13]">
                <div
                  className="h-full rounded-full bg-[#cabeff] transition-all duration-500"
                  style={{
                    width: `${Math.max(6, Math.round(job.progress * 100))}%`,
                  }}
                />
              </div>
              <div className="text-[10px] text-[#938ea1]">{job.statusText}</div>
            </div>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-2">
          <div className="rounded-full border border-[#484555] px-3 py-1 text-[10px] text-[#938ea1]">
            {statusLabel}
          </div>
          {readyToUse ? (
            selected ? (
              <button
                type="button"
                disabled
                className="rounded-full border border-emerald-300/20 px-3 py-1.5 text-[11px] font-medium text-emerald-200/80 opacity-80"
              >
                Active on device
              </button>
            ) : (
              <button
                type="button"
                aria-label={`Use ${item.name} after setup`}
                onClick={onUseModel}
                disabled={busy}
                className={[
                  "rounded-full border px-3 py-1.5 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-70",
                  selectedForSetup
                    ? "border-emerald-300/20 bg-emerald-500/10 text-emerald-200/85"
                    : "border-[#484555] text-[#cabeff] hover:border-[#cabeff]/40",
                ].join(" ")}
              >
                {chooseButtonLabel}
              </button>
            )
          ) : (
            <>
              <button
                type="button"
                aria-label={`Choose ${item.name} for setup`}
                onClick={onChooseModel}
                disabled={busy || selectedForSetup}
                className={[
                  "rounded-full border px-3 py-1.5 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-70",
                  selectedForSetup
                    ? "border-emerald-300/20 bg-emerald-500/10 text-emerald-200/85"
                    : "border-[#484555] text-[#cabeff] hover:border-[#cabeff]/40",
                ].join(" ")}
              >
                {chooseButtonLabel}
              </button>
              {downloading ? (
                <button
                  type="button"
                  onClick={onCancel}
                  className="rounded border border-[#484555] px-2.5 py-1.5 text-[10px] text-[#938ea1] hover:border-red-300/40 hover:text-red-200"
                >
                  Cancel
                </button>
              ) : (
                <button
                  type="button"
                  aria-label={`Download ${item.name}`}
                  onClick={onDownload}
                  disabled={busy}
                  className="rounded-full bg-[#cabeff]/20 px-3 py-1.5 text-[11px] font-bold text-[#cabeff] hover:bg-[#cabeff]/30 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  Download
                </button>
              )}
            </>
          )}
          {selected && (
            <span className="text-[10px] font-medium text-emerald-200/85">
              SpeechKit uses this model now.
            </span>
          )}
          {!selected && readyToUse && selectedForSetup && (
            <span className="text-[10px] font-medium text-emerald-200/85">
              SpeechKit will switch after setup.
            </span>
          )}
          {!readyToUse && selectedForSetup && (
            <span className="text-[10px] font-medium text-emerald-200/85">
              SpeechKit will prefer this model first.
            </span>
          )}
        </div>
      </div>
    </div>
  );
}
function WizardFeatureCard({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="bg-[#1b1b20] p-6 rounded-xl hover:bg-[#2a292f] transition-all flex flex-col items-center text-center">
      <h3 className="text-[#e4e1e9] font-bold mb-1">{title}</h3>
      <p className="text-[#b5b3c4] text-sm">{desc}</p>
    </div>
  );
}
