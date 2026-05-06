import { useEffect, useState } from "react";

import { ModeSourceSection } from "@/components/mode-source-section";
import { ServerConnectionCard } from "@/components/server-connection-card";
import {
  fetchAPIV1ServerConnection,
  type ServerConnectionSetting,
} from "@/lib/speechkit";

export function SpeechKitServerSettingsPage() {
  const [serverConnection, setServerConnection] =
    useState<ServerConnectionSetting | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    fetchAPIV1ServerConnection()
      .then((data) => {
        if (cancelled) return;
        setServerConnection(data);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(String((err as Error)?.message ?? err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (error && !serverConnection) {
    return (
      <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        Failed to load SpeechKit server settings: {error}
      </div>
    );
  }

  if (!serverConnection) {
    return (
      <div className="rounded-lg border border-border/60 p-4">
        <p className="text-sm text-muted-foreground">
          Loading SpeechKit server settings…
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-5">
      <ServerConnectionCard
        serverConnection={serverConnection}
        onSettingsChange={setServerConnection}
      />
      <ModeSourceSection
        serverConnection={serverConnection}
        onServerConnectionChange={setServerConnection}
      />
    </div>
  );
}
