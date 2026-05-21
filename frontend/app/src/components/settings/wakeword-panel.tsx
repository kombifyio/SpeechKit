import { useState, type ChangeEvent } from "react";

import {
  runWakewordSelfTest,
  type SpeechKitSettingsState,
  type WakewordBackend,
  type WakewordBackendOption,
  type WakewordDefaultMode,
  type WakewordPhraseCatalogEntry,
  type WakewordSelfTestReport,
  type WakewordSettings,
} from "@/lib/speechkit";
import { Row, Section } from "./settings-primitives";

const DEFAULT_MODE_OPTIONS: { value: WakewordDefaultMode; label: string }[] = [
  { value: "voice_agent", label: "Voice Agent" },
  { value: "assist", label: "Assist" },
  { value: "dictate", label: "Dictation" },
];

export function WakewordPanel({
  settings,
  onChange,
}: {
  settings: SpeechKitSettingsState;
  onChange: (next: SpeechKitSettingsState) => void;
}) {
  const wake = settings.wakeword;

  const update = (patch: Partial<WakewordSettings>) => {
    onChange({ ...settings, wakeword: { ...wake, ...patch } });
  };

  const selectedEntry = findPhraseEntry(wake.phraseId, wake.phraseCatalog);
  const effectiveThreshold =
    wake.threshold > 0
      ? wake.threshold
      : selectedEntry?.recommendedThreshold ?? 0.5;

  return (
    <Section title="Wake word" testId="settings-wakeword">
      <div className="space-y-4">
        <WakewordStatus wake={wake} entry={selectedEntry} />

        <Row
          label="Enable wake word"
          on={wake.enabled}
          onToggle={() => update({ enabled: !wake.enabled })}
        />

        <p className="text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
          When enabled, SpeechKit listens continuously for the selected phrase
          and starts the chosen mode on detection. Audio for wake detection
          stays fully on-device — nothing leaves your machine.
        </p>

        <div
          className={`space-y-4 transition-opacity ${
            wake.enabled ? "opacity-100" : "pointer-events-none opacity-50"
          }`}
        >
          <BackendSelector
            backend={wake.backend}
            options={wake.backendOptions}
            disabled={!wake.enabled}
            onChange={(backend) => update({ backend })}
          />

          <PhraseSelector
            phraseId={wake.phraseId}
            catalog={wake.phraseCatalog}
            disabled={!wake.enabled}
            onChange={(phraseId) =>
              update({
                phraseId,
                // Reset user threshold so the new entry's recommended value
                // takes over until the user explicitly tunes again.
                threshold: 0,
              })
            }
          />

          <DefaultModeSelector
            value={wake.defaultMode}
            disabled={!wake.enabled}
            onChange={(defaultMode) => update({ defaultMode })}
          />

          <ThresholdInput
            value={wake.threshold}
            effective={effectiveThreshold}
            disabled={!wake.enabled}
            onChange={(threshold) => update({ threshold })}
          />

          <details className="rounded-lg border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 py-2">
            <summary className="cursor-pointer text-xs font-medium text-[color:var(--sk-text)]">
              Advanced
            </summary>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <NumberField
                label="Min consecutive frames"
                hint="2 = default. Higher = stricter (fewer false-accepts, more false-rejects)."
                value={wake.minConsecutiveFrames}
                min={1}
                max={10}
                disabled={!wake.enabled}
                onChange={(minConsecutiveFrames) =>
                  update({ minConsecutiveFrames })
                }
              />
              <NumberField
                label="Cooldown (ms)"
                hint="Minimum gap between two triggers. 1500 = default."
                value={wake.cooldownMs}
                min={0}
                max={10000}
                step={100}
                disabled={!wake.enabled}
                onChange={(cooldownMs) => update({ cooldownMs })}
              />
            </div>
            <div className="mt-3">
              <Row
                label="Debug mode (verbose detector scores in log feed)"
                on={wake.debugMode}
                onToggle={() => update({ debugMode: !wake.debugMode })}
              />
              <p className="mt-1 text-[10px] leading-snug text-[color:var(--sk-text-muted)]">
                Forwards every above-threshold-adjacent score from openWakeWord and
                enables sherpa-onnx C++ verbose logging. Use this while tuning a
                phrase, then switch off — the stream is chatty.
              </p>
            </div>
          </details>

          <SelfTestPanel disabled={!wake.enabled} />
        </div>
      </div>
    </Section>
  );
}

function SelfTestPanel({ disabled }: { disabled: boolean }) {
  const [running, setRunning] = useState(false);
  const [report, setReport] = useState<WakewordSelfTestReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const handleRun = async () => {
    setRunning(true);
    setError(null);
    setReport(null);
    try {
      const next = await runWakewordSelfTest();
      setReport(next);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="rounded-lg border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 py-3">
      <div className="flex items-center justify-between">
        <div>
          <h4 className="text-xs font-semibold text-[color:var(--sk-text)]">
            Test microphone (3 s capture)
          </h4>
          <p className="text-[10px] leading-snug text-[color:var(--sk-text-muted)]">
            Records a short sample from the currently selected mic and reports
            level + resolved device name. Run this first when wake-word doesn't
            fire.
          </p>
        </div>
        <button
          type="button"
          data-testid="wakeword-selftest-run"
          onClick={handleRun}
          disabled={disabled || running}
          className="rounded-md border border-[color:var(--sk-accent)] bg-[color:var(--sk-accent)] px-3 py-1.5 text-xs font-medium text-white shadow-sm transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {running ? "Recording..." : "Test microphone"}
        </button>
      </div>

      {error && (
        <p
          data-testid="wakeword-selftest-error"
          className="mt-2 text-[11px] text-red-500"
        >
          {error}
        </p>
      )}

      {report && (
        <div
          data-testid="wakeword-selftest-report"
          className="mt-3 space-y-1 text-[11px] leading-relaxed text-[color:var(--sk-text)]"
        >
          <p>
            <strong>Device:</strong> {report.resolved_device_name}{" "}
            <span className="text-[color:var(--sk-text-muted)]">
              ({report.device_kind})
            </span>
          </p>
          <p>
            <strong>Captured:</strong> {report.captured_bytes.toLocaleString()} B
            over {report.duration_ms} ms · peak {report.peak_level.toFixed(3)} ·
            RMS {report.rms_level.toFixed(3)}
          </p>
          <p>
            <strong>Backend:</strong> {report.backend} · threshold{" "}
            {report.effective_threshold.toFixed(2)}
          </p>
          <p className="rounded bg-[color:var(--sk-surface-1)] p-2 text-[color:var(--sk-text)]">
            {report.advice}
          </p>
        </div>
      )}
    </div>
  );
}

function BackendSelector({
  backend,
  options,
  disabled,
  onChange,
}: {
  backend: WakewordBackend;
  options: WakewordBackendOption[];
  disabled: boolean;
  onChange: (next: WakewordBackend) => void;
}) {
  const handleChange = (e: ChangeEvent<HTMLSelectElement>) => {
    onChange(e.target.value as WakewordBackend);
  };
  return (
    <div>
      <label className="mb-1.5 block text-xs font-medium text-[color:var(--sk-text)]">
        Detection backend
      </label>
      <select
        data-testid="wakeword-backend-select"
        value={backend}
        onChange={handleChange}
        disabled={disabled}
        className="w-full rounded-md border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] px-3 py-2 text-sm text-[color:var(--sk-text)] focus:border-[color:var(--sk-accent)] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
      >
        {options.map((opt) => (
          <option key={opt.id} value={opt.id}>
            {opt.label}
            {opt.recommended ? " (recommended)" : ""}
          </option>
        ))}
      </select>
    </div>
  );
}

function WakewordStatus({
  wake,
  entry,
}: {
  wake: WakewordSettings;
  entry: WakewordPhraseCatalogEntry | null;
}) {
  const indicatorColor = wake.active
    ? "bg-green-500"
    : wake.enabled
    ? "bg-amber-500"
    : "bg-[color:var(--sk-border)]";
  const headline = wake.active
    ? "Listening"
    : wake.enabled
    ? "Enabled (not active)"
    : "Disabled";
  const fallbackMessage = entry
    ? `${entry.displayName} — ${entry.notes}`
    : "Pick a phrase to see its tradeoffs.";

  return (
    <div
      data-testid="wakeword-status"
      className="rounded-lg border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 py-2"
    >
      <div className="flex items-center gap-2">
        <span
          aria-hidden
          className={`h-2 w-2 rounded-full ${indicatorColor}`}
        />
        <span className="text-xs font-semibold text-[color:var(--sk-text)]">
          {headline}
        </span>
      </div>
      <p className="mt-1 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
        {wake.statusMessage || fallbackMessage}
      </p>
    </div>
  );
}

function PhraseSelector({
  phraseId,
  catalog,
  disabled,
  onChange,
}: {
  phraseId: string;
  catalog: WakewordPhraseCatalogEntry[];
  disabled: boolean;
  onChange: (next: string) => void;
}) {
  const handleChange = (e: ChangeEvent<HTMLSelectElement>) => {
    onChange(e.target.value);
  };

  const entry = findPhraseEntry(phraseId, catalog);

  return (
    <div>
      <label className="mb-1.5 block text-xs font-medium text-[color:var(--sk-text)]">
        Wake phrase
      </label>
      <select
        data-testid="wakeword-phrase-select"
        value={phraseId}
        onChange={handleChange}
        disabled={disabled}
        className="w-full rounded-md border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] px-3 py-2 text-sm text-[color:var(--sk-text)] focus:border-[color:var(--sk-accent)] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
      >
        {catalog.length === 0 ? (
          <option value="">Catalog unavailable</option>
        ) : (
          catalog.map((c) => (
            <option key={c.id} value={c.id}>
              {c.displayName}
            </option>
          ))
        )}
      </select>
      {entry && (
        <p className="mt-1.5 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
          {entry.notes}
        </p>
      )}
    </div>
  );
}

function DefaultModeSelector({
  value,
  disabled,
  onChange,
}: {
  value: WakewordDefaultMode;
  disabled: boolean;
  onChange: (next: WakewordDefaultMode) => void;
}) {
  const handleChange = (e: ChangeEvent<HTMLSelectElement>) => {
    onChange(e.target.value as WakewordDefaultMode);
  };
  return (
    <div>
      <label className="mb-1.5 block text-xs font-medium text-[color:var(--sk-text)]">
        Trigger mode
      </label>
      <select
        data-testid="wakeword-default-mode-select"
        value={value}
        onChange={handleChange}
        disabled={disabled}
        className="w-full rounded-md border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] px-3 py-2 text-sm text-[color:var(--sk-text)] focus:border-[color:var(--sk-accent)] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
      >
        {DEFAULT_MODE_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      <p className="mt-1.5 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
        Which mode starts when the wake phrase fires. Voice Agent is the most
        natural fit for continuous conversation.
      </p>
    </div>
  );
}

function ThresholdInput({
  value,
  effective,
  disabled,
  onChange,
}: {
  value: number;
  effective: number;
  disabled: boolean;
  onChange: (next: number) => void;
}) {
  const handleChange = (e: ChangeEvent<HTMLInputElement>) => {
    const parsed = Number.parseFloat(e.target.value);
    onChange(Number.isFinite(parsed) ? parsed : 0);
  };
  const usingRecommended = value <= 0;
  return (
    <div>
      <label className="mb-1.5 block text-xs font-medium text-[color:var(--sk-text)]">
        Detection threshold
      </label>
      <div className="flex items-center gap-3">
        <input
          data-testid="wakeword-threshold-input"
          type="number"
          step={0.01}
          min={0}
          max={1}
          value={value}
          onChange={handleChange}
          disabled={disabled}
          className="w-24 rounded-md border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] px-2 py-1.5 text-sm text-[color:var(--sk-text)] focus:border-[color:var(--sk-accent)] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
        />
        <span className="text-[11px] text-[color:var(--sk-text-muted)]">
          {usingRecommended
            ? `Using recommended value: ${effective.toFixed(2)}`
            : `Manual override (recommended for this phrase: ${effective.toFixed(
                2
              )})`}
        </span>
      </div>
      <p className="mt-1.5 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
        Range (0, 1]. Lower = more sensitive (more false-accepts), higher =
        stricter (more false-rejects). Set to 0 to use the recommended value for
        the selected phrase.
      </p>
    </div>
  );
}

function NumberField({
  label,
  hint,
  value,
  min,
  max,
  step = 1,
  disabled,
  onChange,
}: {
  label: string;
  hint: string;
  value: number;
  min: number;
  max: number;
  step?: number;
  disabled: boolean;
  onChange: (next: number) => void;
}) {
  const handleChange = (e: ChangeEvent<HTMLInputElement>) => {
    const parsed = Number.parseInt(e.target.value, 10);
    onChange(Number.isFinite(parsed) ? parsed : 0);
  };
  return (
    <div>
      <label className="mb-1 block text-[11px] font-medium text-[color:var(--sk-text)]">
        {label}
      </label>
      <input
        type="number"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={handleChange}
        disabled={disabled}
        className="w-full rounded-md border border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] px-2 py-1.5 text-sm text-[color:var(--sk-text)] focus:border-[color:var(--sk-accent)] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
      />
      <p className="mt-1 text-[10px] leading-snug text-[color:var(--sk-text-muted)]">
        {hint}
      </p>
    </div>
  );
}

function findPhraseEntry(
  id: string,
  catalog: WakewordPhraseCatalogEntry[]
): WakewordPhraseCatalogEntry | null {
  const normalized = id.trim().toLowerCase();
  if (!normalized) return null;
  return catalog.find((c) => c.id === normalized) ?? null;
}
