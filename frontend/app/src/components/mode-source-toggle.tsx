/**
 * Per-mode "run locally vs route to a SpeechKit server" toggle.
 */

import { useId } from "react";

import { serverConnectionReadiness } from "@/components/server-connection-card";
import type { ModeSource, ServerConnectionSetting } from "@/lib/speechkit";

import { cn } from "@/lib/utils";

export type ModeSourceToggleProps = {
  /** Mode for the disabled-state hint text only. */
  modeLabel: string;
  /** Current value; falls back to "local" when undefined. */
  value?: ModeSource;
  /**
   * Called with the new mode source. Caller is responsible for
   * persisting via patchAPIV1ModeSettings.
   */
  onChange: (next: ModeSource) => void;
  /** When non-null, drives the disabled-state copy + behaviour. */
  serverConnection?: ServerConnectionSetting | null;
  /** Extra classes for layout adjustments. */
  className?: string;
};

export function ModeSourceToggle({
  modeLabel,
  value,
  onChange,
  serverConnection,
  className,
}: ModeSourceToggleProps) {
  const headingId = useId();
  const resolved: ModeSource = value === "server" ? "server" : "local";

  const readiness = serverConnectionReadiness(serverConnection);
  const serverReady = readiness === "ready";
  const hint = !serverReady
    ? readiness === "not-configured"
      ? "Register a server target under SpeechKit Server > Server Target first."
      : `Set ${
          serverConnection?.bearerTokenEnv ?? "the server token env var"
        } in the host environment so the device can authenticate.`
    : null;

  const handleServerClick = () => {
    if (!serverReady) return;
    onChange("server");
  };

  return (
    <div
      className={cn(
        "flex flex-col gap-1.5 rounded-lg border border-border/60 bg-muted/40 p-3",
        className,
      )}
      aria-labelledby={headingId}
    >
      <div className="flex items-center justify-between gap-3">
        <div>
          <h4
            id={headingId}
            className="text-sm font-medium leading-tight text-foreground"
          >
            {modeLabel} runs on
          </h4>
          <p className="text-xs text-muted-foreground">
            Per-mode override; applies immediately.
          </p>
        </div>
        <div
          role="radiogroup"
          aria-label={`${modeLabel} mode source`}
          className="inline-flex items-center rounded-md border border-border bg-background p-0.5"
        >
          <button
            type="button"
            role="radio"
            aria-checked={resolved === "local"}
            onClick={() => onChange("local")}
            className={cn(
              "rounded-sm px-3 py-1 text-xs font-medium transition-colors",
              resolved === "local"
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            Local
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={resolved === "server"}
            aria-disabled={!serverReady}
            disabled={!serverReady && resolved !== "server"}
            onClick={handleServerClick}
            className={cn(
              "rounded-sm px-3 py-1 text-xs font-medium transition-colors",
              resolved === "server"
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground",
              !serverReady && resolved !== "server" &&
                "cursor-not-allowed opacity-50",
            )}
            title={hint ?? undefined}
          >
            Server
          </button>
        </div>
      </div>
      {hint ? (
        <p className="text-xs text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  );
}
