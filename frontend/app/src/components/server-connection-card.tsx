/**
 * Settings card that exposes the device-target [server_connection]
 * configuration. Reads via fetchAPIV1ServerConnection on mount, writes
 * back via patchAPIV1ServerConnection on input commit.
 *
 * Bearer tokens are never read from this UI — only the env var name
 * the device should look up at startup. The "is the env var set?"
 * boolean from the backend drives a status hint so the user knows
 * whether their token-management story is wired.
 */

import { useCallback, useEffect, useState } from "react";

import {
  fetchAPIV1ServerConnection,
  patchAPIV1ServerConnection,
  type ServerConnectionSetting,
} from "@/lib/speechkit";

import { cn } from "@/lib/utils";

export type ServerConnectionCardProps = {
  className?: string;
  /**
   * Called whenever a successful PATCH returns. Lets the surrounding
   * settings page refresh its own ServerConnectionSetting copy so
   * dependent UI (e.g. mode-source toggles) re-renders consistently.
   */
  onSettingsChange?: (next: ServerConnectionSetting) => void;
};

export function ServerConnectionCard({
  className,
  onSettingsChange,
}: ServerConnectionCardProps) {
  const [setting, setSetting] = useState<ServerConnectionSetting | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Local form state — kept separate so users can edit without thrashing
  // the persisted server config on every keystroke.
  const [url, setUrl] = useState("");
  const [bearerEnv, setBearerEnv] = useState("");
  const [timeoutSec, setTimeoutSec] = useState(30);

  useEffect(() => {
    let cancelled = false;
    fetchAPIV1ServerConnection()
      .then((data) => {
        if (cancelled) return;
        setSetting(data);
        setUrl(data.url ?? "");
        setBearerEnv(data.bearerTokenEnv ?? "SPEECHKIT_SERVER_TOKEN");
        setTimeoutSec(data.requestTimeoutSec || 30);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(String(err?.message ?? err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const submitPatch = useCallback(
    async (patch: Partial<ServerConnectionSetting>) => {
      setBusy(true);
      setError(null);
      try {
        const next = await patchAPIV1ServerConnection({
          enabled: patch.enabled,
          url: patch.url,
          bearerTokenEnv: patch.bearerTokenEnv,
          fallbackToLocal: patch.fallbackToLocal,
          requestTimeoutSec: patch.requestTimeoutSec,
        });
        setSetting(next);
        if (onSettingsChange) onSettingsChange(next);
      } catch (err) {
        setError(String((err as Error)?.message ?? err));
      } finally {
        setBusy(false);
      }
    },
    [onSettingsChange],
  );

  if (error && !setting) {
    return (
      <div
        className={cn(
          "rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive",
          className,
        )}
      >
        Failed to load server connection settings: {error}
      </div>
    );
  }

  if (!setting) {
    return (
      <div className={cn("rounded-lg border border-border/60 p-4", className)}>
        <p className="text-sm text-muted-foreground">
          Loading server connection…
        </p>
      </div>
    );
  }

  const tokenStatusBadge = setting.enabled
    ? setting.bearerTokenSet
      ? (
        <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
          token set
        </span>
      )
      : (
        <span className="inline-flex items-center rounded-full bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
          token missing
        </span>
      )
    : (
      <span className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
        disabled
      </span>
    );

  return (
    <section
      className={cn(
        "flex flex-col gap-4 rounded-lg border border-border/60 bg-background p-5 shadow-sm",
        className,
      )}
      aria-busy={busy}
    >
      <header className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-base font-semibold text-foreground">
            Server Connection
          </h3>
          <p className="text-sm text-muted-foreground">
            Optional remote SpeechKit server. When enabled, individual modes
            can be routed through it via their per-mode source toggle.
            Settings take effect on next app start.
          </p>
        </div>
        {tokenStatusBadge}
      </header>

      <div className="flex items-center justify-between gap-3 rounded-md bg-muted/50 px-3 py-2">
        <div>
          <p className="text-sm font-medium">Enable server connection</p>
          <p className="text-xs text-muted-foreground">
            Off keeps every mode strictly local regardless of per-mode source.
          </p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={setting.enabled}
          disabled={busy}
          onClick={() => submitPatch({ enabled: !setting.enabled })}
          className={cn(
            "inline-flex h-6 w-11 items-center rounded-full transition-colors",
            setting.enabled ? "bg-foreground" : "bg-muted-foreground/30",
          )}
        >
          <span
            className={cn(
              "h-5 w-5 transform rounded-full bg-background transition-transform",
              setting.enabled ? "translate-x-5" : "translate-x-0.5",
            )}
          />
        </button>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Server URL</span>
          <input
            type="url"
            placeholder="https://speechkit.example.com"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onBlur={() => {
              if (url.trim() !== (setting.url ?? "")) {
                submitPatch({ url: url.trim() });
              }
            }}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Bearer Token Env Var</span>
          <input
            type="text"
            placeholder="SPEECHKIT_SERVER_TOKEN"
            value={bearerEnv}
            onChange={(e) => setBearerEnv(e.target.value)}
            onBlur={() => {
              const trimmed = bearerEnv.trim();
              if (trimmed !== (setting.bearerTokenEnv ?? "")) {
                submitPatch({ bearerTokenEnv: trimmed });
              }
            }}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          />
          <span className="text-xs text-muted-foreground">
            Env var the device process reads at startup. The token value
            never travels through the UI.
          </span>
        </label>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="flex items-center justify-between gap-3 rounded-md bg-muted/50 px-3 py-2">
          <div>
            <p className="text-sm font-medium">Fallback to local on error</p>
            <p className="text-xs text-muted-foreground">
              Retry against the in-process kernel if the server fails.
            </p>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={setting.fallbackToLocal}
            disabled={busy}
            onClick={() =>
              submitPatch({ fallbackToLocal: !setting.fallbackToLocal })
            }
            className={cn(
              "inline-flex h-6 w-11 items-center rounded-full transition-colors",
              setting.fallbackToLocal
                ? "bg-foreground"
                : "bg-muted-foreground/30",
            )}
          >
            <span
              className={cn(
                "h-5 w-5 transform rounded-full bg-background transition-transform",
                setting.fallbackToLocal ? "translate-x-5" : "translate-x-0.5",
              )}
            />
          </button>
        </div>
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Request timeout (s)</span>
          <input
            type="number"
            min={0}
            max={3600}
            value={timeoutSec}
            onChange={(e) => setTimeoutSec(Number.parseInt(e.target.value, 10) || 0)}
            onBlur={() => {
              if (timeoutSec !== setting.requestTimeoutSec) {
                submitPatch({ requestTimeoutSec: timeoutSec });
              }
            }}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          />
          <span className="text-xs text-muted-foreground">
            Caps non-streaming HTTP calls. WebSocket sessions are not
            affected.
          </span>
        </label>
      </div>

      {error ? (
        <p className="text-sm text-destructive">{error}</p>
      ) : null}
    </section>
  );
}
