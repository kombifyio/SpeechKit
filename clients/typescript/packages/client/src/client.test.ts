import { describe, expect, it, vi } from "vitest";

import { SpeechKitClient } from "./client.js";
import { HTTPError } from "./types.js";

interface RecordedRequest {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: unknown;
}

function mockFetch(status = 200, responseBody: unknown = {}) {
  const requests: RecordedRequest[] = [];
  const impl = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push({
      url: String(input),
      method: init?.method ?? "GET",
      headers: (init?.headers ?? {}) as Record<string, string>,
      body: init?.body,
    });
    return new Response(JSON.stringify(responseBody), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  });
  return { impl: impl as unknown as typeof fetch, requests };
}

describe("SpeechKitClient URL resolution", () => {
  it("defaults basePath to /v1 for direct server access", async () => {
    const { impl, requests } = mockFetch(200, {});
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", fetch: impl });
    await client.assistProcess({ text: "hi" });
    expect(requests[0]?.url).toBe("http://localhost:8080/v1/assist/process");
  });

  it("routes versioned endpoints through a custom gateway basePath", async () => {
    const { impl, requests } = mockFetch(200, {});
    const client = new SpeechKitClient({
      baseUrl: "https://api.kombify.io",
      basePath: "/v1/speechkit",
      fetch: impl,
    });
    await client.assistProcess({ text: "hi" });
    await client.voiceAgentCreateSession();
    expect(requests[0]?.url).toBe("https://api.kombify.io/v1/speechkit/assist/process");
    expect(requests[1]?.url).toBe("https://api.kombify.io/v1/speechkit/voiceagent/sessions");
  });

  it("normalizes basePath (missing leading slash, trailing slash)", async () => {
    const { impl, requests } = mockFetch(200, {});
    const client = new SpeechKitClient({
      baseUrl: "https://api.kombify.io",
      basePath: "v1/speechkit/",
      fetch: impl,
    });
    await client.config();
    expect(requests[0]?.url).toBe("https://api.kombify.io/v1/speechkit/config");
  });

  it("keeps status() on the server root regardless of basePath", async () => {
    const { impl, requests } = mockFetch(200, { status: "ok" });
    const client = new SpeechKitClient({
      baseUrl: "https://api.kombify.io",
      basePath: "/v1/speechkit",
      fetch: impl,
    });
    await client.status();
    expect(requests[0]?.url).toBe("https://api.kombify.io/readyz");
  });

  it("strips trailing slashes from baseUrl", async () => {
    const { impl, requests } = mockFetch(200, {});
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080///", fetch: impl });
    await client.config();
    expect(requests[0]?.url).toBe("http://localhost:8080/v1/config");
  });

  it("rejects a baseUrl without a scheme", () => {
    expect(() => new SpeechKitClient({ baseUrl: "localhost:8080" })).toThrow(/http/);
  });

  it("passes absolute URLs through resolve() untouched", () => {
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", fetch: mockFetch().impl });
    expect(client.resolve("https://elsewhere.example/x")).toBe("https://elsewhere.example/x");
    expect(client.resolve("/v1/config")).toBe("http://localhost:8080/v1/config");
    expect(client.resolve("v1/config")).toBe("http://localhost:8080/v1/config");
  });
});

describe("SpeechKitClient requests", () => {
  it("sends a bearer token when configured", async () => {
    const { impl, requests } = mockFetch(200, {});
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", token: "tok-1", fetch: impl });
    await client.config();
    expect(requests[0]?.headers["Authorization"]).toBe("Bearer tok-1");
  });

  it("omits the auth header for an empty token", async () => {
    const { impl, requests } = mockFetch(200, {});
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", token: "  ", fetch: impl });
    await client.config();
    expect(requests[0]?.headers["Authorization"]).toBeUndefined();
  });

  it("uploads dictation audio as multipart form data", async () => {
    const { impl, requests } = mockFetch(200, { text: "hello world" });
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", fetch: impl });
    const result = await client.transcribe(new Blob(["audio-bytes"]), {
      filename: "clip.wav",
      language: "en",
    });
    expect(result.text).toBe("hello world");
    expect(requests[0]?.method).toBe("POST");
    expect(requests[0]?.url).toBe("http://localhost:8080/v1/dictation/transcribe");
    const form = requests[0]?.body as FormData;
    expect(form).toBeInstanceOf(FormData);
    expect((form.get("audio") as File).name).toBe("clip.wav");
    expect(form.get("language")).toBe("en");
    expect(form.get("model")).toBeNull();
  });

  it("returns the voice agent session ticket payload", async () => {
    const ticket = {
      session_id: "sess-1",
      ticket: "tkt-abc",
      ws_url: "wss://origin.example/v1/voiceagent/sessions/sess-1/ws",
      ws_subprotocol: "ticket.tkt-abc",
      expires_at: "2026-07-15T00:00:00Z",
    };
    const { impl, requests } = mockFetch(200, ticket);
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", fetch: impl });
    const created = await client.voiceAgentCreateSession({ persona_id: "helper" });
    expect(created).toEqual(ticket);
    expect(requests[0]?.method).toBe("POST");
    expect(requests[0]?.body).toBe(JSON.stringify({ persona_id: "helper" }));
  });

  it("unwraps list envelopes and defaults to empty arrays", async () => {
    const { impl } = mockFetch(200, {});
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", fetch: impl });
    expect(await client.catalogProfiles()).toEqual([]);
    expect(await client.ttsVoices()).toEqual([]);
    expect(await client.transcripts()).toEqual([]);
  });

  it("encodes query parameters", async () => {
    const { impl, requests } = mockFetch(200, { profiles: [] });
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", fetch: impl });
    await client.catalogProfiles("voice agent");
    expect(requests[0]?.url).toBe("http://localhost:8080/v1/catalog/profiles?mode=voice%20agent");
  });

  it("throws HTTPError with status and body on non-2xx", async () => {
    const { impl } = mockFetch(403, { error: { code: "speechkit_feature_not_entitled" } });
    const client = new SpeechKitClient({ baseUrl: "http://localhost:8080", fetch: impl });
    const err = await client.config().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).status).toBe(403);
    expect((err as HTTPError).body).toContain("speechkit_feature_not_entitled");
  });

  it("builds from environment variables", () => {
    const client = SpeechKitClient.fromEnv({
      SPEECHKIT_SERVER_URL: "https://speechkit.example",
      SPEECHKIT_SERVER_TOKEN: "envtok",
    });
    expect(client.resolve("/readyz")).toBe("https://speechkit.example/readyz");
  });
});
