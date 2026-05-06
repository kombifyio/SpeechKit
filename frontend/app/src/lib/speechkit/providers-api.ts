import type {
  APIV1ProfilesResponse,
  APIV1ProviderArtifactsResponse,
  DownloadJob,
  ModelProfile,
  ProviderReadiness,
  RuntimeMode,
} from "./types";

export async function fetchModelProfiles(): Promise<ModelProfile[]> {
  const response = await fetch("/models/profiles", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`model profiles request failed: ${response.status}`);
  }

  const payload = (await response.json()) as
    | ModelProfile[]
    | { profiles?: ModelProfile[] };
  return Array.isArray(payload) ? payload : (payload.profiles ?? []);
}

export async function fetchAPIV1Profiles(): Promise<APIV1ProfilesResponse> {
  const response = await fetch("/api/v1/providers/profiles", {
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`api v1 profiles request failed: ${response.status}`);
  }
  return (await response.json()) as APIV1ProfilesResponse;
}

export async function fetchAPIV1Readiness(): Promise<ProviderReadiness[]> {
  const response = await fetch("/api/v1/providers/readiness", {
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`api v1 readiness request failed: ${response.status}`);
  }
  return (await response.json()) as ProviderReadiness[];
}

export async function fetchAPIV1ProviderArtifacts(): Promise<APIV1ProviderArtifactsResponse> {
  const response = await fetch("/api/v1/providers/artifacts", {
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(
      `api v1 provider artifacts request failed: ${response.status}`,
    );
  }
  return (await response.json()) as APIV1ProviderArtifactsResponse;
}

export async function fetchAPIV1ProviderArtifactJobs(): Promise<DownloadJob[]> {
  const response = await fetch("/api/v1/providers/artifacts/jobs", {
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(
      `api v1 provider artifact jobs request failed: ${response.status}`,
    );
  }
  return (await response.json()) as DownloadJob[];
}

export async function startAPIV1ProviderArtifactDownload(
  artifactId: string,
): Promise<DownloadJob> {
  const response = await fetch(
    `/api/v1/providers/artifacts/${artifactId}/download`,
    { method: "POST" },
  );
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(
      errorText ||
        `api v1 provider artifact download failed: ${response.status}`,
    );
  }
  return (await response.json()) as DownloadJob;
}

export async function selectAPIV1ProviderArtifact(
  artifactId: string,
): Promise<{ message?: string; artifactId: string; profileId: string }> {
  const response = await fetch(
    `/api/v1/providers/artifacts/${artifactId}/select`,
    { method: "POST" },
  );
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(
      errorText || `api v1 provider artifact select failed: ${response.status}`,
    );
  }
  return (await response.json()) as {
    message?: string;
    artifactId: string;
    profileId: string;
  };
}

export async function activateAPIV1ProviderProfile(
  profileId: string,
): Promise<{ profileId: string; mode: RuntimeMode; model?: string }> {
  const response = await fetch(`/api/v1/providers/${profileId}/activate`, {
    method: "POST",
  });
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(
      errorText || `api v1 profile activation failed: ${response.status}`,
    );
  }
  return (await response.json()) as {
    profileId: string;
    mode: RuntimeMode;
    model?: string;
  };
}

export async function saveProviderCredential(provider: string, secret: string) {
  const body = new URLSearchParams({ provider, credential: secret });
  const response = await fetch("/settings/provider-credentials/save", {
    method: "POST",
    body,
  });
  if (!response.ok)
    throw new Error(`provider credential save failed: ${response.status}`);
  return (await response.json()) as { message?: string };
}

export async function updateProviderIntegration(
  provider: string,
  enabled: boolean,
) {
  const body = new URLSearchParams({
    provider,
    enabled: enabled ? "1" : "0",
  });
  const response = await fetch("/settings/provider-integrations/update", {
    method: "POST",
    body,
  });
  if (!response.ok)
    throw new Error(`provider integration update failed: ${response.status}`);
  return (await response.json()) as { message?: string };
}

export async function clearProviderCredential(provider: string) {
  const body = new URLSearchParams({ provider });
  const response = await fetch("/settings/provider-credentials/clear", {
    method: "POST",
    body,
  });
  if (!response.ok)
    throw new Error(`provider credential clear failed: ${response.status}`);
  return (await response.json()) as { message?: string };
}

export async function testProviderCredential(provider: string, secret: string) {
  const body = new URLSearchParams({ provider, credential: secret });
  const response = await fetch("/settings/provider-credentials/test", {
    method: "POST",
    body,
  });
  if (!response.ok)
    throw new Error(`provider credential test failed: ${response.status}`);
  return (await response.json()) as { message?: string };
}
