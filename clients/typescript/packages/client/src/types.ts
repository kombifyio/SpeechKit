// Wire types for the SpeechKit Server v1 API. The generated types live in
// ./generated/openapi.ts (`pnpm run gen:types`, from docs/server/openapi.v1.yaml;
// CI fails on drift) and are re-exported from the package root as `components`,
// `paths` and `operations`. The interfaces below are hand-written, stricter
// views kept for API stability, additive
// across minor versions, never rename or remove without a major bump.
// ./spec-drift.ts fails the build if they stop matching the generated schemas.
// AsyncAPI WS frames (docs/server/asyncapi.v1.yaml) are still hand-maintained.

export interface Status {
  status: string;
  components?: Record<string, unknown>;
  uptime_seconds?: number;
  version?: string;
}

export interface TranscribeOptions {
  language?: string;
  model?: string;
  prompt?: string;
}

export interface TranscribeResponse {
  text: string;
  language?: string;
  duration_ms: number;
  latency_ms: number;
  provider?: string;
  model?: string;
  confidence?: number;
  speakers?: DiarizationResult;
}

export interface DictionaryEntry {
  id?: number;
  spoken: string;
  canonical: string;
  language: string;
  source?: string;
  enabled: boolean;
  usageCount?: number;
  createdAt?: string;
  updatedAt?: string;
}

export interface AudioAsset {
  storageKind: "local-file";
  /** @deprecated The server never sends the file path (json:"-"); always undefined. */
  path?: string;
  mimeType: string;
  sizeBytes: number;
  durationMs: number;
}

export interface Transcript {
  id: number;
  text: string;
  language: string;
  provider: string;
  model: string;
  durationMs: number;
  latencyMs: number;
  audioPath?: string;
  audio?: AudioAsset;
  createdAt: string;
  ownerUserId?: string;
  ownerOrgId?: string;
  ownerSource?: string;
  /** Speaker diarization result; omitted when the transcript has none. */
  speakers?: DiarizationResult;
  /** Always emitted by the server (no omitempty). */
  pinned: boolean;
}

export interface Speaker {
  label: string;
  personId?: string;
  displayName?: string;
  role?: string;
  confidence?: number;
  attributionConfidence?: number;
}

export interface SpeakerWord {
  text: string;
  startMs?: number;
  endMs?: number;
  confidence?: number;
  speakerLabel?: string;
  speakerConfidence?: number;
  personId?: string;
  displayName?: string;
  role?: string;
  attributionConfidence?: number;
}

export interface SpeakerSegment {
  text: string;
  startMs?: number;
  endMs?: number;
  speakerLabel?: string;
  speakerConfidence?: number;
  personId?: string;
  displayName?: string;
  role?: string;
  attributionConfidence?: number;
  words?: SpeakerWord[];
}

export interface DiarizationResult {
  provider?: string;
  model?: string;
  level?: "none" | "diarization" | "attribution" | "provider_identification" | "biometric";
  text?: string;
  language?: string;
  speakers?: Speaker[];
  segments?: SpeakerSegment[];
  words?: SpeakerWord[];
}

export interface TTSSynthesizeRequest {
  text: string;
  locale?: string;
  voice?: string;
  speed?: number;
  format?: string;
}

export interface TTSSynthesizeResponse {
  audio_base64: string;
  format: string;
  sample_rate?: number;
  duration_ms?: number;
  provider?: string;
  voice?: string;
}

export interface Voice {
  provider: string;
  id: string;
  locale: string;
  default: boolean;
  discovery: "configured";
}

export interface CatalogReadiness {
  schemaVersion?: string;
  profileId: string;
  mode: string;
  providerKind: string;
  executionMode?: string;
  modelId?: string;
  source?: string;
  active: boolean;
  default: boolean;
  configured: boolean;
  credentialsReady: boolean;
  runtimeReady: boolean;
  capabilityReady: boolean;
  ready: boolean;
  missing?: Array<"mode_disabled" | "provider_disabled" | "credentials" | "runtime">;
  /** The active network scope forbids this profile; `ready` is then false. */
  blockedByScope?: boolean;
  disabledReasonId?: string;
  requirements?: ReadinessRequirement[];
  actions?: ReadinessAction[];
  artifacts?: ReadinessArtifact[];
}

export interface ReadinessRequirement {
  id: string;
  label: string;
  category: string;
  required: boolean;
  ready: boolean;
  missing?: string;
}

export interface ReadinessAction {
  id: string;
  label: string;
  kind: string;
  target?: string;
}

export interface ReadinessArtifact {
  id: string;
  name: string;
  kind: string;
  sizeLabel?: string;
  sizeBytes?: number;
  available: boolean;
  selected: boolean;
  runtimeReady?: boolean;
  runtimeProblem?: string;
  recommended?: boolean;
}

export interface ProviderProfile {
  id: string;
  mode: string;
  name: string;
  providerKind: string;
  executionMode?: string;
  modelId?: string;
  source?: string;
  description?: string;
  capabilities?: string[];
  modality?: string;
  supportedLocales?: string[];
  nativeOptions?: string[];
  authRequirement?: string;
  transport?: string;
  evidenceUrl?: string;
  variants?: ModelVariant[];
}

export interface ModelVariant {
  id: string;
  name: string;
  modelId: string;
  description?: string;
  recommended?: boolean;
}

export interface ModeContract {
  mode: string;
  intelligence: string;
  input: string;
  output: string;
  allowed: string[];
  forbidden: string[];
}

export interface VoiceAgentSessionTicket {
  session_id: string;
  ai_session_id?: string;
  ticket: string;
  ws_url: string;
  /**
   * WebSocket subprotocol value to pass during upgrade, e.g.
   * `ticket.<value>`. Prefer this over placing the ticket in URLs.
   */
  ws_subprotocol: string;
  expires_at: string;
}

export interface VoiceAgentTranscript {
  id: number;
  transcript: string;
  turns?: VoiceAgentTurn[];
  language: string;
  created_at: string;
}

export interface VoiceAgentTurn {
  role: string;
  text: string;
  createdAt?: string;
}

export interface VoiceAgentSessionSummary {
  title?: string;
  summary: string;
  ideas?: string[];
  decisions?: string[];
  openQuestions?: string[];
  nextSteps?: string[];
  rawText?: string;
}

export interface VoiceAgentSummary {
  id: number;
  summary: VoiceAgentSessionSummary;
  language: string;
  created_at: string;
}

export class HTTPError extends Error {
  readonly status: number;
  readonly body: string;

  constructor(status: number, body: string) {
    super(`speechkit: HTTP ${status}${body ? `: ${body}` : ""}`);
    this.name = "HTTPError";
    this.status = status;
    this.body = body;
  }
}
