/**
 * Settings card for the device-target [server_connection] configuration.
 * Server targets are user/operator registered. Product builds do not ship
 * operator-specific presets; private endpoints can be added locally when needed.
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus } from "lucide-react";

import {
  fetchAPIV1ServerConnection,
  patchAPIV1ServerConnection,
  testAPIV1ServerConnection,
  type ServerConnectionSetting,
  type ServerConnectionSmokeResponse,
  type ServerConnectionTarget,
} from "@/lib/speechkit";

import { cn } from "@/lib/utils";

type AuthMode = "bearer" | "api_key";

type NewServerForm = {
  label: string;
  url: string;
  bearerTokenEnv: string;
  authMode: AuthMode;
  token: string;
};

const emptyNewServerForm: NewServerForm = {
  label: "",
  url: "",
  bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
  authMode: "bearer",
  token: "",
};

export type ServerConnectionCardProps = {
  className?: string;
  serverConnection?: ServerConnectionSetting;
  /**
   * Called whenever a successful PATCH returns. Lets the surrounding
   * settings page refresh its own ServerConnectionSetting copy so
   * dependent UI (e.g. mode-source toggles) re-renders consistently.
   */
  onSettingsChange?: (next: ServerConnectionSetting) => void;
};

export function ServerConnectionCard({
  className,
  serverConnection,
  onSettingsChange,
}: ServerConnectionCardProps) {
  const [internalSetting, setInternalSetting] =
    useState<ServerConnectionSetting | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [smokeBusy, setSmokeBusy] = useState(false);
  const [smokeResult, setSmokeResult] =
    useState<ServerConnectionSmokeResponse | null>(null);
  const [showNewServer, setShowNewServer] = useState(false);
  const [newServer, setNewServer] =
    useState<NewServerForm>(emptyNewServerForm);
  const setting = serverConnection ?? internalSetting;

  const [url, setUrl] = useState("");
  const [bearerEnv, setBearerEnv] = useState("");
  const [authMode, setAuthMode] = useState<AuthMode>("bearer");
  const [timeoutSec, setTimeoutSec] = useState(30);
  const [fallbackToLocal, setFallbackToLocal] = useState(true);

  useEffect(() => {
    if (serverConnection) return;
    let cancelled = false;
    fetchAPIV1ServerConnection()
      .then((data) => {
        if (cancelled) return;
        setInternalSetting(data);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(String(err?.message ?? err));
      });
    return () => {
      cancelled = true;
    };
  }, [serverConnection]);

  useEffect(() => {
    if (!setting) return;
    setUrl(setting.url ?? "");
    setBearerEnv(setting.bearerTokenEnv ?? "SPEECHKIT_SERVER_TOKEN");
    setAuthMode(setting.authMode ?? "bearer");
    setTimeoutSec(setting.requestTimeoutSec || 30);
    setFallbackToLocal(setting.fallbackToLocal);
  }, [setting]);

  const targets = useMemo(
    () => serverTargetsForSetting(setting),
    [setting],
  );
  const activeTargetId = activeTargetIDForSetting(setting, targets);
  const activeTarget = targets.find((target) => target.id === activeTargetId);

  const submitPatch = useCallback(
    async (
      patch: Partial<
        ServerConnectionSetting & {
          activeTargetId: string;
          targets: ServerConnectionTarget[];
          token: string;
        }
      >,
    ) => {
      setBusy(true);
      setError(null);
      try {
        const next = await patchAPIV1ServerConnection({
          enabled: patch.enabled,
          activeTargetId: patch.activeTargetId,
          url: patch.url,
          bearerTokenEnv: patch.bearerTokenEnv,
          authMode: patch.authMode,
          fallbackToLocal: patch.fallbackToLocal,
          requestTimeoutSec: patch.requestTimeoutSec,
          targets: patch.targets,
          token: patch.token,
        });
        setInternalSetting(next);
        onSettingsChange?.(next);
        return next;
      } catch (err) {
        setError(String((err as Error)?.message ?? err));
        return null;
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

  const tokenStatusBadge = setting.bearerTokenSet ? (
    <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
      token set
    </span>
  ) : (
    <span className="inline-flex items-center rounded-full bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
      token missing
    </span>
  );

  const selectTarget = (targetID: string) => {
    const target = targets.find((candidate) => candidate.id === targetID);
    if (!target) return;
    setUrl(target.url);
    setBearerEnv(target.bearerTokenEnv ?? "SPEECHKIT_SERVER_TOKEN");
    setAuthMode(target.authMode ?? "bearer");
    setTimeoutSec(target.requestTimeoutSec || 30);
    setFallbackToLocal(target.fallbackToLocal);
    void submitPatch({ activeTargetId: target.id });
  };

  const testActiveTarget = async () => {
    await runSmokeTest({
      url,
      bearerTokenEnv: bearerEnv,
      authMode,
      requestTimeoutSec: timeoutSec,
    });
  };

  const runSmokeTest = async (request: {
    url: string;
    bearerTokenEnv?: string;
    authMode?: AuthMode;
    token?: string;
    requestTimeoutSec?: number;
  }) => {
    setSmokeBusy(true);
    setError(null);
    setSmokeResult(null);
    try {
      const result = await testAPIV1ServerConnection(request);
      setSmokeResult(result);
      return result;
    } catch (err) {
      const message = String((err as Error)?.message ?? err);
      setSmokeResult({ ok: false, status: "failed", message });
      return null;
    } finally {
      setSmokeBusy(false);
    }
  };

  const registerNewServer = async () => {
    const trimmedURL = newServer.url.trim();
    if (!trimmedURL) {
      setError("Server URL is required");
      return;
    }
    const label =
      newServer.label.trim() || labelFromServerURL(trimmedURL) || "Server";
    const id = serverTargetID(label, trimmedURL, targets);
    const target: ServerConnectionTarget = {
      id,
      label,
      url: trimmedURL,
      authMode: newServer.authMode,
      bearerTokenEnv:
        newServer.bearerTokenEnv.trim() || defaultTokenEnv(),
      bearerTokenSet: newServer.token.trim() !== "",
      fallbackToLocal,
      requestTimeoutSec: timeoutSec || 30,
    };
    setUrl(target.url);
    setBearerEnv(target.bearerTokenEnv ?? defaultTokenEnv());
    setAuthMode(target.authMode);
    const next = await submitPatch({
      activeTargetId: id,
      targets: [...targets.filter((candidate) => candidate.id !== id), target],
      token: newServer.token.trim() || undefined,
    });
    if (!next) return;
    setShowNewServer(false);
    setNewServer(emptyNewServerForm);
  };

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
            Server Target
          </h3>
          <p className="text-sm text-muted-foreground">
            Select the SpeechKit server used when a mode runs on a server.
          </p>
        </div>
        {tokenStatusBadge}
      </header>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_auto_auto] sm:items-end">
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Active server</span>
          <select
            aria-label="Active server"
            value={activeTargetId}
            disabled={busy || targets.length === 0}
            onChange={(event) => selectTarget(event.target.value)}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          >
            {targets.length === 0 ? (
              <option value="">No registered server</option>
            ) : (
              targets.map((target) => (
                <option key={target.id} value={target.id}>
                  {target.label}
                </option>
              ))
            )}
          </select>
        </label>
        <button
          type="button"
          disabled={busy}
          onClick={() => setShowNewServer((open) => !open)}
          className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium text-foreground hover:bg-muted disabled:opacity-60"
        >
          <Plus className="size-4" aria-hidden="true" />
          New Server
        </button>
        <button
          type="button"
          disabled={busy || smokeBusy || !url.trim()}
          onClick={testActiveTarget}
          className="h-10 rounded-md border border-border bg-muted/40 px-3 text-sm font-medium text-foreground hover:bg-muted disabled:opacity-60"
        >
          Test active
        </button>
      </div>

      {activeTarget ? (
        <div className="rounded-md border border-border/60 bg-muted/35 px-3 py-2 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">
            {activeTarget.label}
          </span>{" "}
          <span className="break-all">{activeTarget.url}</span>
        </div>
      ) : null}

      {showNewServer ? (
        <div className="grid grid-cols-1 gap-3 rounded-md border border-border/60 bg-muted/30 p-3 sm:grid-cols-2">
          <label className="flex flex-col gap-1.5 text-sm">
            <span className="font-medium">Server name</span>
            <input
              type="text"
              value={newServer.label}
              onChange={(event) =>
                setNewServer((current) => ({
                  ...current,
                  label: event.target.value,
                }))
              }
              placeholder="Local SpeechKit"
              className="rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-sm">
            <span className="font-medium">Server URL</span>
            <input
              type="url"
              value={newServer.url}
              onChange={(event) =>
                setNewServer((current) => ({
                  ...current,
                  url: event.target.value,
                }))
              }
              placeholder="http://127.0.0.1:8080"
              className="rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-sm">
            <span className="font-medium">Token Env Var</span>
            <input
              type="text"
              value={newServer.bearerTokenEnv}
              onChange={(event) =>
                setNewServer((current) => ({
                  ...current,
                  bearerTokenEnv: event.target.value,
                }))
              }
              placeholder="SPEECHKIT_SERVER_TOKEN"
              className="rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-sm">
            <span className="font-medium">Token value</span>
            <input
              type="password"
              value={newServer.token}
              onChange={(event) =>
                setNewServer((current) => ({
                  ...current,
                  token: event.target.value,
                }))
              }
              placeholder="Optional; stored under the env/secret name"
              className="rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-sm">
            <span className="font-medium">Auth mode</span>
            <select
              value={newServer.authMode}
              onChange={(event) => {
                const next =
                  event.target.value === "api_key" ? "api_key" : "bearer";
                setNewServer((current) => ({
                  ...current,
                  authMode: next,
                  bearerTokenEnv:
                    current.bearerTokenEnv || defaultTokenEnv(),
                }));
              }}
              className="rounded-md border border-border bg-background px-3 py-2 text-sm"
            >
              <option value="bearer">Bearer</option>
              <option value="api_key">API key header</option>
            </select>
          </label>
          <div className="flex flex-wrap items-end gap-2">
            <button
              type="button"
              disabled={smokeBusy || !newServer.url.trim()}
              onClick={() =>
                runSmokeTest({
                  url: newServer.url.trim(),
                  bearerTokenEnv: newServer.bearerTokenEnv.trim(),
                  authMode: newServer.authMode,
                  token: newServer.token.trim() || undefined,
                  requestTimeoutSec: timeoutSec,
                })
              }
              className="h-10 rounded-md border border-border bg-background px-3 text-sm font-medium hover:bg-muted disabled:opacity-60"
            >
              Test server
            </button>
            <button
              type="button"
              disabled={busy || !newServer.url.trim()}
              onClick={registerNewServer}
              className="h-10 rounded-md bg-foreground px-3 text-sm font-medium text-background disabled:opacity-60"
            >
              Register and activate
            </button>
          </div>
        </div>
      ) : null}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Endpoint URL</span>
          <input
            type="url"
            placeholder="https://speechkit.example.com"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onBlur={() => {
              if (url.trim() !== (setting.url ?? "")) {
                void submitPatch({ url: url.trim() });
              }
            }}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Credential env var</span>
          <input
            type="text"
            placeholder="SPEECHKIT_SERVER_TOKEN"
            value={bearerEnv}
            onChange={(e) => setBearerEnv(e.target.value)}
            onBlur={() => {
              const trimmed = bearerEnv.trim();
              if (trimmed !== (setting.bearerTokenEnv ?? "")) {
                void submitPatch({ bearerTokenEnv: trimmed });
              }
            }}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          />
          <span className="text-xs text-muted-foreground">
            The secret value is resolved from local secret storage, env, or
            Doppler.
          </span>
        </label>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Auth mode</span>
          <select
            value={authMode}
            disabled={busy}
            title="API key sends X-Api-Key. Bearer sends Authorization."
            onChange={(e) => {
              const next = e.target.value === "api_key" ? "api_key" : "bearer";
              setAuthMode(next);
              void submitPatch({ authMode: next });
            }}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          >
            <option value="bearer">Bearer</option>
            <option value="api_key">API key header</option>
          </select>
        </label>
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
            aria-checked={fallbackToLocal}
            disabled={busy}
            onClick={() => {
              const next = !fallbackToLocal;
              setFallbackToLocal(next);
              void submitPatch({ fallbackToLocal: next });
            }}
            className={cn(
              "inline-flex h-6 w-11 items-center rounded-full transition-colors",
              fallbackToLocal ? "bg-foreground" : "bg-muted-foreground/30",
            )}
          >
            <span
              className={cn(
                "h-5 w-5 transform rounded-full bg-background transition-transform",
                fallbackToLocal ? "translate-x-5" : "translate-x-0.5",
              )}
            />
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium">Request timeout (s)</span>
          <input
            type="number"
            min={0}
            max={3600}
            value={timeoutSec}
            onChange={(e) =>
              setTimeoutSec(Number.parseInt(e.target.value, 10) || 0)
            }
            onBlur={() => {
              if (timeoutSec !== setting.requestTimeoutSec) {
                void submitPatch({ requestTimeoutSec: timeoutSec });
              }
            }}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm"
          />
          <span className="text-xs text-muted-foreground">
            Caps non-streaming HTTP calls. WebSocket sessions are not affected.
          </span>
        </label>
      </div>

      {smokeResult ? (
        <p
          className={cn(
            "rounded-md px-3 py-2 text-sm",
            smokeResult.ok
              ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
              : "bg-destructive/10 text-destructive",
          )}
        >
          {smokeResult.message}
        </p>
      ) : null}

      {error ? <p className="text-sm text-destructive">{error}</p> : null}
    </section>
  );
}

function serverTargetsForSetting(
  setting: ServerConnectionSetting | null,
): ServerConnectionTarget[] {
  if (!setting) return [];
  if (setting.targets && setting.targets.length > 0) {
    return setting.targets;
  }
  if (!setting.url) return [];
  const id = setting.activeTargetId || serverTargetID("Configured server", setting.url, []);
  return [
    {
      id,
      label: labelFromServerURL(setting.url) || "Configured server",
      url: setting.url,
      authMode: setting.authMode ?? "bearer",
      bearerTokenEnv: setting.bearerTokenEnv ?? "SPEECHKIT_SERVER_TOKEN",
      bearerTokenSet: setting.bearerTokenSet,
      fallbackToLocal: setting.fallbackToLocal,
      requestTimeoutSec: setting.requestTimeoutSec || 30,
    },
  ];
}

function activeTargetIDForSetting(
  setting: ServerConnectionSetting | null,
  targets: ServerConnectionTarget[],
) {
  if (!setting || targets.length === 0) return "";
  if (setting.activeTargetId) {
    return setting.activeTargetId;
  }
  return (
    targets.find(
      (target) =>
        trimTrailingSlash(target.url) === trimTrailingSlash(setting.url) &&
        target.bearerTokenEnv === setting.bearerTokenEnv &&
        target.authMode === (setting.authMode ?? "bearer"),
    )?.id ?? targets[0].id
  );
}

function serverTargetID(
  label: string,
  url: string,
  existing: ServerConnectionTarget[],
) {
  const base =
    slug(label) || slug(labelFromServerURL(url) ?? "") || "server-target";
  let candidate = base;
  let index = 2;
  const existingIDs = new Set(existing.map((target) => target.id));
  while (existingIDs.has(candidate)) {
    candidate = `${base}-${index}`;
    index += 1;
  }
  return candidate;
}

function slug(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function labelFromServerURL(raw: string) {
  try {
    return new URL(raw).host;
  } catch {
    return "";
  }
}

function defaultTokenEnv() {
  return "SPEECHKIT_SERVER_TOKEN";
}

function trimTrailingSlash(value: string) {
  return value.trim().replace(/\/+$/, "");
}
