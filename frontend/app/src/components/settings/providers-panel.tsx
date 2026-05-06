import type { Dispatch, ReactNode, SetStateAction } from "react";

import type {
  ProviderCredentialState,
  ProviderIntegrationState,
} from "@/lib/speechkit";

import { providerSecretNoun } from "@/components/settings/secrets-panel";

const INTEGRATION_ORDER = [
  "huggingface",
  "openrouter",
  "openai",
  "google",
  "groq",
  "ollama",
] as const;

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

type IntegrationMode = ProviderIntegrationState["supportedModes"][number];

const MODE_TAG_ORDER: IntegrationMode[] = [
  "dictate",
  "assist",
  "voice_agent",
];

export function IntegrationsSection({
  integrations,
  credentials,
  providerTokens,
  providerBusy,
  setProviderTokens,
  onToggle,
  onSaveCredential,
  tokenStatusLabel,
}: {
  integrations?: Record<string, ProviderIntegrationState>;
  credentials?: Record<string, ProviderCredentialState>;
  providerTokens: Record<string, string>;
  providerBusy: Record<string, boolean>;
  setProviderTokens: Dispatch<SetStateAction<Record<string, string>>>;
  onToggle: (provider: string, enabled: boolean) => void;
  onSaveCredential: (provider: string) => void;
  tokenStatusLabel: (cred: ProviderCredentialState) => string;
}) {
  const cards = integrationCards(integrations);

  return (
    <ProviderPanelSection title="Integrations" className="xl:col-span-2">
      {cards.length === 0 ? (
        <p className="text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
          Built-in local models are available. Optional providers appear here
          when this build exposes them.
        </p>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {cards.map((integration) => {
            const credential = credentials?.[integration.provider];
            const needsCredential =
              integration.enabled &&
              integration.credentialRequired &&
              !credential?.available;
            const isBusy = providerBusy[integration.provider] ?? false;
            const token = providerTokens[integration.provider] ?? "";
            const setupTarget = integration.setupUrl
              ? setupDisplayUrl(integration.setupUrl)
              : "";

            return (
              <div
                key={integration.provider}
                data-testid="settings-integration-card"
                className={[
                  "min-w-0 rounded-xl border bg-[color:var(--sk-surface-1)] px-3 py-3",
                  integration.enabled
                    ? "border-[color:var(--sk-accent)]/30"
                    : "border-[color:var(--sk-panel-border)]",
                ].join(" ")}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <ProviderWordmark provider={integration.provider} />
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      <span className="rounded border border-[color:var(--sk-border)] px-1.5 py-0.5 text-[9px] font-medium uppercase tracking-[0.12em] text-[color:var(--sk-text-muted)]">
                        {integrationCategoryLabel(integration)}
                      </span>
                      {sortedSupportedModes(integration.supportedModes).map((mode) => (
                        <span
                          key={mode}
                          data-testid="integration-mode-tag"
                          className="rounded bg-[color:var(--sk-surface-2)] px-1.5 py-0.5 text-[9px] text-[color:var(--sk-text-muted)]"
                        >
                          {modeLabel(mode)}
                        </span>
                      ))}
                    </div>
                  </div>
                  <button
                    type="button"
                    role="switch"
                    aria-label={`Enable ${integration.label} integration`}
                    aria-checked={integration.enabled}
                    disabled={isBusy}
                    onClick={() =>
                      onToggle(integration.provider, !integration.enabled)
                    }
                    className={[
                      "relative inline-flex h-5.5 w-9.5 shrink-0 items-center rounded-full transition-colors",
                      integration.enabled
                        ? "bg-[color:var(--sk-accent)]"
                        : "bg-[color:var(--sk-border)]",
                      isBusy ? "opacity-60" : "cursor-pointer",
                    ].join(" ")}
                  >
                    <span
                      className={[
                        "inline-block h-4 w-4 rounded-full bg-white shadow transition-transform",
                        integration.enabled
                          ? "translate-x-4.75"
                          : "translate-x-0.75",
                      ].join(" ")}
                    />
                  </button>
                </div>

                <div className="mt-3 flex items-center justify-between gap-2">
                  <span
                    className={[
                      "text-[10px]",
                      integration.available
                        ? "text-emerald-300/75"
                        : "text-amber-200/75",
                    ].join(" ")}
                  >
                    {integration.credentialRequired
                      ? credential
                        ? tokenStatusLabel(credential)
                        : "No key configured"
                      : "Local runtime"}
                  </span>
                  {integration.setupUrl && (
                    <a
                      href={integration.setupUrl}
                      target="_blank"
                      rel="noreferrer"
                      title={integration.setupUrl}
                      className="group/setup max-w-[11rem] shrink-0 text-right text-[10px] font-medium text-[color:var(--sk-accent)]/85 hover:text-[color:var(--sk-accent)]"
                    >
                      <span>{integrationSetupLabel(integration)}</span>
                      <span className="hidden max-w-full truncate text-[color:var(--sk-text-muted)] group-hover/setup:block">
                        {setupTarget}
                      </span>
                    </a>
                  )}
                </div>

                {needsCredential && credential && (
                  <div className="mt-3 flex gap-2">
                    <input
                      aria-label={`${integration.label} ${providerSecretNoun(integration.provider)}`}
                      type="password"
                      value={token}
                      onChange={(event) =>
                        setProviderTokens((tokens) => ({
                          ...tokens,
                          [integration.provider]: event.target.value,
                        }))
                      }
                      placeholder={
                        credential.envName ||
                        (integration.provider === "huggingface"
                          ? "HF token"
                          : "API key")
                      }
                      className="sk-input h-8 min-w-0 flex-1 rounded px-2.5 text-xs"
                    />
                    <button
                      type="button"
                      onClick={() => onSaveCredential(integration.provider)}
                      disabled={isBusy}
                      className="sk-secondary-button shrink-0 rounded px-3 text-[11px] font-medium"
                    >
                      Save
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </ProviderPanelSection>
  );
}

function ProviderPanelSection({
  title,
  children,
  className = "",
}: {
  title: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={["min-w-0 py-2", className].join(" ")}>
      <div className="mb-4 border-b border-[color:var(--sk-shell-divider)]/85 pb-3">
        <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
          {title}
        </span>
      </div>
      {children}
    </section>
  );
}

function integrationCards(
  integrations?: Record<string, ProviderIntegrationState>,
) {
  if (!integrations) return [];
  return INTEGRATION_ORDER.map((provider) => integrations[provider]).filter(
    (integration): integration is ProviderIntegrationState =>
      Boolean(integration),
  );
}

function integrationCategoryLabel(integration: ProviderIntegrationState) {
  switch (integration.integrationKind) {
    case "cloud_gateway":
      return "Cloud Router / Gateway";
    case "local_provider":
      return "Local Provider";
    default:
      return "Direct Provider";
  }
}

function modeLabel(mode: "dictate" | "assist" | "voice_agent") {
  switch (mode) {
    case "dictate":
      return "Dictation";
    case "voice_agent":
      return "Voice Agent";
    default:
      return "Assist";
  }
}

function sortedSupportedModes(modes: IntegrationMode[]) {
  const modeSet = new Set(modes);
  return MODE_TAG_ORDER.filter((mode) => modeSet.has(mode));
}

function setupDisplayUrl(setupUrl: string) {
  try {
    const parsed = new URL(setupUrl);
    return `${parsed.host}${parsed.pathname}`.replace(/\/$/, "");
  } catch {
    return setupUrl;
  }
}

function integrationSetupLabel(integration: ProviderIntegrationState) {
  if (!integration.setupUrl) return "Open setup";
  if (!integration.credentialRequired) return `Open ${integration.label} setup`;
  if (integration.provider === "huggingface") {
    return "Create token";
  }
  return "Create API key";
}

function ProviderWordmark({ provider }: { provider: string }) {
  const label =
    provider === "google"
      ? "Gemini / Google AI"
      : provider === "huggingface"
        ? "Hugging Face"
        : provider === "openrouter"
          ? "OpenRouter"
          : provider === "openai"
            ? "OpenAI"
            : provider === "ollama"
              ? "Ollama"
              : provider === "groq"
                ? "Groq"
                : "Provider";
  const src = integrationLogoSrc[provider] ?? "";
  const frameClass = logoFrameClass[provider] ?? "bg-white";
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span
        className={[
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-[color:var(--sk-panel-border)] p-1.5",
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
        className="min-w-0 truncate text-sm font-semibold text-[color:var(--sk-text)]"
      >
        {label}
      </span>
    </div>
  );
}
