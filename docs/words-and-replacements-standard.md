# Words And Replacements Standard

> Status: active v0.46 framework standard, introduced in v0.44.
> Scope: framework kernel, consumed by Dictation, Assist, Voice Agent, Device,
> Server, and Local-Library embeddings.
> Implementation: public SDK contracts, resolver / applier / service logic,
> scoped stores, Device and Server APIs, Settings UI, and pack-shaped
> import/export paths. Provider-native biasing depth and full command / snippet
> / template runtime migration remain follow-up work.
> Migration stance: Dictionary is not a new framework concept anymore. Existing
> dictionary-shaped code paths are migration adapters only where still present;
> new product, docs, SDK, API, and UI surfaces speak Words and Replacements.

## Product Thesis

SpeechKit has three strict voice modes: Dictation, Assist, and Voice Agent.
Customization is not a fourth mode. It is the cross-mode extension layer that
makes a voice product understand the words that matter and shape the text that
comes out.

Every customization feature in SpeechKit reduces to two core primitives:

| Primitive | Meaning | Direction |
| --- | --- | --- |
| `Word` | Know or recognize this term. | Forward: shapes what STT and LLMs perceive. |
| `Replacement` | When you encounter X, produce Y. | Backward: transforms perceived or generated text. |

From these two primitives:

- a vocabulary is a `Lexicon`, a named collection of Words
- a replacement collection is a `Ruleset`
- synonyms, substitutions, snippets, commands, and templates are Replacement
  kinds
- a Native Template is a versioned, curated Customization Pack plus activation
  metadata
- advanced Assist behavior composes the same primitives instead of inventing a
  parallel customization model

If a future voice customization feature cannot be expressed as Words and/or
Replacements, the standard must be revisited before adding another subsystem.

## Core Primitive Boundary

The core authoring primitives are:

- `Word`
- `Replacement`

The core organizing forms are:

- `Lexicon`
- `Ruleset`
- `Customization Pack`
- Native Template activation metadata

`Dictionary` and `Vocabulary` are compatibility or provider-rendering terms,
not framework concepts for new product surfaces. A Native Template is not a
third authoring primitive; it is a curated pack source that the resolver reads
through the same Words/Replacements path as user, org, app, install, or session
customization data.

## Design Goals

- Model-agnostic: author a Word once and render it to provider-specific STT or
  Voice Agent biasing hints.
- Integration-agnostic: Device, Server, and Local-Library hosts consume the same
  resolved customization set.
- Scope-neutral: the framework supports global, app, install, org, workspace,
  user, and session use cases without deciding product policy.
- Deterministic: replacement order and stage behavior are reproducible.
- Efficient: resolve, compile, and cache a customization set per context;
  invalidate affected cache keys on writes.
- Portable: export/import a versioned Customization Pack with Words,
  Replacements, Lexicons, Rulesets, and attachments.
- Clean migration: Dictionary is replaced as a concept by Words and
  Replacements. Any temporary dictionary-shaped adapter must not become a new
  product surface.

## Word

A `Word` is declarative recognition knowledge. It never rewrites text by itself.

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | Stable identifier. |
| `term` | string | Canonical written form, for example `Kombify`. |
| `sounds_like` | []string | Optional common mishears or phonetic hints. |
| `language` | string | BCP-47 prefix such as `en`, `de`, or empty for all languages. |
| `weight` | float | Optional relative boost, mapped to provider-native scales where supported. |
| `tags` | []string | Context selection, for example `medical` or `project-x`. |
| `scope` | object | Where it applies, for example `app`, `install`, `org`, `workspace`, `user`, or `session`; `org`, `workspace`, `user`, and `session` require an explicit key when selected directly. |
| `source` | string | Where it came from, for example `settings`, `api`, `developer`, `migration`, or `pack:<id>`. |
| `enabled` | bool | Active flag. |
| `usage_count` | int | Telemetry for future learned weighting. |
| `created_at`, `updated_at` | time | Audit and sync metadata. |

`sounds_like` may later seed derived substitution Replacements, but that is an
implementation policy question. The semantic source remains the Word.

## Replacement

A `Replacement` is a deterministic transformation rule. It is one type with a
closed `kind` discriminator.

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | Stable identifier. |
| `match` | object | Trigger specification. |
| `output` | object | Result specification; shape depends on `kind`. |
| `kind` | enum | `substitution`, `synonym`, `snippet`, `command`, or `template`. |
| `language` | string | BCP-47 prefix or empty for all languages. |
| `modes` | []enum | Subset of `dictation`, `assist`, `voice_agent`; default is all. |
| `stage` | enum | `pre_recognition_cleanup`, `post_stt`, `post_llm`, or `output`. |
| `priority` | int | Higher values apply first. Stable ID breaks ties. |
| `tags` | []string | Context selection. A tagged rule is active only when its tags match the resolve context. |
| `scope` | object | Where it applies, independent from source. |
| `source` | string | Where it came from, independent from scope. |
| `enabled` | bool | Active flag. |
| `usage_count` | int | Telemetry. |
| `created_at`, `updated_at` | time | Audit and sync metadata. |

`match` has the shape:

```json
{
  "type": "literal|phrase|regex|spoken_alias",
  "pattern": "string",
  "case_sensitive": false,
  "word_boundary": true
}
```

### Replacement Kinds

| Kind | Purpose | Current scattered equivalent |
| --- | --- | --- |
| `substitution` | Text-to-text rewrite, including spoken form to canonical form. | Former dictionary corrections. |
| `synonym` | Normalize multiple variants to one preferred form. | Implicit or ad hoc today. |
| `snippet` | Expand a short trigger into a static text block. | Host quick-insert behavior. |
| `command` | Emit an intent and optional payload. | `internal/shortcuts` and voice-companion intent phrases. |
| `template` | Produce a slotted prompt/template for Assist/Summarize flows. This Replacement kind is distinct from the Native Template catalog. | Assist rewrite, email, summarize instructions. |

The kind set is closed. Adding another kind is a standard change, not a local
implementation shortcut.

## Collections And Packs

| Collection | Contains | Meaning |
| --- | --- | --- |
| `Lexicon` | `Word` records | A named vocabulary set. |
| `Ruleset` | `Replacement` records | A named transformation set. |

Lexicons and Rulesets can attach to scopes, modes, personas, and tags. A user
can keep a `medical` Lexicon or a `corp-style` Ruleset and activate it only in
the contexts that need it.

A Customization Pack is the portable interchange format for:

- Words
- Replacements
- Lexicons
- Rulesets
- attachments
- language and version metadata

Packs are model-agnostic and integration-agnostic. The same pack should import
into a Windows Device install, a self-hosted Server deployment, or a
Local-Library embedding without provider-specific rewrites.

Pack import is scope-aware and source-aware. A host imports a pack into an
explicit `app`, `install`, `org`, `user`, or `session` scope. Re-importing a
pack replaces only rows from the same `scope + source + language`.

## Native Templates

A native Template is a versioned, curated Customization Pack plus activation
metadata. It is not a new customization concept, it is distinct from the
`Replacement.kind = template` output kind, and it does not create mode-specific
Words or Replacements. The resolver consumes active Template data the same way
it consumes any other framework customization.

Template sources are stable and portable:

```text
builtin:<template-id>@<version>
server:<template-id>@<version>
```

V1 ships with built-in examples:

- `standard-punctuation-de-en` is active by default and contains provider-neutral
  punctuation and numeric separator replacements for German and English.
- `developer-de-en` is opt-in and contains developer vocabulary plus technical
  spoken replacements for API, SDK, JSON, YAML, GitHub, TypeScript, PowerShell,
  Wails, Auth0, Cloudflare, dot, slash, underscore, dash, arrow, and equals.

The native Template Catalog API is:

| Operation | Meaning |
| --- | --- |
| `ListTemplates(ctx)` | Lists built-in and server-side Template metadata. |
| `ResolveTemplatePack(id, version)` | Returns the versioned Pack for import or runtime use. |
| `ActiveTemplateIDs` | Selects which Template data is included in Dictation, Assist, and Voice Agent resolve contexts. |

Device and Server hosts may expose the same Templates. A connected SpeechKit
Server can therefore act as the live development catalog; once a server Template
has proven useful, the same Pack shape can be vendored as a built-in Framework
Template. Marketplace distribution is intentionally not implemented in V1, but
the source/version shape is marketplace-ready.

## Resolution Model

`scope` and `source` are separate concepts:

```text
scope  = where the customization applies
source = where the customization came from
```

The resolver receives an explicit ordered scope list in `customize.Context`.
Hosts can override this order without creating new store types. SpeechKit uses
these defaults:

```text
Device: builtin < app < install < user < session
Server: builtin < app < install < org < user < session
```

The active set is resolved for a context:

```text
Context = { scope_order, active_template_ids, language, mode, stage, persona, tags }
```

Higher layers override lower layers by identity:

- Words: `term + language`
- Replacements: `match + kind + language`

Mode, stage, persona, and tag filters select the final active set. This keeps,
for example, an Assist-only command from affecting Dictation.

## Central Customization Service

The public contract lives in `pkg/speechkit/customize`; the internal runtime
implementation lives in `internal/customize`. Runtime consumers go through the
central Customization Service instead of calling store facets ad hoc.

| Responsibility | Contract |
| --- | --- |
| Resolver | Return active Words and Replacements for a context. |
| Biaser | Render Words into provider-specific hints. |
| Applier | Apply Replacements by stage and priority. |
| Service | Return active Words, active Replacements, prompt hints, keyterms, Voice Agent hints, and a compiled applier for one context. |

The service resolves, compiles, and caches the result per context. Cache keys
include scope order, mode, language, stage, tags, persona, and store version or
updated timestamp. Writes invalidate affected scope keys.

## Pipeline Contract

Per-mode wiring:

- Dictation: Words bias STT before transcription; SpeechKit applies `post_stt`
  substitutions and synonyms after STT. Dictation remains STT-only and never
  calls LLM utilities.
- Assist: Words bias STT; substitutions/synonyms clean transcripts before the
  existing shortcuts/LLM path. `command` Replacements are typed and storable,
  while the runtime command resolver remains a separate implementation detail.
- Voice Agent: Words bias Live recognition and system context. Output
  replacements wait for a streaming-safe follow-up contract.

The key property: a mode does not own customization logic. It consumes the same
Customization Service with a different context.

## Provider Biasing

A Word is provider-neutral. The Biaser renders active Words into native provider
options.

| Provider family | Biasing mechanism |
| --- | --- |
| whisper.cpp, OpenAI-compatible, VPS, Ollama | Prompt text through `TranscribeOpts.Prompt`. |
| Deepgram | Keyterm/keyword options. |
| Google Cloud STT | `speechContexts.phrases` with boost where available. |
| AssemblyAI | `word_boost` and boost-level options. |
| HuggingFace | Prompt where the selected model supports it. |
| Gemini Live / Voice Agent | Recognition/system prompt hint. |

Weights map to provider-native boost scales where possible and are ignored
explicitly where no provider equivalent exists.

## Replacement Semantics

- Stages are explicit: `pre_recognition_cleanup`, `post_stt`, `post_llm`, and
  `output`.
- Ordering is deterministic: descending priority, then stable ID.
- Word and Replacement matching share one normalization pass: lowercase,
  accent stripping, whitespace collapse, and punctuation handling.
- Literal and phrase rules compile into one automaton per resolved context.
- Regex rules are precompiled and grouped.
- Snippet and template expansion have max-iteration loop guards and do not
  re-apply the rule that produced the current output.

## Storage, APIs, And UI

Store interfaces:

- `WordStore`
- `ReplacementStore`
- collection stores for Lexicons and Rulesets
- attachment stores for scope/mode/persona/tag bindings

The stores are scope-aware and shaped after the existing persistence style.
SQLite and Postgres migrations are additive. Replace/import operations are
source-aware and only replace rows for the selected `scope + source + language`.

Server Target endpoints:

| Endpoint | Read | Write | Notes |
| --- | --- | --- | --- |
| `/v1/words` | authenticated | admin | Word list and replace operations with optional `scope`, `scope_key`, and `source`. |
| `/v1/replacements` | authenticated | admin | Replacement list and replace operations with optional `scope`, `scope_key`, and `source`. |
| `/v1/lexicons` | authenticated | admin | Named Word collections. |
| `/v1/rulesets` | authenticated | admin | Named Replacement collections. |

Device Target routes follow the existing local route convention and power the
Settings UI. The Device UI writes user settings by default, while APIs and
resolver support global, app, install, org, workspace, user, and session scopes.

## Migration Policy

The old dictionary model decomposes cleanly:

```text
DictionaryRow{Spoken, Canonical}
  -> Word{term: Canonical}
  -> Replacement{kind: substitution, match: Spoken, output: Canonical}
```

Migration phases:

1. Add schema for Words, Replacements, Lexicons, Rulesets, and attachments.
2. Split existing dictionary rows into Word plus substitution Replacement.
3. Move UI, public docs, and new APIs to Words and Replacements.
4. Keep any dictionary-shaped route or client method only as a temporary
   migration adapter while consumers move. It must not grow new behavior.
5. Keep `internal/shortcuts` initially; re-derive it from command Replacements
   only after the new path is tested.

Usage counts are recorded on the matched Word/Replacement IDs, not on a legacy
dictionary row.

## Test Standard

Required coverage:

- public validation for invalid kind/stage/mode, empty terms, invalid matches,
  priority ordering, and pack round-trips
- resolver precedence for `builtin < app < install < org < user < session`,
  host override behavior, mode/stage/tag/language filtering, and disabled rules
- applier determinism, word-boundary behavior, synonym normalization, loop
  guards, disabled rules, and usage matches
- store migration for empty DBs, existing dictionary rows, duplicate canonical
  terms, disabled rows, scoped rows, and language normalization
- API routes for global/admin scopes and user/settings authoring
- pipeline tests proving Dictation, Assist, and Voice Agent consume the central
  service with different contexts
- frontend tests for the Customization nav/page, Word CRUD, Replacement CRUD for
  substitution/synonym, effective customization display, import rejection, and
  export state

Regression requirement: an Assist-only Replacement must never affect Dictation.

## Rollout

The implementation was introduced in the v0.44 beta line and remains active in
the v0.46 public framework surface.

| Phase | Outcome |
| --- | --- |
| Foundation | Kernel primitives, central service, resolver/biaser/applier, scoped storage, source-aware imports, dictionary migration. |
| Reach | Provider biasing gaps closed and customization stage wired into all three modes. |
| Productization | Unified settings surface, Customization Pack import/export, API/docs/release gates. |

## Open Questions

- Should `Word.sounds_like` auto-create derived substitution Replacements or
  remain biasing-only?
- What policy promotes learned Words/Replacements from telemetry, and what
  consent/audit controls does that require?
- Which provider-native biasing adapters should be promoted next?
- When should temporary dictionary-shaped adapters be removed from public
  surfaces after downstream consumers have migrated?
