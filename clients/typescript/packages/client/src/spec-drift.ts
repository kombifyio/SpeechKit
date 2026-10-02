// Compile-time guard: hand-written wire types in types.ts must stay assignable
// to the schemas generated from docs/server/openapi.v1.yaml. The build fails if
// the spec changes a field type the hand-written type relies on. Types-only:
// nothing is emitted at runtime.
import type { components } from "./generated/openapi.js";
import type {
  AudioAsset,
  CatalogReadiness,
  DiarizationResult,
  DictionaryEntry,
  ModeContract,
  ModelVariant,
  ProviderProfile,
  ReadinessAction,
  ReadinessArtifact,
  ReadinessRequirement,
  Transcript,
  TranscribeResponse,
  TTSSynthesizeRequest,
  TTSSynthesizeResponse,
  Voice,
  VoiceAgentSessionTicket,
  VoiceAgentSummary,
  VoiceAgentTranscript,
} from "./types.js";

type Schemas = components["schemas"];
type Assert<_T extends true> = never;
type Covered<Local, Spec> = [Local] extends [Spec] ? true : false;

export type SpecDriftGuard = [
  Assert<Covered<ProviderProfile, Schemas["ProviderProfile"]>>,
  Assert<Covered<ModeContract, Schemas["ModeContract"]>>,
  Assert<Covered<DictionaryEntry, Schemas["DictionaryEntry"]>>,
  Assert<Covered<VoiceAgentTranscript, Schemas["VoiceAgentTranscript"]>>,
  Assert<Covered<TTSSynthesizeResponse, Schemas["TTSSynthesizeResponse"]>>,
  Assert<Covered<VoiceAgentSummary, Schemas["VoiceAgentSummaryResponse"]>>,
  Assert<Covered<Transcript, Schemas["Transcript"]>>,
  Assert<Covered<AudioAsset, Schemas["AudioAsset"]>>,
  Assert<Covered<Voice, Schemas["Voice"]>>,
  Assert<Covered<TTSSynthesizeRequest, Schemas["TTSSynthesizeRequest"]>>,
  Assert<Covered<TranscribeResponse, Schemas["DictateResponse"]>>,
  Assert<Covered<CatalogReadiness, Schemas["ProviderReadiness"]>>,
  Assert<Covered<DiarizationResult, Schemas["DiarizationResult"]>>,
  Assert<Covered<ModelVariant, Schemas["ModelVariant"]>>,
  Assert<Covered<ReadinessRequirement, Schemas["ReadinessRequirement"]>>,
  Assert<Covered<ReadinessAction, Schemas["ReadinessAction"]>>,
  Assert<Covered<ReadinessArtifact, Schemas["ReadinessArtifact"]>>,
  Assert<Covered<VoiceAgentSessionTicket, Schemas["CreateSessionResponse"]>>,
];
