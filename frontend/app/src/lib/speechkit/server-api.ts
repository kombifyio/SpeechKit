import type {
  ServerConnectionSetting,
  ServerConnectionSmokeRequest,
  ServerConnectionSmokeResponse,
  ServerConnectionTarget,
} from "./types";

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
    activeTargetId: string;
    url: string;
    bearerTokenEnv: string;
    authMode: "bearer" | "api_key";
    fallbackToLocal: boolean;
    requestTimeoutSec: number;
    targets: ServerConnectionTarget[];
    token: string;
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

export async function testAPIV1ServerConnection(
  request: ServerConnectionSmokeRequest,
): Promise<ServerConnectionSmokeResponse> {
  const resp = await fetch("/api/v1/server-connection/smoke", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  if (!resp.ok) {
    const errorText = await resp.text();
    throw new Error(errorText || `server smoke test failed: ${resp.status}`);
  }
  return (await resp.json()) as ServerConnectionSmokeResponse;
}
