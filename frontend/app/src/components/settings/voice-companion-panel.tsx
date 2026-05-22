import { useCallback, useMemo, useState } from "react";

import type {
  HomeAssistantSettings,
  PiperTTSSettings,
  PiperVoiceSummary,
} from "@/lib/speechkit";
import { fetchPiperVoices, probeHomeAssistant } from "@/lib/speechkit/settings-api";

import { Section } from "@/components/settings/settings-primitives";

type ProbeStatus =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "ok"; url?: string }
  | { kind: "error"; message: string };

const LOCALE_PRESETS: Array<{ code: string; label: string }> = [
  { code: "en", label: "English (en)" },
  { code: "de", label: "Deutsch (de)" },
];

// VoiceCompanionPanel groups two related v0.38.2 sections that both
// power the Voice-Companion / Assist Mode runtime:
//   1. HomeAssistant Conversation API (URL + token env + language)
//   2. Piper TTS (offline subprocess synthesizer, per-locale voice picker)
//
// Both blocks are read-only in the UI when no usable backend exists
// (e.g. piper voice_dir empty); the Save button on the parent settings
// page persists the changes via /settings/update — this panel only
// emits onChange callbacks.
export function VoiceCompanionPanel({
  homeAssistant,
  piper,
  onUpdateHomeAssistant,
  onUpdatePiper,
}: {
  homeAssistant: HomeAssistantSettings;
  piper: PiperTTSSettings;
  onUpdateHomeAssistant: (next: HomeAssistantSettings) => void;
  onUpdatePiper: (next: PiperTTSSettings) => void;
}) {
  return (
    <div className="grid grid-cols-1 gap-y-5">
      <HomeAssistantSection
        value={homeAssistant}
        onChange={onUpdateHomeAssistant}
      />
      <PiperSection value={piper} onChange={onUpdatePiper} />
    </div>
  );
}

function HomeAssistantSection({
  value,
  onChange,
}: {
  value: HomeAssistantSettings;
  onChange: (next: HomeAssistantSettings) => void;
}) {
  const [status, setStatus] = useState<ProbeStatus>({ kind: "idle" });

  const runProbe = useCallback(async () => {
    setStatus({ kind: "running" });
    try {
      const result = await probeHomeAssistant({
        url: value.url,
        tokenEnv: value.tokenEnv,
      });
      if (result.ok) {
        setStatus({ kind: "ok", url: result.url });
      } else {
        setStatus({ kind: "error", message: result.error ?? "Unknown error" });
      }
    } catch (err) {
      setStatus({
        kind: "error",
        message: err instanceof Error ? err.message : String(err),
      });
    }
  }, [value.url, value.tokenEnv]);

  return (
    <Section
      title="Home Assistant Bridge"
      testId="settings-section-homeassistant"
    >
      <p className="mb-3 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
        Voice-Companion sends matched smart-home utterances ("turn on the
        kitchen light") to Home Assistant's Conversation API. Configure URL
        and a long-lived access token env-var to enable. Without both fields
        the skill stays inert and HA-style requests fall through to the LLM.
      </p>
      <div className="grid gap-3">
        <LabeledInput
          label="HA base URL"
          placeholder="https://ha.example.com:8123"
          value={value.url}
          onChange={(v) => onChange({ ...value, url: v })}
        />
        <LabeledInput
          label="Token env-var name"
          placeholder="HA_LL_TOKEN"
          value={value.tokenEnv}
          onChange={(v) => onChange({ ...value, tokenEnv: v })}
          helper={
            value.tokenConfigured
              ? `Resolved (${value.tokenEnv}): a token is currently set.`
              : value.tokenEnv
                ? `Env var ${value.tokenEnv} is empty — export the long-lived access token.`
                : "Set the env-var name that holds your HA long-lived access token."
          }
        />
        <LabeledInput
          label="Language override"
          placeholder="(empty = use user locale)"
          value={value.language}
          onChange={(v) => onChange({ ...value, language: v })}
        />
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={runProbe}
            disabled={status.kind === "running" || !value.url || !value.tokenEnv}
            className="rounded-lg border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-2)] px-3 py-1.5 text-xs font-medium text-[color:var(--sk-text)] transition-all hover:border-[color:var(--sk-accent)]/60 disabled:cursor-not-allowed disabled:opacity-50"
            data-testid="homeassistant-test-button"
          >
            {status.kind === "running" ? "Testing…" : "Test connection"}
          </button>
          <span
            className="text-[11px] leading-tight"
            data-testid="homeassistant-test-status"
          >
            {renderProbeStatus(status)}
          </span>
        </div>
      </div>
    </Section>
  );
}

function PiperSection({
  value,
  onChange,
}: {
  value: PiperTTSSettings;
  onChange: (next: PiperTTSSettings) => void;
}) {
  const [refreshError, setRefreshError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const localeRows = useMemo(() => buildLocaleRows(value), [value]);

  const refreshVoices = useCallback(async () => {
    setRefreshing(true);
    setRefreshError(null);
    try {
      const result = await fetchPiperVoices(value.voiceDir);
      onChange({
        ...value,
        voiceDir: result.voiceDir || value.voiceDir,
        availableVoices: result.voices,
      });
      if (result.error) {
        setRefreshError(result.error);
      }
    } catch (err) {
      setRefreshError(err instanceof Error ? err.message : String(err));
    } finally {
      setRefreshing(false);
    }
  }, [value, onChange]);

  return (
    <Section title="Piper Local Voices" testId="settings-section-piper">
      <p className="mb-3 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
        Piper synthesizes Assist replies fully offline. Point it at a
        directory of .onnx voice models (download via
        scripts/prepare-piper-voices.ps1 or .sh); the per-locale voice
        controls below pin which voice Piper uses for English/German.
      </p>
      <div className="grid gap-3">
        <div className="flex items-center justify-between">
          <span className="text-sm">Enable Piper provider</span>
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              className="h-4 w-4"
              checked={value.enabled}
              onChange={(e) => onChange({ ...value, enabled: e.target.checked })}
              data-testid="piper-enabled-checkbox"
            />
          </label>
        </div>
        <LabeledInput
          label="Voice directory"
          placeholder="C:/SpeechKit/piper-voices"
          value={value.voiceDir}
          onChange={(v) => onChange({ ...value, voiceDir: v })}
        />
        <LabeledInput
          label="Binary path (optional)"
          placeholder="piper"
          value={value.binary}
          onChange={(v) => onChange({ ...value, binary: v })}
          helper="Leave empty to use `piper` on $PATH."
        />
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={refreshVoices}
            disabled={refreshing || !value.voiceDir}
            className="rounded-lg border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-2)] px-3 py-1.5 text-xs font-medium text-[color:var(--sk-text)] transition-all hover:border-[color:var(--sk-accent)]/60 disabled:cursor-not-allowed disabled:opacity-50"
            data-testid="piper-refresh-button"
          >
            {refreshing ? "Scanning…" : "Refresh voice list"}
          </button>
          <span
            className="text-[11px] text-[color:var(--sk-text-muted)]"
            data-testid="piper-voice-count"
          >
            {value.availableVoices.length === 0
              ? value.voiceDir
                ? `No .onnx files found in ${value.voiceDir}`
                : "Set a voice directory to scan."
              : `${value.availableVoices.length} voice${value.availableVoices.length === 1 ? "" : "s"} installed`}
          </span>
        </div>
        {refreshError && (
          <span className="text-[11px] text-red-400" data-testid="piper-refresh-error">
            {refreshError}
          </span>
        )}
        <div className="grid gap-2 border-t border-[color:var(--sk-shell-divider)]/60 pt-3">
          <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
            Per-locale default voice
          </span>
          {localeRows.map((row) => (
            <LocaleVoiceRow
              key={row.code}
              code={row.code}
              label={row.label}
              currentVoice={row.currentVoice}
              voices={row.localeVoices}
              allVoices={value.availableVoices}
              onSelect={(filename) =>
                onChange({
                  ...value,
                  defaultVoices: {
                    ...value.defaultVoices,
                    [row.code]: filename,
                  },
                })
              }
              onClear={() => {
                const next = { ...value.defaultVoices };
                delete next[row.code];
                onChange({ ...value, defaultVoices: next });
              }}
            />
          ))}
        </div>
      </div>
    </Section>
  );
}

function LocaleVoiceRow({
  code,
  label,
  currentVoice,
  voices,
  allVoices,
  onSelect,
  onClear,
}: {
  code: string;
  label: string;
  currentVoice: string;
  voices: PiperVoiceSummary[];
  allVoices: PiperVoiceSummary[];
  onSelect: (filename: string) => void;
  onClear: () => void;
}) {
  // When there are no matching-locale voices but there are some
  // unrelated voices installed, fall back to listing all so the user
  // can still pick something for the locale.
  const options = voices.length > 0 ? voices : allVoices;
  return (
    <div className="flex items-center gap-3">
      <span className="w-24 shrink-0 text-xs text-[color:var(--sk-text-muted)]">
        {label}
      </span>
      <select
        value={currentVoice}
        onChange={(e) => {
          const v = e.target.value;
          if (v) onSelect(v);
          else onClear();
        }}
        className="min-w-0 flex-1 rounded-lg border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-2)] px-2 py-1.5 text-xs text-[color:var(--sk-text)]"
        data-testid={`piper-voice-select-${code}`}
      >
        <option value="">(use built-in default)</option>
        {options.map((v) => (
          <option key={v.filename} value={v.filename}>
            {voiceOptionLabel(v)}
          </option>
        ))}
      </select>
    </div>
  );
}

function buildLocaleRows(piper: PiperTTSSettings) {
  return LOCALE_PRESETS.map((preset) => {
    const localeVoices = piper.availableVoices.filter(
      (v) => v.locale === preset.code
    );
    return {
      code: preset.code,
      label: preset.label,
      currentVoice: piper.defaultVoices[preset.code] ?? "",
      localeVoices,
    };
  });
}

function voiceOptionLabel(v: PiperVoiceSummary): string {
  const parts: string[] = [];
  if (v.name) parts.push(v.name);
  if (v.quality) parts.push(v.quality);
  if (v.region) parts.push(v.region);
  const human = parts.length > 0 ? parts.join(" / ") : v.filename;
  const size = v.sizeKB > 0 ? ` — ${formatKB(v.sizeKB)}` : "";
  return `${human}${size}`;
}

function formatKB(kb: number): string {
  if (kb >= 1024) {
    return `${(kb / 1024).toFixed(1)} MB`;
  }
  return `${kb} KB`;
}

function renderProbeStatus(status: ProbeStatus) {
  switch (status.kind) {
    case "idle":
      return (
        <span className="text-[color:var(--sk-text-muted)]">
          Click "Test connection" to verify URL + token.
        </span>
      );
    case "running":
      return (
        <span className="text-[color:var(--sk-text-muted)]">Probing…</span>
      );
    case "ok":
      return (
        <span className="text-emerald-300">
          Connected{status.url ? ` to ${status.url}` : ""}.
        </span>
      );
    case "error":
      return <span className="text-red-400">{status.message}</span>;
  }
}

function LabeledInput({
  label,
  value,
  onChange,
  placeholder,
  helper,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  helper?: string;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs text-[color:var(--sk-text-muted)]">{label}</span>
      <input
        type="text"
        spellCheck={false}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className="rounded-lg border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-2)] px-2 py-1.5 text-xs text-[color:var(--sk-text)]"
      />
      {helper && (
        <span className="text-[10px] leading-relaxed text-[color:var(--sk-text-muted)]">
          {helper}
        </span>
      )}
    </label>
  );
}
