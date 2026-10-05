# Provider recognition definitions

Hosts supply Words, explicit recognition hints and option sources. The concrete
adapter owns the definition for its provider, model and transport. Host code
does not choose native field names or branch on provider identity.

`customize.BuildRecognitionHints(words)` renders enabled canonical terms into
a transcript-style prompt and canonical terms plus pronunciation aliases into
keyterms. `TranscribeOpts.WithVocabulary(words)` keeps these dictionary hints
separate from explicit `Prompt` and `Keyterms`. Word weights are not currently
mapped to per-word native boosts; Google Chirp 3 uses an adapter-level fixed
boost. Vocabulary guidance never guarantees an exact recognized spelling.

## Shared selection and resolution

`ProviderOptionManifest.RecognitionBiasOption()` is the common channel selector
for `customize.BuildProviderBiasForManifest` previews and STT recognition.
Implemented native, derived or emulated keyterms take precedence over an
implemented prompt channel. Unsupported, unimplemented, unknown and
provider-default channels cannot carry dictionary hints. This selects one
dictionary channel; explicit request prompts and keyterms keep their existing
precedence and adapter behavior.

`ResolveTranscribeOptions` uses the built-in provider/modality manifest.
`ResolveTranscribeOptionsWithManifest` accepts the adapter's concrete definition
without a mutable global registry or changes to desktop/server consumers. The
manifest's `NativeKey` documents the wire destination; the adapter serializes
the resolved value and enforces its model and transport restrictions. Unsupported
explicit options remain in `Effective.Unsupported`; a definition is not an
instruction to serialize an unsupported field.

```go
manifest := provideropts.OpenAICompatibleSTTManifest(provider.Name())
request := stt.TranscribeOpts{Language: "multi"}.WithVocabulary(words)
resolved := stt.ResolveTranscribeOptionsWithManifest(
    manifest, profileID, request.ForProvider(provider.Name()), defaults, overrides,
)
preview := customize.BuildProviderBiasForManifest(words, manifest)
// Serialize resolved.Prompt using this adapter's declared multipart prompt field.
```

An adapter can supply a different manifest for each model or native transport.
The OpenAI-compatible adapter exposes `TranscribeManifest(model)` for that
purpose: current OpenAI `gpt-transcribe` uses native keywords; its older models
and generic multipart routes use the prompt channel, even when a route's
identity is shared with another adapter.

Native stream consumers use the same option sources:

```go
request := stt.TranscribeOptionsFromStream(streamOptions).ForProvider(provider.Name())
resolved := stt.ResolveTranscribeOptionsWithManifest(
    streamManifest, profileID, request, defaults, overrides,
)
streamOptions = stt.ApplyResolvedDictationStreamOptions(streamOptions, resolved)
// Serialize the stream's declared native vocabulary/context fields.
```

The precedence remains provider defaults, global defaults (`Options`), provider
overrides, then explicit request overrides (`RequestOptions` and typed request
hints). `vocabulary_bias=false` at global or provider scope suppresses dictionary
hints; an explicit request override may re-enable them. Providers still decide
whether explicit hints are supported and how to serialize them. Stored Words,
template selection, authenticated scope and final Replacements stay in the
shared customization consumer path.

## Current adapter wire destinations

This table describes the implemented Dictation/Assist STT and meeting paths.
Native Voice Agent has its own modality definition and session configuration.
Limits belong to the concrete adapter, rather than host-specific truncation.

| Adapter and transport | Dictionary destination | Adapter constraints |
| --- | --- | --- |
| Deepgram Listen batch/native stream | Query `keyterm` for Nova-3; `keywords` on earlier supported models | Trimmed, deduplicated, at most 100 terms; model selects the native parameter. |
| OpenAI `gpt-transcribe` batch | Multipart `keywords[]` | Enabled only for the model family that this serializer supports. Explicit prompt remains separate. |
| OpenAI native dictation stream | Session transcription `keywords` | Uses the live transcription model and its native session serializer. |
| Older OpenAI models; generic OpenAI-compatible routes including Groq, Ollama, VPS and Foundry multipart | Multipart `prompt` | Canonical terms rendered as transcript context; generic routes expose no native keyterm field. |
| Local whisper.cpp batch | Multipart `prompt` | Local process/model lifecycle remains adapter-owned. |
| Azure Speech fast transcription (Foundry MAI) batch | Multipart definition `phraseList.phrases` | Trimmed and deduplicated; configured `MaxPhrases`, default 100. Separate from Foundry's OpenAI-compatible route. |
| Google Speech-to-Text v1 batch | `config.speechContexts[].phrases` | Language candidate handling and endpoint version remain adapter-owned. |
| Google Chirp 3 batch | `config.adaptation.phraseSets[].inlinePhraseSet.phrases[].value` | Uses the adapter's fixed phrase boost; needs its v2 regional endpoint and credentials. |
| Gemini Transcribe batch/native stream | `transcription_config.custom_vocabulary` | Keyterms channel; no free-text prompt mapping. |
| AssemblyAI batch | `keyterms_prompt` | Synchronous transcription caps the total keyterms payload at 2048 characters. |
| AssemblyAI native dictation stream | Query `keyterms_prompt`; explicit context uses `agent_context` | At most 100 terms, each at most 50 runes; context at most 1750 runes. |
| Hugging Face routed ASR; OpenRouter transcription | No dictionary recognition channel | Their current serializers expose neither keyterms nor prompt hints. Final Replacements can still transform recognized text. |

Built-in manifests are provider/modality defaults, not a promise that every
model and transport accepts every option. A custom adapter should declare only
channels its serializer implements, use the supplied-manifest resolver for the
concrete request, and verify the resulting network or subprocess effect with a
fixture. Adding an adapter does not require a new host vocabulary path.
