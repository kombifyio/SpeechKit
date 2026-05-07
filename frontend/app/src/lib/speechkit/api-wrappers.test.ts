import { afterEach, describe, expect, it, vi } from "vitest";

import {
  armQuickNoteRecording,
  createQuickNote,
  dashboardAudioDownloadURL,
  deleteQuickNote,
  fetchDashboardStats,
  fetchHistory,
  fetchLogs,
  fetchQuickNotes,
  pinQuickNote,
  quickNoteEmail,
  quickNoteSummary,
  revealDashboardAudio,
  updateQuickNote,
} from "./dashboard-api";
import {
  cancelModelDownload,
  fetchDownloadCatalog,
  fetchDownloadJobs,
  selectDownloadedModel,
  startModelDownload,
} from "./downloads-api";
import {
  fetchAPIV1Dictionary,
  fetchAPIV1Modes,
  fetchAudioDevices,
  fetchAudioOutputDevices,
  importAPIV1Dictionary,
  patchAPIV1ModeSettings,
  setActiveMode,
  setAudioDevice,
  setAudioOutputDevice,
  setModeEnabled,
} from "./modes-api";
import {
  activateAPIV1ProviderProfile,
  clearProviderCredential,
  fetchAPIV1Profiles,
  fetchAPIV1ProviderArtifactJobs,
  fetchAPIV1ProviderArtifacts,
  fetchAPIV1Readiness,
  fetchModelProfiles,
  saveProviderCredential,
  selectAPIV1ProviderArtifact,
  startAPIV1ProviderArtifactDownload,
  testProviderCredential,
  updateProviderIntegration,
} from "./providers-api";
import {
  fetchAPIV1ServerConnection,
  patchAPIV1ServerConnection,
} from "./server-api";
import type { AudioDevice } from "./types";
import {
  cancelAppUpdateDownload,
  fetchAppUpdateJobs,
  fetchAppVersion,
  openAppUpdateInstaller,
  startAppUpdateDownload,
} from "../api/app-update";

const originalFetch = globalThis.fetch;

type FetchCall = { input: RequestInfo | URL; init?: RequestInit };

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

function installFetch(
  handler: (input: string, init?: RequestInit) => Response,
): FetchCall[] {
  const calls: FetchCall[] = [];
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ input, init });
    return handler(String(input), init);
  }) as unknown as typeof fetch;
  return calls;
}

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function textResponse(message: string, status: number): Response {
  return new Response(status === 204 ? null : message, { status });
}

function formBody(init?: RequestInit): Record<string, string> {
  expect(init?.body).toBeInstanceOf(URLSearchParams);
  return Object.fromEntries((init?.body as URLSearchParams).entries());
}

describe("download API wrappers", () => {
  it("fetches catalog/jobs and sends form encoded mutations", async () => {
    const calls = installFetch((input) => {
      if (input === "/models/downloads/catalog") return jsonResponse([{ id: "tiny" }]);
      if (input === "/models/downloads/jobs") return jsonResponse([{ id: "job-1" }]);
      if (input === "/models/downloads/start") return jsonResponse({ id: "job-2" });
      if (input === "/models/downloads/select") return jsonResponse({ message: "selected" });
      if (input === "/models/downloads/cancel") return textResponse("", 204);
      return textResponse("unexpected", 404);
    });

    await expect(fetchDownloadCatalog()).resolves.toEqual([{ id: "tiny" }]);
    await expect(fetchDownloadJobs()).resolves.toEqual([{ id: "job-1" }]);
    await expect(startModelDownload("tiny")).resolves.toEqual({ id: "job-2" });
    await expect(selectDownloadedModel("tiny")).resolves.toEqual({ message: "selected" });
    await expect(cancelModelDownload("job-2")).resolves.toBeUndefined();

    expect(calls[2]).toMatchObject({ input: "/models/downloads/start" });
    expect(calls[2].init?.method).toBe("POST");
    expect(formBody(calls[2].init)).toEqual({ model_id: "tiny" });
    expect(formBody(calls[4].init)).toEqual({ job_id: "job-2" });
  });

  it("surfaces response text for failed download mutations", async () => {
    installFetch(() => textResponse("model unavailable", 409));

    await expect(startModelDownload("missing")).rejects.toThrow("model unavailable");
    await expect(selectDownloadedModel("missing")).rejects.toThrow("model unavailable");
  });
});

describe("provider API wrappers", () => {
  it("normalizes provider profile reads and posts provider actions", async () => {
    const calls = installFetch((input) => {
      if (input === "/models/profiles") return jsonResponse({ profiles: [{ id: "p1" }] });
      if (input === "/api/v1/providers/profiles") return jsonResponse({ profiles: [] });
      if (input === "/api/v1/providers/readiness") return jsonResponse([{ provider: "openai" }]);
      if (input === "/api/v1/providers/artifacts") return jsonResponse({ artifacts: [] });
      if (input === "/api/v1/providers/artifacts/jobs") return jsonResponse([{ id: "job" }]);
      if (input === "/api/v1/providers/artifacts/artifact-1/download") {
        return jsonResponse({ id: "download" });
      }
      if (input === "/api/v1/providers/artifacts/artifact-1/select") {
        return jsonResponse({ artifactId: "artifact-1", profileId: "profile-1" });
      }
      if (input === "/api/v1/providers/profile-1/activate") {
        return jsonResponse({ profileId: "profile-1", mode: "assist" });
      }
      if (input.startsWith("/settings/provider-")) return jsonResponse({ message: "ok" });
      return textResponse("unexpected", 404);
    });

    await expect(fetchModelProfiles()).resolves.toEqual([{ id: "p1" }]);
    await expect(fetchAPIV1Profiles()).resolves.toEqual({ profiles: [] });
    await expect(fetchAPIV1Readiness()).resolves.toEqual([{ provider: "openai" }]);
    await expect(fetchAPIV1ProviderArtifacts()).resolves.toEqual({ artifacts: [] });
    await expect(fetchAPIV1ProviderArtifactJobs()).resolves.toEqual([{ id: "job" }]);
    await expect(startAPIV1ProviderArtifactDownload("artifact-1")).resolves.toEqual({ id: "download" });
    await expect(selectAPIV1ProviderArtifact("artifact-1")).resolves.toEqual({
      artifactId: "artifact-1",
      profileId: "profile-1",
    });
    await expect(activateAPIV1ProviderProfile("profile-1")).resolves.toEqual({
      profileId: "profile-1",
      mode: "assist",
    });
    await saveProviderCredential("openai", "secret");
    await updateProviderIntegration("groq", true);
    await clearProviderCredential("hf");
    await testProviderCredential("google", "secret");

    expect(calls[5].init?.method).toBe("POST");
    expect(calls[8].input).toBe("/settings/provider-credentials/save");
    expect(formBody(calls[8].init)).toEqual({ provider: "openai", credential: "secret" });
    expect(formBody(calls[9].init)).toEqual({ provider: "groq", enabled: "1" });
  });

  it("uses response text when provider artifact actions fail", async () => {
    installFetch(() => textResponse("artifact blocked", 423));

    await expect(startAPIV1ProviderArtifactDownload("artifact-1")).rejects.toThrow("artifact blocked");
    await expect(selectAPIV1ProviderArtifact("artifact-1")).rejects.toThrow("artifact blocked");
    await expect(activateAPIV1ProviderProfile("profile-1")).rejects.toThrow("artifact blocked");
  });
});

describe("mode and server API wrappers", () => {
  it("normalizes audio devices and posts mode changes", async () => {
    const defaultDevice: AudioDevice = { deviceId: "default", label: "Default", isDefault: true };
    const calls = installFetch((input) => {
      if (input === "/audio/devices") return jsonResponse([defaultDevice]);
      if (input === "/audio/output/devices") return jsonResponse({ devices: [defaultDevice], selectedOutputDeviceId: "speaker" });
      if (input === "/audio/device" || input === "/audio/output/device") return jsonResponse({ message: "saved" });
      if (input === "/mode/active" || input === "/mode/enabled") return jsonResponse({ message: "saved" });
      if (input === "/api/v1/modes") return jsonResponse({ modes: [] });
      if (input === "/api/v1/dictionary?language=en-US") return jsonResponse({ language: "en-US", entries: [] });
      if (input === "/api/v1/dictionary") {
        return jsonResponse({
          language: "en-US",
          entries: [{ spoken: "speech kit", canonical: "SpeechKit", enabled: true, usageCount: 0 }],
        });
      }
      if (input === "/api/v1/modes/assist/settings") return jsonResponse({ primaryProfileId: "assist.openai" });
      return textResponse("unexpected", 404);
    });

    await expect(fetchAudioDevices()).resolves.toEqual({
      devices: [defaultDevice],
      selectedDeviceId: "default",
    });
    await expect(fetchAudioOutputDevices()).resolves.toEqual({
      devices: [defaultDevice],
      selectedDeviceId: "speaker",
    });
    await expect(setAudioDevice("mic-2")).resolves.toBe("saved");
    await expect(setAudioOutputDevice("speaker-2")).resolves.toBe("saved");
    await expect(setActiveMode("assist")).resolves.toBe("saved");
    await expect(setModeEnabled("assist", false)).resolves.toBe("saved");
    await expect(fetchAPIV1Modes()).resolves.toEqual({ modes: [] });
    await expect(fetchAPIV1Dictionary("en-US")).resolves.toEqual({ language: "en-US", entries: [] });
    await expect(
      importAPIV1Dictionary("en-US", [
        { spoken: "speech kit", canonical: "SpeechKit", enabled: true, usageCount: 0 },
      ]),
    ).resolves.toEqual({
      language: "en-US",
      entries: [{ spoken: "speech kit", canonical: "SpeechKit", enabled: true, usageCount: 0 }],
    });
    await expect(patchAPIV1ModeSettings("assist", { ttsEnabled: true })).resolves.toEqual({
      primaryProfileId: "assist.openai",
    });

    expect(formBody(calls[2].init)).toMatchObject({ device_id: "mic-2" });
    expect(formBody(calls[5].init)).toEqual({ mode: "assist", enabled: "0" });
    expect(calls[8].init?.headers).toEqual({ "Content-Type": "application/json" });
    expect(calls[9].init?.method).toBe("PATCH");
  });

  it("fetches and patches server connection settings", async () => {
    const calls = installFetch((input) => {
      if (input === "/api/v1/server-connection") {
        return jsonResponse({
          enabled: true,
          url: "https://speechkit.example",
          bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
          bearerTokenSet: false,
          fallbackToLocal: true,
          requestTimeoutSec: 30,
        });
      }
      return textResponse("unexpected", 404);
    });

    await expect(fetchAPIV1ServerConnection()).resolves.toMatchObject({
      enabled: true,
      fallbackToLocal: true,
    });
    await expect(patchAPIV1ServerConnection({ enabled: false, requestTimeoutSec: 10 })).resolves.toMatchObject({
      enabled: true,
    });

    expect(calls[0].init).toEqual({ cache: "no-store" });
    expect(calls[1].init?.method).toBe("PATCH");
    expect(calls[1].init?.headers).toEqual({ "Content-Type": "application/json" });
    expect(JSON.parse(String(calls[1].init?.body))).toEqual({
      enabled: false,
      requestTimeoutSec: 10,
    });
  });
});

describe("dashboard and update API wrappers", () => {
  it("wraps dashboard reads and quicknote mutations", async () => {
    const calls = installFetch((input) => {
      if (input === "/dashboard/history") return jsonResponse([{ id: 1 }]);
      if (input === "/dashboard/stats") return jsonResponse({ total: 1 });
      if (input === "/dashboard/logs") return jsonResponse([{ message: "ok" }]);
      if (input === "/dashboard/quicknotes") return jsonResponse([{ id: 2 }]);
      if (input === "/quicknotes/create") return jsonResponse({ id: 3, message: "created" });
      if (input === "/quicknotes/update") return jsonResponse({ message: "updated" });
      if (input === "/quicknotes/pin") return jsonResponse({ message: "pinned" });
      if (input === "/quicknotes/delete") return jsonResponse({ message: "deleted" });
      if (input === "/quicknotes/summary") return jsonResponse({ summary: "summary" });
      if (input === "/quicknotes/email") return jsonResponse({ email: "email" });
      if (input === "/quicknotes/record-mode?id=4") return jsonResponse({ message: "armed" });
      if (input === "/dashboard/audio/reveal") return jsonResponse({ message: "revealed" });
      return textResponse("unexpected", 404);
    });

    await expect(fetchHistory()).resolves.toEqual([{ id: 1 }]);
    await expect(fetchDashboardStats()).resolves.toEqual({ total: 1 });
    await expect(fetchLogs()).resolves.toEqual([{ message: "ok" }]);
    await expect(fetchQuickNotes()).resolves.toEqual([{ id: 2 }]);
    await expect(createQuickNote("note")).resolves.toEqual({ id: 3, message: "created" });
    await expect(updateQuickNote(3, "updated")).resolves.toBe("updated");
    await expect(pinQuickNote(3, true)).resolves.toBe("pinned");
    await expect(deleteQuickNote(3)).resolves.toBe("deleted");
    await expect(quickNoteSummary(3)).resolves.toBe("summary");
    await expect(quickNoteEmail(3)).resolves.toBe("email");
    await expect(armQuickNoteRecording(4)).resolves.toBe("armed");
    await expect(revealDashboardAudio("quicknote", 3)).resolves.toBe("revealed");
    expect(dashboardAudioDownloadURL("transcription", 7)).toBe("/dashboard/audio?kind=transcription&id=7");

    expect(formBody(calls[4].init)).toEqual({ text: "note" });
    expect(formBody(calls[11].init)).toEqual({ kind: "quicknote", id: "3" });
  });

  it("wraps app update endpoints and propagates text errors", async () => {
    const calls = installFetch((input) => {
      if (input === "/app/version") return jsonResponse({ version: "1.0.0" });
      if (input === "/app/update/jobs") return jsonResponse([{ id: "job" }]);
      if (input === "/app/update/download") return jsonResponse({ id: "job-2" });
      if (input === "/app/update/cancel") return textResponse("", 204);
      if (input === "/app/update/open") return jsonResponse({ message: "opened" });
      return textResponse("unexpected", 404);
    });

    await expect(fetchAppVersion()).resolves.toEqual({ version: "1.0.0" });
    await expect(fetchAppUpdateJobs()).resolves.toEqual([{ id: "job" }]);
    await expect(startAppUpdateDownload("1.1.0")).resolves.toEqual({ id: "job-2" });
    await expect(cancelAppUpdateDownload("job-2")).resolves.toBeUndefined();
    await expect(openAppUpdateInstaller("job-2")).resolves.toEqual({ message: "opened" });

    expect(formBody(calls[2].init)).toEqual({ version: "1.1.0" });
    expect(formBody(calls[3].init)).toEqual({ job_id: "job-2" });

    installFetch(() => textResponse("download blocked", 409));
    await expect(startAppUpdateDownload("2.0.0")).rejects.toThrow("download blocked");
  });
});
