import type { ServerConnectionSetting } from "./types";

/**
 * Fetches the [server_connection] device-target settings. The bearer
 * token value never crosses this boundary — only the env var name
 * + a "is the env var set" boolean so the UI can show a hint.
 */
export async function fetchAPIV1ServerConnection(): Promise<ServerConnectionSetting> {
  const resp = await fetch("/api/v1/server-connection", { cache: "no-store" });
  if (!resp.ok) {
    const errorText = await resp.text();
    throw new Error(
      errorText || `server-connection fetch failed: ${resp.status}`,
    );
  }
  return (await resp.json()) as ServerConnectionSetting;
}

export async function patchAPIV1ServerConnection(
  patch: Partial<{
    enabled: boolean;
    url: string;
    bearerTokenEnv: string;
    fallbackToLocal: boolean;
    requestTimeoutSec: number;
  }>,
): Promise<ServerConnectionSetting> {
  const resp = await fetch("/api/v1/server-connection", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!resp.ok) {
    const errorText = await resp.text();
    throw new Error(
      errorText || `server-connection patch failed: ${resp.status}`,
    );
  }
  return (await resp.json()) as ServerConnectionSetting;
}
