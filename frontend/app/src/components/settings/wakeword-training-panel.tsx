import { useCallback, useEffect, useState } from "react";

import {
  deleteWakewordActivation,
  listWakewordActivations,
  updateWakewordActivationLabel,
  type LocalWakewordActivation,
  type LocalWakewordActivationLabel,
  type WakewordActivationsListResponse,
} from "@/lib/speechkit";
import { Section } from "./settings-primitives";

const LABEL_OPTIONS: { value: LocalWakewordActivationLabel; label: string }[] = [
  { value: "", label: "Unlabelled" },
  { value: "correct", label: "Correct" },
  { value: "false_positive", label: "False positive" },
];

// WakewordTrainingPanel renders the v0.37.6 settings tab for browsing,
// labelling, and deleting the wake-word activation clips captured by the
// sidecar (v0.37.4). It talks to the local control plane endpoints in
// cmd/speechkit/routes_wakeword_activations.go.
//
// Privacy: when `enabled=false` in the list response (no
// `[wakeword.training_data].local_capture_enabled` in TOML), the
// panel renders an explainer instead of an empty list.
export function WakewordTrainingPanel() {
  const [data, setData] = useState<WakewordActivationsListResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setError(null);
      const next = await listWakewordActivations();
      setData(next);
    } catch (e: unknown) {
      setError((e as Error).message ?? String(e));
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const onLabelChange = useCallback(
    async (id: string, label: LocalWakewordActivationLabel) => {
      try {
        setBusyId(id);
        await updateWakewordActivationLabel(id, label);
        await refresh();
      } catch (e: unknown) {
        setError((e as Error).message ?? String(e));
      } finally {
        setBusyId(null);
      }
    },
    [refresh],
  );

  const onDelete = useCallback(
    async (id: string) => {
      if (!window.confirm("Delete this activation? The .wav and .json files are removed from disk.")) {
        return;
      }
      try {
        setBusyId(id);
        await deleteWakewordActivation(id);
        await refresh();
      } catch (e: unknown) {
        setError((e as Error).message ?? String(e));
      } finally {
        setBusyId(null);
      }
    },
    [refresh],
  );

  return (
    <Section title="Wake-word training data" testId="settings-wakeword-training">
      <div className="space-y-3">
        <p className="text-sm text-muted-foreground">
          Browse, label, and delete the activation clips that the wake-word
          detector saved to your local disk. Captured clips help improve
          recognition over time when you opt into the upload pipeline.
        </p>
        {!data && !error && <div className="text-sm text-muted-foreground">Loading…</div>}
        {error && (
          <div
            className="rounded border border-destructive bg-destructive/10 p-2 text-sm text-destructive"
            data-testid="wakeword-training-error"
          >
            {error}
          </div>
        )}
        {data && !data.enabled && data.count === 0 && (
          <div
            className="rounded border border-border bg-muted/40 p-3 text-sm text-muted-foreground"
            data-testid="wakeword-training-disabled"
          >
            Activation capture is currently disabled. Enable it in the Wake-word
            settings tab to start saving clips locally.
          </div>
        )}
        {data && data.enabled && data.count === 0 && (
          <div
            className="rounded border border-border bg-muted/40 p-3 text-sm text-muted-foreground"
            data-testid="wakeword-training-empty"
          >
            No activations captured yet. Trigger the wake-word a few times and
            then reload this panel.
          </div>
        )}
        {data && data.count > 0 && (
          <div className="space-y-2" data-testid="wakeword-training-list">
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>
                {data.count} captured · stored in <code>{data.dir}</code>
              </span>
              <button
                type="button"
                onClick={() => void refresh()}
                className="rounded border border-border px-2 py-1 hover:bg-muted"
                data-testid="wakeword-training-refresh"
              >
                Refresh
              </button>
            </div>
            {data.activations.map((a) => (
              <WakewordTrainingRow
                key={a.id}
                activation={a}
                busy={busyId === a.id}
                onLabel={(label) => void onLabelChange(a.id, label)}
                onDelete={() => void onDelete(a.id)}
              />
            ))}
          </div>
        )}
      </div>
    </Section>
  );
}

function WakewordTrainingRow({
  activation,
  busy,
  onLabel,
  onDelete,
}: {
  activation: LocalWakewordActivation;
  busy: boolean;
  onLabel: (label: LocalWakewordActivationLabel) => void;
  onDelete: () => void;
}) {
  const captured = formatTimestamp(activation.captured_at);
  return (
    <div
      className="rounded border border-border bg-card p-3"
      data-testid={`wakeword-training-row-${activation.id}`}
    >
      <div className="flex items-baseline justify-between gap-2">
        <div className="text-sm">
          <span className="font-medium">{activation.phrase || activation.phrase_id}</span>{" "}
          <span className="text-muted-foreground">· score {activation.score.toFixed(2)}</span>
        </div>
        <div className="text-xs text-muted-foreground">{captured}</div>
      </div>
      <div className="mt-2">
        <audio
          src={activation.audio_url}
          controls
          preload="none"
          className="w-full"
          data-testid={`wakeword-training-audio-${activation.id}`}
        />
      </div>
      <div className="mt-2 flex items-center gap-2">
        <label className="text-xs text-muted-foreground" htmlFor={`label-${activation.id}`}>
          Label
        </label>
        <select
          id={`label-${activation.id}`}
          value={activation.label}
          disabled={busy}
          onChange={(e) => onLabel(e.target.value as LocalWakewordActivationLabel)}
          className="rounded border border-border bg-background px-2 py-1 text-sm"
          data-testid={`wakeword-training-label-${activation.id}`}
        >
          {LABEL_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        <span className="text-xs text-muted-foreground">
          {activation.uploaded ? "Uploaded" : "Local only"}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          onClick={onDelete}
          disabled={busy}
          className="rounded border border-destructive px-2 py-1 text-xs text-destructive hover:bg-destructive/10"
          data-testid={`wakeword-training-delete-${activation.id}`}
        >
          Delete
        </button>
      </div>
    </div>
  );
}

function formatTimestamp(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}
