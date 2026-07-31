import {
  HTTPError,
  type CatalogReadiness,
  type DictionaryEntry,
  type ModeContract,
  type ProviderProfile,
  type Status,
  type TTSSynthesizeRequest,
  type TTSSynthesizeResponse,
  type Transcript,
  type TranscribeOptions,
  type TranscribeResponse,
  type VoiceAgentSessionTicket,
  type VoiceAgentSummary,
  type VoiceAgentTranscript,
  type Voice,
} from "./types.js";

export interface ClientOptions {
  /** Base URL of the SpeechKit Server. Defaults to `http://localhost:8080`. */
  baseUrl?: string;
  /**
   * Path prefix in front of every versioned API route. Defaults to `/v1`
   * (direct server access). Gateway consumers set `baseUrl` to the gateway
   * origin and `basePath` to the mounted prefix, e.g.
   * `baseUrl: "https://api.kombify.io"` + `basePath: "/v1/speechkit"`.
   */
  basePath?: string;
  /** Bearer token. Empty string is treated as "no auth header". */
  token?: string;
  /**
   * Custom fetch implementation. Defaults to globalThis.fetch (Node 20+,
   * modern browsers). Use this to inject undici, msw, or a logging wrapper.
   */
  fetch?: typeof fetch;
  /** User-Agent string sent with every request. */
  userAgent?: string;
  /**
   * Request timeout in milliseconds. Implemented with an AbortController so
   * it works in browsers and Node. Defaults to 30_000.
   */
  timeoutMs?: number;
}

export interface RequestOverrides {
  signal?: AbortSignal;
  headers?: Record<string, string>;
  /** Override timeoutMs for this request only. */
  timeoutMs?: number;
}

/**
 * SpeechKit Server REST client. Mirrors `pkg/speechkit/client.Client` in Go.
 *
 * ```ts
 * const client = new SpeechKitClient({
 *   baseUrl: process.env.SPEECHKIT_SERVER_URL,
 *   token: process.env.SPEECHKIT_TOKEN,
 * });
 * const status = await client.status();
 * ```
 */
export class SpeechKitClient {
  private readonly baseUrl: string;
  private readonly basePath: string;
  private readonly token: string;
  private readonly fetchImpl: typeof fetch;
  private readonly userAgent: string;
  private readonly timeoutMs: number;

  constructor(opts: ClientOptions = {}) {
    const raw = (opts.baseUrl ?? "http://localhost:8080").replace(/\/+$/, "");
    if (!/^https?:\/\//.test(raw)) {
      throw new Error("baseUrl must include http:// or https://");
    }
    this.baseUrl = raw;
    this.basePath = normalizeBasePath(opts.basePath);
    this.token = (opts.token ?? "").trim();
    this.fetchImpl = opts.fetch ?? globalThis.fetch.bind(globalThis);
    this.userAgent = opts.userAgent ?? "speechkit-client-ts/0.1";
    this.timeoutMs = opts.timeoutMs ?? 30_000;
  }

  /**
   * Build a SpeechKitClient from `SPEECHKIT_SERVER_URL` and
   * `SPEECHKIT_TOKEN` (or `SPEECHKIT_SERVER_TOKEN`). Node-only — in a
   * browser, construct with explicit options instead.
   */
  static fromEnv(
    env: Record<string, string | undefined> = typeof process !== "undefined" ? process.env : {},
  ): SpeechKitClient {
    const opts: ClientOptions = {};
    const baseUrl = env["SPEECHKIT_SERVER_URL"];
    if (baseUrl) opts.baseUrl = baseUrl;
    const token = env["SPEECHKIT_TOKEN"] ?? env["SPEECHKIT_SERVER_TOKEN"];
    if (token) opts.token = token;
    return new SpeechKitClient(opts);
  }

  // ── Health & config ──────────────────────────────────────────────────

  /**
   * GET /readyz — readiness probe. Always resolved against the server
   * root, not `basePath`: readiness is intentionally not exposed through
   * the kombify Gateway (only `{basePath}/healthz` is public there).
   */
  status(overrides?: RequestOverrides): Promise<Status> {
    return this.requestJSON<Status>("GET", "/readyz", undefined, overrides);
  }

  /** GET /v1/config — server config summary. */
  config(overrides?: RequestOverrides): Promise<Record<string, unknown>> {
    return this.requestJSON<Record<string, unknown>>("GET", this.v1("/config"), undefined, overrides);
  }

  // ── Catalog ──────────────────────────────────────────────────────────

  /** GET /v1/catalog/profiles?mode=... */
  async catalogProfiles(mode?: string, overrides?: RequestOverrides): Promise<ProviderProfile[]> {
    const path = mode
      ? this.v1(`/catalog/profiles?mode=${encodeURIComponent(mode)}`)
      : this.v1("/catalog/profiles");
    const body = await this.requestJSON<{ profiles?: ProviderProfile[] }>("GET", path, undefined, overrides);
    return body.profiles ?? [];
  }

  /** GET /v1/catalog/contracts */
  async catalogContracts(overrides?: RequestOverrides): Promise<ModeContract[]> {
    const body = await this.requestJSON<{ contracts?: ModeContract[] }>(
      "GET",
      this.v1("/catalog/contracts"),
      undefined,
      overrides,
    );
    return body.contracts ?? [];
  }

  /** GET /v1/catalog/readiness */
  async catalogReadiness(overrides?: RequestOverrides): Promise<CatalogReadiness[]> {
    const body = await this.requestJSON<{ readiness?: CatalogReadiness[] }>(
      "GET",
      this.v1("/catalog/readiness"),
      undefined,
      overrides,
    );
    return body.readiness ?? [];
  }

  // ── Dictation ────────────────────────────────────────────────────────

  /**
   * POST /v1/dictation/transcribe — multipart upload of an audio Blob.
   * Works in browsers (MediaRecorder) and Node (fs.openAsBlob, undici).
   */
  async transcribe(
    audio: Blob,
    options: TranscribeOptions & { filename?: string } = {},
    overrides?: RequestOverrides,
  ): Promise<TranscribeResponse> {
    const form = new FormData();
    form.append("audio", audio, options.filename ?? "speech.webm");
    if (options.language) form.append("language", options.language);
    if (options.model) form.append("model", options.model);
    if (options.prompt) form.append("prompt", options.prompt);
    return this.requestRaw<TranscribeResponse>("POST", this.v1("/dictation/transcribe"), form, overrides);
  }

  // ── Assist ───────────────────────────────────────────────────────────

  /** POST /v1/assist/process. */
  assistProcess(
    payload: Record<string, unknown>,
    overrides?: RequestOverrides,
  ): Promise<Record<string, unknown>> {
    return this.requestJSON<Record<string, unknown>>("POST", this.v1("/assist/process"), payload, overrides);
  }

  // ── Voice Agent ──────────────────────────────────────────────────────

  /**
   * POST /v1/voiceagent/sessions — mints a session_id + single-use ticket
   * (default TTL 90 s, no refresh). Open the returned `ws_url` with the
   * `ws_subprotocol` value (`ticket.<ticket>`) as the WebSocket
   * subprotocol; do not place the ticket in URLs.
   */
  voiceAgentCreateSession(
    payload: Record<string, unknown> = {},
    overrides?: RequestOverrides,
  ): Promise<VoiceAgentSessionTicket> {
    return this.requestJSON<VoiceAgentSessionTicket>("POST", this.v1("/voiceagent/sessions"), payload, overrides);
  }

  /** GET /v1/voiceagent/sessions/{id}/transcript. */
  voiceAgentSessionTranscript(id: number, overrides?: RequestOverrides): Promise<VoiceAgentTranscript> {
    return this.requestJSON<VoiceAgentTranscript>(
      "GET",
      this.v1(`/voiceagent/sessions/${id}/transcript`),
      undefined,
      overrides,
    );
  }

  /** GET /v1/voiceagent/sessions/{id}/summary. */
  voiceAgentSessionSummary(id: number, overrides?: RequestOverrides): Promise<VoiceAgentSummary> {
    return this.requestJSON<VoiceAgentSummary>(
      "GET",
      this.v1(`/voiceagent/sessions/${id}/summary`),
      undefined,
      overrides,
    );
  }

  // ── TTS ──────────────────────────────────────────────────────────────

  /** POST /v1/tts/synthesize. */
  ttsSynthesize(input: TTSSynthesizeRequest, overrides?: RequestOverrides): Promise<TTSSynthesizeResponse> {
    return this.requestJSON<TTSSynthesizeResponse>("POST", this.v1("/tts/synthesize"), input, overrides);
  }

  /** GET /v1/tts/voices. */
  async ttsVoices(overrides?: RequestOverrides): Promise<Voice[]> {
    const body = await this.requestJSON<{ voices?: Voice[] }>("GET", this.v1("/tts/voices"), undefined, overrides);
    return body.voices ?? [];
  }

  // ── Vocabulary ───────────────────────────────────────────────────────

  /** GET /v1/vocabulary/dictionary?language=. */
  async vocabularyEntries(language?: string, overrides?: RequestOverrides): Promise<DictionaryEntry[]> {
    const path = language
      ? this.v1(`/vocabulary/dictionary?language=${encodeURIComponent(language)}`)
      : this.v1("/vocabulary/dictionary");
    const body = await this.requestJSON<{ entries?: DictionaryEntry[] }>("GET", path, undefined, overrides);
    return body.entries ?? [];
  }

  // ── Transcripts ──────────────────────────────────────────────────────

  /** GET /v1/transcripts. */
  async transcripts(limit?: number, overrides?: RequestOverrides): Promise<Transcript[]> {
    const path = limit && limit > 0 ? this.v1(`/transcripts?limit=${limit}`) : this.v1("/transcripts");
    const body = await this.requestJSON<{ transcripts?: Transcript[] }>("GET", path, undefined, overrides);
    return body.transcripts ?? [];
  }

  /** GET /v1/transcripts/{id}. */
  transcript(id: number, overrides?: RequestOverrides): Promise<Transcript> {
    return this.requestJSON<Transcript>("GET", this.v1(`/transcripts/${id}`), undefined, overrides);
  }

  // ── Internal request helpers ─────────────────────────────────────────

  /**
   * Compose the full URL for a request. Path may begin with `/` or be
   * absolute; absolute URLs replace the baseUrl entirely.
   */
  resolve(path: string): string {
    if (/^https?:\/\//.test(path)) return path;
    return `${this.baseUrl}${path.startsWith("/") ? path : `/${path}`}`;
  }

  /** Prefix a versioned API route with the configured basePath. */
  private v1(path: string): string {
    return `${this.basePath}${path}`;
  }

  private async requestJSON<T>(
    method: string,
    path: string,
    body: unknown,
    overrides?: RequestOverrides,
  ): Promise<T> {
    const headers: Record<string, string> = {
      Accept: "application/json",
      "User-Agent": this.userAgent,
      ...overrides?.headers,
    };
    let payload: string | undefined;
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
      payload = JSON.stringify(body);
    }
    return this.send<T>(method, path, payload, headers, overrides);
  }

  private async requestRaw<T>(
    method: string,
    path: string,
    body: BodyInit,
    overrides?: RequestOverrides,
  ): Promise<T> {
    const headers: Record<string, string> = {
      Accept: "application/json",
      "User-Agent": this.userAgent,
      ...overrides?.headers,
    };
    return this.send<T>(method, path, body, headers, overrides);
  }

  private async send<T>(
    method: string,
    path: string,
    body: BodyInit | undefined,
    headers: Record<string, string>,
    overrides?: RequestOverrides,
  ): Promise<T> {
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    const timeoutMs = overrides?.timeoutMs ?? this.timeoutMs;
    const controller = new AbortController();
    const timer =
      timeoutMs > 0 ? setTimeout(() => controller.abort(new Error("speechkit timeout")), timeoutMs) : null;
    if (overrides?.signal) {
      overrides.signal.addEventListener("abort", () => controller.abort(overrides.signal?.reason));
    }
    const init: RequestInit = {
      method,
      headers,
      signal: controller.signal,
    };
    if (body !== undefined) init.body = body;
    try {
      const response = await this.fetchImpl(this.resolve(path), init);
      const text = await response.text();
      if (!response.ok) {
        throw new HTTPError(response.status, text);
      }
      if (text.length === 0) return undefined as T;
      return JSON.parse(text) as T;
    } finally {
      if (timer) clearTimeout(timer);
    }
  }
}

function normalizeBasePath(basePath: string | undefined): string {
  const raw = (basePath ?? "/v1").trim();
  if (raw === "" || raw === "/") return "";
  const withLeading = raw.startsWith("/") ? raw : `/${raw}`;
  return withLeading.replace(/\/+$/, "");
}
