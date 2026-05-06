/**
 * Composite component that renders the per-mode local-vs-server
 * toggles for Dictation / Assist / Voice Agent in a single place,
 * driven off the device-target's /api/v1/modes settings + the
 * /api/v1/server-connection state. Used by the SpeechKit Server settings page.
 *
 * Self-contained: fetches everything it needs on mount, persists each
 * toggle change immediately. Surfaces errors inline so users see why
 * a write didn't stick.
 */

import { useCallback, useEffect, useState } from "react";

import { ModeSourceToggle } from "@/components/mode-source-toggle";
import {
  fetchAPIV1Modes,
  fetchAPIV1ServerConnection,
  patchAPIV1ModeSettings,
  patchAPIV1ServerConnection,
  type APIV1ModeSettings,
  type ModeSource,
  type ServerConnectionSetting,
} from "@/lib/speechkit";

import { cn } from "@/lib/utils";

// patchAPIV1ModeSettings expects snake_case mode keys; APIV1ModeSettings
// is keyed in camelCase. We carry both forms so each side gets the
// shape it expects without ad-hoc conversions in the render path.
type ModeKey = "dictation" | "assist" | "voice_agent";
type SettingsKey = "dictation" | "assist" | "voiceAgent";

const MODE_LABELS: Record<ModeKey, string> = {
  dictation: "Dictation",
  assist: "Assist",
  voice_agent: "Voice Agent",
};

const SETTINGS_KEY: Record<ModeKey, SettingsKey> = {
  dictation: "dictation",
  assist: "assist",
  voice_agent: "voiceAgent",
};

const MODE_KEYS: ModeKey[] = ["dictation", "assist", "voice_agent"];

export type ModeSourceSectionProps = {
  className?: string;
  /**
   * Optional pre-fetched ServerConnectionSetting. Skipping the auto-
   * fetch lets a parent component drive the lifecycle in lockstep
   * with its own ServerConnectionCard.
   */
  serverConnection?: ServerConnectionSetting;
  onServerConnectionChange?: (next: ServerConnectionSetting) => void;
};

export function ModeSourceSection({
  className,
  serverConnection: externalServerConnection,
  onServerConnectionChange,
}: ModeSourceSectionProps) {
  const [modes, setModes] = useState<APIV1ModeSettings | null>(null);
  const [fetchedServerConnection, setFetchedServerConnection] =
    useState<ServerConnectionSetting | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Derived state: when the parent passes a serverConnection prop, we
  // mirror it directly without going through useState. Only the
  // self-fetched path needs an effect — that keeps the React-compiler
  // happy (no synchronous setState inside an effect body) and avoids
  // re-render churn when the parent prop identity changes.
  const serverConnection = externalServerConnection ?? fetchedServerConnection;

  const applyServerConnection = useCallback(
    (nextConnection: ServerConnectionSetting) => {
      setFetchedServerConnection(nextConnection);
      onServerConnectionChange?.(nextConnection);
    },
    [onServerConnectionChange],
  );

  useEffect(() => {
    let cancelled = false;
    fetchAPIV1Modes()
      .then((data) => {
        if (cancelled) return;
        setModes(data.settings);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(String(err?.message ?? err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    // Skip the self-fetch when the parent supplied the value.
    if (externalServerConnection) {
      return;
    }
    let cancelled = false;
    fetchAPIV1ServerConnection()
      .then((data) => {
        if (cancelled) return;
        setFetchedServerConnection(data);
      })
      .catch(() => {
        // Surface only via the modes error so we don't double-toast.
      });
    return () => {
      cancelled = true;
    };
  }, [externalServerConnection]);

  const updateMode = useCallback(
    async (mode: ModeKey, next: ModeSource) => {
      setBusy(true);
      setError(null);
      try {
        if (next === "server" && !serverConnection?.enabled) {
          applyServerConnection(
            await patchAPIV1ServerConnection({ enabled: true }),
          );
        }
        const updated = await patchAPIV1ModeSettings(mode, { modeSource: next });
        const settingsKey = SETTINGS_KEY[mode];
        setModes((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            [settingsKey]: { ...prev[settingsKey], ...updated },
          };
        });
      } catch (err) {
        setError(String((err as Error)?.message ?? err));
      } finally {
        setBusy(false);
      }
    },
    [applyServerConnection, serverConnection?.enabled],
  );

  const updateAllModes = useCallback(
    async (next: ModeSource) => {
      if (!modes) return;
      setBusy(true);
      setError(null);
      try {
        if (next === "server" && !serverConnection?.enabled) {
          applyServerConnection(
            await patchAPIV1ServerConnection({ enabled: true }),
          );
        }
        const updates = await Promise.all(
          MODE_KEYS.map(async (mode) => {
            const updated = await patchAPIV1ModeSettings(mode, { modeSource: next });
            return [mode, updated] as const;
          }),
        );
        setModes((prev) => {
          if (!prev) return prev;
          return updates.reduce<APIV1ModeSettings>((acc, [mode, updated]) => {
            const settingsKey = SETTINGS_KEY[mode];
            return {
              ...acc,
              [settingsKey]: { ...acc[settingsKey], ...updated },
            };
          }, prev);
        });
        if (next === "local") {
          applyServerConnection(
            await patchAPIV1ServerConnection({ enabled: false }),
          );
        }
      } catch (err) {
        setError(String((err as Error)?.message ?? err));
      } finally {
        setBusy(false);
      }
    },
    [applyServerConnection, modes, serverConnection],
  );

  if (error && !modes) {
    return (
      <div
        className={cn(
          "rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive",
          className,
        )}
      >
        Failed to load mode source settings: {error}
      </div>
    );
  }

  if (!modes) {
    return (
      <div className={cn("rounded-lg border border-border/60 p-4", className)}>
        <p className="text-sm text-muted-foreground">Loading…</p>
      </div>
    );
  }

  const currentSources = MODE_KEYS.map(
    (mode) => modes[SETTINGS_KEY[mode]]?.modeSource ?? "local",
  );
  const allLocal = currentSources.every((value) => value !== "server");
  const allServer = currentSources.every((value) => value === "server");
  const globalSourceLabel = allServer ? "Server" : allLocal ? "Local" : "Custom";

  return (
    <section
      className={cn(
        "flex flex-col gap-3 rounded-lg border border-border/60 bg-background p-5 shadow-sm",
        className,
      )}
    >
      <header>
        <h3 className="text-base font-semibold text-foreground">Mode Source</h3>
        <p className="text-sm text-muted-foreground">
          Switch all modes between the local Framework runtime and the configured
          SpeechKit server, then override individual modes below.
        </p>
      </header>
      <div className="flex flex-col gap-2 rounded-lg border border-border/60 bg-muted/40 p-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="text-sm font-medium text-foreground">Global runtime</p>
          <p className="text-xs text-muted-foreground">Current: {globalSourceLabel}</p>
        </div>
        <div
          role="radiogroup"
          aria-label="Global mode source"
          className="inline-flex w-fit items-center rounded-md border border-border bg-background p-0.5"
        >
          <button
            type="button"
            role="radio"
            aria-checked={allLocal}
            disabled={busy}
            onClick={() => updateAllModes("local")}
            className={cn(
              "rounded-sm px-3 py-1 text-xs font-medium transition-colors",
              allLocal
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            Local
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={allServer}
            disabled={busy}
            onClick={() => updateAllModes("server")}
            className={cn(
              "rounded-sm px-3 py-1 text-xs font-medium transition-colors",
              allServer
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            Server
          </button>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-2.5">
        {MODE_KEYS.map((mode) => {
          const value = modes[SETTINGS_KEY[mode]]?.modeSource ?? "local";
          return (
            <ModeSourceToggle
              key={mode}
              modeLabel={MODE_LABELS[mode]}
              value={value as ModeSource}
              onChange={(next) => updateMode(mode, next)}
              serverConnection={serverConnection}
            />
          );
        })}
      </div>
      {error ? <p className="text-sm text-destructive">{error}</p> : null}
    </section>
  );
}
