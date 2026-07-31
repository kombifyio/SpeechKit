// Manual type definitions mirroring the OpenAPI schemas in
// docs/server/openapi.v1.yaml and the AsyncAPI WS frame schemas in
// docs/server/asyncapi.v1.yaml. Until openapi-typescript codegen is wired
// up (separate PR), these are hand-maintained — additive across minor
// versions, never rename or remove without a major bump.

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
  duration_ms?: number;
  latency_ms?: number;
  provider?: string;
  model?: string;
  confidence?: number;
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
  storageKind: string;
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
  discovery?: string;
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
  missing?: string[];
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
  ticket: string;
  ws_url: string;
  /**
   * WebSocket subprotocol value to pass during upgrade, e.g.
   * `ticket.<value>`. Prefer this over placing the ticket in URLs.
   */
  ws_subprotocol?: string;
  expires_at?: string;
}

export interface VoiceAgentTranscript {
  id: number;
  transcript: string;
  turns?: Array<Record<string, unknown>>;
  language: string;
  created_at: string;
}

export interface VoiceAgentSummary {
  id: number;
  summary: Record<string, unknown>;
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
