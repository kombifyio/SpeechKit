// Control-plane client for the SpeechKit desktop host.
//
// The desktop host serves docs/api/openapi.v1.yaml on a local HTTP port and
// rejects every request, reads included, that lacks the per-session token in
// X-SpeechKit-Control-Token. This client is the typed surface a managed
// product UI uses instead of app-local fetch wrappers. How the token reaches
// the UI (a meta element in the served HTML, the Wails binding) is a desktop
// concern: the client only asks a `tokenProvider` for it.
import type { components } from "./generated/desktop-openapi.js";
import { HTTPError } from "./types.js";

type Schemas = components["schemas"];

export type DesktopSettingsState = Schemas["SettingsState"];
export type DesktopOverlayState = Schemas["OverlayState"];
export type DesktopFeatures = Schemas["Features"];
export type DesktopAppVersion = Schemas["AppVersion"];
export type DesktopMode = Schemas["Mode"];
export type DesktopModesResponse = Schemas["ModesResponse"];
export type DesktopCommandAccepted = Schemas["CommandAccepted"];
export type DesktopMeetingSettings = Schemas["MeetingSettings"];
export type DesktopMeetingSettingsPatch = Schemas["MeetingSettingsPatch"];
export type RecordingSession = Schemas["RecordingSession"];
export type RecordingSessionCreateRequest = Schemas["RecordingSessionCreateRequest"];
export type RecordingSessionCaptureResponse = Schemas["RecordingSessionCaptureResponse"];
export type RecordingSessionState = Schemas["RecordingSessionStateResponse"];
export type RecordingSessionNotes = Schemas["RecordingSessionNotes"];
export type RecordingSessionReview = Schemas["RecordingSessionEnhancement"];
export type RecordingSessionSnapshot = Schemas["RecordingSessionSnapshot"];
export type RecordingSessionSummaryRequest = Schemas["RecordingSessionSummaryRequest"];
export type RecordingSessionSummary = Schemas["RecordingSessionSummaryResponse"];
export type RecordingSessionSearchHit = Schemas["RecordingSessionSearchHit"];
export type RecordingSessionImport = Schemas["RecordingSessionImport"];
export type MeetingSummaryBatch = Schemas["MeetingSummaryBatch"];
export type TeamsPreflight = Schemas["TeamsPreflight"];
export type TranscriptPasteResult = Schemas["TranscriptPasteResult"];
export type Notebook = Schemas["Notebook"];
export type NotebookRequest = Schemas["NotebookRequest"];
export type NotebookDestinationOption = Schemas["NotebookDestinationOption"];
export type FoundryAuthStatus = Schemas["FoundryAuthStatus"];
export type FoundryLoginStatus = Schemas["FoundryLoginStatus"];
export type FoundryLoginMode = "azure_cli" | "browser" | "device_code";
export type Microsoft365Integration = Schemas["Microsoft365Integration"];
export type Microsoft365IntegrationPatch = Schemas["Microsoft365IntegrationPatch"];
export type Microsoft365ConnectLevel = "basic" | "transcripts" | "recaps";
export type PrivacyRequest = Schemas["PrivacyRequest"];
export type PrivacyDeleteResponse = Schemas["PrivacyDeleteResponse"];
export type PrivacyExportResponse = Schemas["PrivacyExportResponse"];

/** The header the desktop host requires on every control-plane request. */
export const CONTROL_TOKEN_HEADER = "X-SpeechKit-Control-Token";

export interface DesktopClientOptions {
  /**
   * Origin of the desktop host's control plane, e.g. `http://127.0.0.1:53211`.
   * Empty (the default) sends relative URLs, which is right for a UI the host
   * itself serves.
   */
  baseUrl?: string;
  /** Resolves the per-session control-plane token. Called lazily, cached until the host answers 401. */
  tokenProvider: () => string | Promise<string>;
  /** Custom fetch. Defaults to globalThis.fetch. */
  fetch?: typeof fetch;
  /** Request timeout in milliseconds; 0 disables it. Defaults to 30_000. */
  timeoutMs?: number;
}

export interface DesktopRequestOptions {
  signal?: AbortSignal;
}

export interface RecordingSessionListQuery {
  kind?: "dictation" | "meeting";
  limit?: number;
  includeSegments?: boolean;
}

export interface StartReviewRequest {
  /** Template to write up as; empty means the general meeting template. */
  templateSlug?: string;
  /** BCP-47 tag of the language to write up in. */
  language?: string;
}

/** A media object loaded through fetch, exposed as a blob: URL. */
export interface ObjectUrl {
  url: string;
  /** Releases the blob. Call it when the element showing it unmounts. */
  revoke(): void;
}

function trimTrailingSlashes(value: string): string {
  let end = value.length;
  while (end > 0 && value.charCodeAt(end - 1) === 47) end -= 1;
  return value.slice(0, end);
}

class DesktopTransport {
  private cachedToken: string | null = null;

  constructor(
    private readonly baseUrl: string,
    private readonly tokenProvider: () => string | Promise<string>,
    private readonly fetchImpl: typeof fetch,
    private readonly timeoutMs: number,
  ) {}

  private async token(): Promise<string> {
    if (this.cachedToken) return this.cachedToken;
    const token = String(await this.tokenProvider()).trim();
    if (!token) throw new Error("speechkit: the control-plane token provider returned no token");
    this.cachedToken = token;
    return token;
  }

  async send(
    method: string,
    path: string,
    init: { body?: unknown; accept?: string },
    options?: DesktopRequestOptions,
  ): Promise<Response> {
    const headers: Record<string, string> = {
      Accept: init.accept ?? "application/json",
      [CONTROL_TOKEN_HEADER]: await this.token(),
    };
    let payload: string | undefined;
    if (init.body !== undefined) {
      headers["Content-Type"] = "application/json";
      payload = JSON.stringify(init.body);
    }
    const controller = new AbortController();
    const timer =
      this.timeoutMs > 0
        ? setTimeout(() => controller.abort(new Error("speechkit timeout")), this.timeoutMs)
        : null;
    const signal = options?.signal;
    const onAbort = () => controller.abort(signal?.reason);
    if (signal) {
      if (signal.aborted) onAbort();
      else signal.addEventListener("abort", onAbort, { once: true });
    }
    try {
      const requestInit: RequestInit = { method, headers, signal: controller.signal };
      if (payload !== undefined) requestInit.body = payload;
      const response = await this.fetchImpl(`${this.baseUrl}${path}`, requestInit);
      if (!response.ok) {
        if (response.status === 401) this.cachedToken = null;
        throw new HTTPError(response.status, await response.text());
      }
      return response;
    } finally {
      if (timer) clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
    }
  }

  async json<T>(
    method: string,
    path: string,
    body?: unknown,
    options?: DesktopRequestOptions,
  ): Promise<T> {
    const init: { body?: unknown } = {};
    if (body !== undefined) init.body = body;
    const response = await this.send(method, path, init, options);
    const text = await response.text();
    return (text.length === 0 ? undefined : JSON.parse(text)) as T;
  }

  async blob(path: string, accept: string, options?: DesktopRequestOptions): Promise<Blob> {
    return (await this.send("GET", path, { accept }, options)).blob();
  }
}

const seg = (id: number | string): string => encodeURIComponent(String(id));

/**
 * Typed client for the desktop control plane (docs/api/openapi.v1.yaml).
 * Build it with {@link createDesktopClient}.
 */
export class DesktopClient {
  readonly settings: {
    /** GET /settings/state: includes `policyLockedFields`, the keys an administrator policy pins. */
    state(options?: DesktopRequestOptions): Promise<DesktopSettingsState>;
    /** The configuration keys an administrator policy pins; empty when none is. */
    lockedFields(options?: DesktopRequestOptions): Promise<string[]>;
    /** GET /api/v1/settings/meeting */
    meeting(options?: DesktopRequestOptions): Promise<DesktopMeetingSettings>;
    /** PATCH /api/v1/settings/meeting */
    updateMeeting(
      patch: DesktopMeetingSettingsPatch,
      options?: DesktopRequestOptions,
    ): Promise<DesktopMeetingSettings>;
  };
  readonly overlay: {
    /** GET /overlay/state: the live dictation, assist and voice agent state. */
    state(options?: DesktopRequestOptions): Promise<DesktopOverlayState>;
  };
  readonly modes: {
    /** GET /api/v1/modes */
    list(options?: DesktopRequestOptions): Promise<DesktopModesResponse>;
    /** POST /api/v1/modes/{mode}/start */
    start(mode: DesktopMode, options?: DesktopRequestOptions): Promise<DesktopCommandAccepted>;
    /** POST /api/v1/modes/{mode}/stop */
    stop(mode: DesktopMode, options?: DesktopRequestOptions): Promise<DesktopCommandAccepted>;
  };
  readonly recordings: {
    list(query?: RecordingSessionListQuery, options?: DesktopRequestOptions): Promise<RecordingSession[]>;
    get(id: number, options?: DesktopRequestOptions): Promise<RecordingSession>;
    create(
      request: RecordingSessionCreateRequest,
      options?: DesktopRequestOptions,
    ): Promise<RecordingSession>;
    /** Capture lifecycle: POST .../start, pause, resume, stop. */
    start(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionCaptureResponse>;
    pause(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionCaptureResponse>;
    resume(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionCaptureResponse>;
    stop(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionCaptureResponse>;
    /** POST .../finish: marks the session finished and stores the supplied summary. */
    finish(
      id: number,
      summary: RecordingSessionSummaryRequest,
      options?: DesktopRequestOptions,
    ): Promise<RecordingSession>;
    /** GET .../state: runtime capture state, not the outcome of the last command. */
    state(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionState>;
    notes(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionNotes>;
    saveNotes(
      id: number,
      notes: RecordingSessionNotes,
      options?: DesktopRequestOptions,
    ): Promise<RecordingSessionNotes>;
    /** GET .../enhancements (the spec's name for the write-ups of a meeting). */
    reviews(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionReview[]>;
    /** POST .../enhancements: starts a write-up run; poll {@link reviews}. */
    startReview(
      id: number,
      request?: StartReviewRequest,
      options?: DesktopRequestOptions,
    ): Promise<RecordingSessionReview>;
    /** POST .../open and .../close: the notebook of a session. */
    open(id: number, options?: DesktopRequestOptions): Promise<RecordingSession>;
    close(id: number, options?: DesktopRequestOptions): Promise<RecordingSession>;
    summary(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionSummary>;
    summaryBatches(id: number, options?: DesktopRequestOptions): Promise<MeetingSummaryBatch[]>;
    search(
      query: string,
      filter?: { kind?: "dictation" | "meeting"; limit?: number },
      options?: DesktopRequestOptions,
    ): Promise<RecordingSessionSearchHit[]>;
    /** GET .../imports: the Teams transcript and Copilot recap import rows. */
    imports(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionImport[]>;
    /** POST .../preflight: checks the Microsoft 365 transcript path once. */
    runPreflight(id: number, options?: DesktopRequestOptions): Promise<TeamsPreflight>;
    /** GET .../preflight: the recorded outcome; undefined when none was recorded (HTTP 204). */
    preflight(id: number, options?: DesktopRequestOptions): Promise<TeamsPreflight | undefined>;
    /** POST .../transcript-paste with a JSON body: adds a transcript to a finished meeting. */
    pasteTranscript(
      id: number,
      text: string,
      replace?: boolean,
      options?: DesktopRequestOptions,
    ): Promise<TranscriptPasteResult>;
    snapshots(id: number, options?: DesktopRequestOptions): Promise<RecordingSessionSnapshot[]>;
    snapshot(
      id: number,
      snapshotId: number,
      options?: DesktopRequestOptions,
    ): Promise<RecordingSessionSnapshot>;
    captureSnapshot(
      id: number,
      note?: string,
      options?: DesktopRequestOptions,
    ): Promise<RecordingSessionSnapshot>;
    deleteSnapshot(id: number, snapshotId: number, options?: DesktopRequestOptions): Promise<void>;
    /**
     * GET .../snapshots/{id}/image through fetch, as a blob: URL. An <img>
     * cannot send the token header, so its source must never point at the
     * route directly.
     */
    snapshotImage(
      id: number,
      snapshotId: number,
      options?: DesktopRequestOptions,
    ): Promise<ObjectUrl>;
  };
  readonly notebooks: {
    list(options?: DesktopRequestOptions): Promise<Notebook[]>;
    get(id: number, options?: DesktopRequestOptions): Promise<Notebook>;
    create(request: NotebookRequest, options?: DesktopRequestOptions): Promise<Notebook>;
    update(id: number, request: NotebookRequest, options?: DesktopRequestOptions): Promise<Notebook>;
    delete(id: number, options?: DesktopRequestOptions): Promise<{ id: number; deleted: boolean }>;
    /** GET /api/v1/notebooks/destinations: 503 (HTTPError) until the OneNote connector is wired. */
    destinations(options?: DesktopRequestOptions): Promise<NotebookDestinationOption[]>;
  };
  readonly foundry: {
    status(options?: DesktopRequestOptions): Promise<FoundryAuthStatus>;
    /** Starts the sign-in job; poll {@link loginStatus}. */
    login(
      mode: FoundryLoginMode,
      tenantId?: string,
      options?: DesktopRequestOptions,
    ): Promise<{ jobId: string }>;
    loginStatus(options?: DesktopRequestOptions): Promise<FoundryLoginStatus>;
    logout(options?: DesktopRequestOptions): Promise<FoundryAuthStatus>;
  };
  readonly microsoft365: {
    status(options?: DesktopRequestOptions): Promise<Microsoft365Integration>;
    update(
      patch: Microsoft365IntegrationPatch,
      options?: DesktopRequestOptions,
    ): Promise<Microsoft365Integration>;
    /** Starts the browser sign-in; follow `connect` in the returned status. */
    connect(
      level?: Microsoft365ConnectLevel,
      options?: DesktopRequestOptions,
    ): Promise<Microsoft365Integration>;
    disconnect(options?: DesktopRequestOptions): Promise<Microsoft365Integration>;
  };
  readonly privacy: {
    /** POST /api/v1/privacy/export as JSON (GDPR Art. 15). */
    export(
      request?: Omit<PrivacyRequest, "scope" | "format"> & { scope?: PrivacyRequest["scope"] },
      options?: DesktopRequestOptions,
    ): Promise<PrivacyExportResponse>;
    /** The same export as a ZIP with the audio files. */
    exportArchive(
      request?: Omit<PrivacyRequest, "scope" | "format"> & { scope?: PrivacyRequest["scope"] },
      options?: DesktopRequestOptions,
    ): Promise<Blob>;
    /** POST /api/v1/privacy/delete (GDPR Art. 17); sends `confirm: true`. */
    delete(
      request?: Omit<PrivacyRequest, "scope" | "confirm" | "format"> & {
        scope?: PrivacyRequest["scope"];
      },
      options?: DesktopRequestOptions,
    ): Promise<PrivacyDeleteResponse>;
  };
  /** GET /features: which capabilities this build and install provide. */
  readonly features: (options?: DesktopRequestOptions) => Promise<DesktopFeatures>;
  readonly app: {
    /** GET /app/version */
    version(options?: DesktopRequestOptions): Promise<DesktopAppVersion>;
  };

  constructor(opts: DesktopClientOptions) {
    if (typeof opts.tokenProvider !== "function") {
      throw new Error("speechkit: a desktop client needs a tokenProvider");
    }
    const baseUrl = trimTrailingSlashes(opts.baseUrl ?? "");
    if (baseUrl !== "" && !/^https?:\/\//.test(baseUrl)) {
      throw new Error("baseUrl must include http:// or https://");
    }
    const http = new DesktopTransport(
      baseUrl,
      opts.tokenProvider,
      opts.fetch ?? globalThis.fetch.bind(globalThis),
      opts.timeoutMs ?? 30_000,
    );
    const rec = (id: number) => `/api/v1/recording-sessions/${seg(id)}`;
    const capture = (verb: "start" | "pause" | "resume" | "stop") =>
      (id: number, options?: DesktopRequestOptions) =>
        http.json<RecordingSessionCaptureResponse>("POST", `${rec(id)}/${verb}`, undefined, options);

    this.settings = {
      state: (options) => http.json("GET", "/settings/state", undefined, options),
      lockedFields: async (options) =>
        (await http.json<DesktopSettingsState>("GET", "/settings/state", undefined, options))
          .policyLockedFields ?? [],
      meeting: (options) => http.json("GET", "/api/v1/settings/meeting", undefined, options),
      updateMeeting: (patch, options) => http.json("PATCH", "/api/v1/settings/meeting", patch, options),
    };
    this.overlay = {
      state: (options) => http.json("GET", "/overlay/state", undefined, options),
    };
    this.modes = {
      list: (options) => http.json("GET", "/api/v1/modes", undefined, options),
      start: (mode, options) => http.json("POST", `/api/v1/modes/${seg(mode)}/start`, undefined, options),
      stop: (mode, options) => http.json("POST", `/api/v1/modes/${seg(mode)}/stop`, undefined, options),
    };
    this.recordings = {
      list: (query, options) => {
        const params = new URLSearchParams();
        if (query?.kind) params.set("kind", query.kind);
        if (query?.limit !== undefined) params.set("limit", String(query.limit));
        if (query?.includeSegments !== undefined) params.set("include_segments", String(query.includeSegments));
        const qs = params.toString();
        return http.json("GET", `/api/v1/recording-sessions${qs ? `?${qs}` : ""}`, undefined, options);
      },
      get: (id, options) => http.json("GET", rec(id), undefined, options),
      create: (request, options) => http.json("POST", "/api/v1/recording-sessions", request, options),
      start: capture("start"),
      pause: capture("pause"),
      resume: capture("resume"),
      stop: capture("stop"),
      finish: (id, summary, options) => http.json("POST", `${rec(id)}/finish`, summary, options),
      state: (id, options) => http.json("GET", `${rec(id)}/state`, undefined, options),
      notes: (id, options) => http.json("GET", `${rec(id)}/notes`, undefined, options),
      saveNotes: (id, notes, options) => http.json("PUT", `${rec(id)}/notes`, notes, options),
      reviews: (id, options) => http.json("GET", `${rec(id)}/enhancements`, undefined, options),
      startReview: (id, request, options) =>
        http.json("POST", `${rec(id)}/enhancements`, request ?? {}, options),
      open: (id, options) => http.json("POST", `${rec(id)}/open`, undefined, options),
      close: (id, options) => http.json("POST", `${rec(id)}/close`, undefined, options),
      summary: (id, options) => http.json("GET", `${rec(id)}/summary`, undefined, options),
      summaryBatches: (id, options) => http.json("GET", `${rec(id)}/summary-batches`, undefined, options),
      search: (query, filter, options) => {
        const params = new URLSearchParams({ q: query });
        if (filter?.kind) params.set("kind", filter.kind);
        if (filter?.limit !== undefined) params.set("limit", String(filter.limit));
        return http.json("GET", `/api/v1/recording-sessions/search?${params.toString()}`, undefined, options);
      },
      imports: (id, options) => http.json("GET", `${rec(id)}/imports`, undefined, options),
      runPreflight: (id, options) => http.json("POST", `${rec(id)}/preflight`, undefined, options),
      preflight: (id, options) => http.json("GET", `${rec(id)}/preflight`, undefined, options),
      pasteTranscript: (id, text, replace, options) =>
        http.json("POST", `${rec(id)}/transcript-paste`, replace === undefined ? { text } : { text, replace }, options),
      snapshots: (id, options) => http.json("GET", `${rec(id)}/snapshots`, undefined, options),
      snapshot: (id, snapshotId, options) =>
        http.json("GET", `${rec(id)}/snapshots/${seg(snapshotId)}`, undefined, options),
      captureSnapshot: (id, note, options) =>
        http.json("POST", `${rec(id)}/snapshots`, note === undefined ? {} : { note }, options),
      deleteSnapshot: async (id, snapshotId, options) => {
        await http.json("DELETE", `${rec(id)}/snapshots/${seg(snapshotId)}`, undefined, options);
      },
      snapshotImage: async (id, snapshotId, options) => {
        const blob = await http.blob(`${rec(id)}/snapshots/${seg(snapshotId)}/image`, "image/*", options);
        const url = URL.createObjectURL(blob);
        return { url, revoke: () => URL.revokeObjectURL(url) };
      },
    };
    this.notebooks = {
      list: (options) => http.json("GET", "/api/v1/notebooks", undefined, options),
      get: (id, options) => http.json("GET", `/api/v1/notebooks/${seg(id)}`, undefined, options),
      create: (request, options) => http.json("POST", "/api/v1/notebooks", request, options),
      update: (id, request, options) => http.json("PATCH", `/api/v1/notebooks/${seg(id)}`, request, options),
      delete: (id, options) => http.json("DELETE", `/api/v1/notebooks/${seg(id)}`, undefined, options),
      destinations: (options) => http.json("GET", "/api/v1/notebooks/destinations", undefined, options),
    };
    this.foundry = {
      status: (options) => http.json("GET", "/auth/foundry/status", undefined, options),
      login: (mode, tenantId, options) =>
        http.json("POST", "/auth/foundry/login", tenantId ? { mode, tenantId } : { mode }, options),
      loginStatus: (options) => http.json("GET", "/auth/foundry/login/status", undefined, options),
      logout: (options) => http.json("POST", "/auth/foundry/logout", undefined, options),
    };
    this.microsoft365 = {
      status: (options) => http.json("GET", "/api/v1/integrations/microsoft365", undefined, options),
      update: (patch, options) => http.json("PATCH", "/api/v1/integrations/microsoft365", patch, options),
      connect: (level, options) =>
        http.json("POST", "/api/v1/integrations/microsoft365/connect", level ? { level } : {}, options),
      disconnect: (options) =>
        http.json("POST", "/api/v1/integrations/microsoft365/disconnect", undefined, options),
    };
    this.privacy = {
      export: (request, options) =>
        http.json("POST", "/api/v1/privacy/export", { scope: "install", ...request }, options),
      exportArchive: async (request, options) => {
        const response = await http.send(
          "POST",
          "/api/v1/privacy/export",
          { body: { scope: "install", ...request, format: "zip" }, accept: "application/zip" },
          options,
        );
        return response.blob();
      },
      delete: (request, options) =>
        http.json("POST", "/api/v1/privacy/delete", { scope: "install", ...request, confirm: true }, options),
    };
    this.features = (options) => http.json("GET", "/features", undefined, options);
    this.app = {
      version: (options) => http.json("GET", "/app/version", undefined, options),
    };
  }
}

/**
 * Build the desktop control-plane client.
 *
 * ```ts
 * const client = createDesktopClient({
 *   tokenProvider: () => readTokenFromHostPage(),
 * });
 * const { policyLockedFields } = await client.settings.state();
 * ```
 */
export function createDesktopClient(opts: DesktopClientOptions): DesktopClient {
  return new DesktopClient(opts);
}
