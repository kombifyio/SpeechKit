# SpeechKit Local Device-Agent Bridge (G0)

`speechkit-device-agent` is the credential-minimal LAN-side host for a paired
microphone/speaker satellite. It owns capture, playback, wake-word state, and
device health. The local `speechkit-server` owns the Home Assistant origin and
token, the TTS provider, the device-to-room binding, the command allowlist, and
the durable action ledger.

The device is configured with only:

- the local SpeechKit server origin;
- its independent pairing token;
- the expected stable SpeechKit server instance ID;
- the expected pairing ID for the current credential epoch; and
- its device, room, audio-device, wake-word, and readiness descriptors.

It never receives a Home Assistant URL or token. Rotating a device token also
requires a new, never-recycled pairing ID so old request claims cannot cross a
credential epoch.

## Local-only protocol

Protocol `speechkit.device_agent.v1` exposes exactly four device routes:

| Method | Route | Purpose |
|---|---|---|
| `POST` | `/v1/device-agent/register` | Validate the bound device and report server-pinned Home Assistant/TTS readiness. |
| `POST` | `/v1/device-agent/events` | Publish bounded wake, capture, Assist-result, and TTS lifecycle metadata. |
| `POST` | `/v1/device-agent/assist` | Submit one explicitly named, time-bounded local light rule. |
| `POST` | `/v1/device-agent/tts` | Look up a completed Assist claim and synthesize its persisted Home Assistant response for local playback. |

These routes use a per-device bearer token and the
`X-SpeechKit-Device-ID` header. The server also checks the request's direct
remote address against that device's configured local CIDRs; forwarded headers
do not extend the trust boundary. Every response pins
`X-SpeechKit-Server-Instance-ID`, and registration must return the expected
server instance and pairing epoch.

Plain HTTP is accepted only when the device connects to a literal loopback
address or `localhost`. A LAN IP or DNS name requires HTTPS. The server applies
the same local-origin and transport restriction to Home Assistant: HTTP is
loopback-only, while every off-loopback Home Assistant origin requires HTTPS.
Redirects and proxy traversal are disabled for both credential-bearing clients.

The general `/api/v1` compatibility alias deliberately does not expose these
routes. They are also absent from Gateway, federation, MCP, and the general
Assist/LLM pipeline.

## Fail-closed command policy

Every paired device needs at least one immutable, server-owned
`local_rules` entry. A request names one `command_id`/`rule_id` and must match
the configured device, authoritative room, normalized exact phrase, locale,
and active RFC3339 time window. There is no rule search, fuzzy matching, NLP,
intent inference, or fallback.

G0 accepts only reversible Tier-1 actions:

- `turn_on` for one explicit `light.*` entity; or
- `turn_off` for one explicit `light.*` entity.

The rule window must be positive and no longer than 31 days. These static local
rules are a narrow G0 allowlist. They are explicitly **not** a Workbench
approval, Cloud standing grant, delegated identity, or federated capability.

Home Assistant is the sole smart-home semantic authority. SpeechKit sends the
server-owned canonical rule phrase to Home Assistant's Conversation API. It
reports `action_executed=yes` only after Home Assistant returns exactly the
authorized entity as a successful target, returns no failed target, and a REST
readback of `/api/states/<entity_id>` confirms the expected `on` or `off`
state. A general LLM never interprets, retries, or rephrases this command.

## At-most-once behavior

`POST /v1/device-agent/assist` requires a fresh UUIDv7 `request_id`. Before any
Home Assistant dispatch, the server durably commits a keyed request claim. A
completed outcome can be replayed without dispatching again. Different command
content under an existing request ID is rejected.

This is durable **at-most-once** execution, not exactly-once execution. If the
server cannot prove the outcome after committing the claim—for example after a
transport interruption, an unexpected Home Assistant target, or failed state
readback—the claim becomes indeterminate. Replaying that request ID returns a
conflict and never dispatches again; the operator must inspect Home Assistant
state before issuing a new command with a fresh request ID.

## Claim-bound local TTS

The TTS route is not a general text-to-speech surface. Its strict request body
contains only the completed Assist claim ID and the output format:

```json
{
  "request_id": "019b2d4d-8f3b-7abc-8def-1234567890ab",
  "format": "wav"
}
```

The server combines the authenticated device's pairing epoch with
`request_id`, looks up that claim in the durable ledger, and proceeds only when
the Assist result is completed and contains persisted Home Assistant speech and
language. It supplies exactly those stored values to the local TTS provider.
Caller-provided `text`, `locale`, or any other unknown field is rejected before
provider execution. Missing, foreign-pairing, pending, or indeterminate claims
cannot produce audio.

`TTSResponse.request_id` echoes the claim ID, and the device rejects a response
whose ID does not match its request. TTS retry never repeats the Home Assistant
action because it reads the already completed claim rather than invoking
Assist again.

## Phase-A Kombify Box media ingress

The Kombify Box uses a separate finite media endpoint rather than receiving a
general SpeechKit server credential or any Home Assistant credential. This
endpoint is owned by the local device-agent implementation but is not one of
the four `speechkit.device_agent.v1` routes:

| Method | Dedicated-listener path | Purpose |
| --- | --- | --- |
| `POST` | `/v1/box-media/turn` | Submit one complete microphone turn and receive one complete local-TTS result. |

The listener accepts TLS 1.3 only. It does not offer plaintext, redirects,
proxy trust, cloud fallback, streaming, or another route. Its certificate must
identify the explicit local listen IP. The Box pins the operator-provisioned
local CA; SpeechKit does not create production certificates or copy a CA from a
cloud service.

The request is a raw `audio/L16` body in network byte order with exactly these
properties:

- `Content-Type: audio/L16; rate=16000; channels=1`;
- known, even `Content-Length` for 300 milliseconds through 15 seconds;
- `Authorization: Bearer <Box-media pairing token>` (32 through 512 bytes),
  independently provisioned and forbidden from matching the credential for the
  four device-agent G0 routes;
- `X-SpeechKit-Device-ID`, `X-SpeechKit-Pairing-ID`, and a fresh UUIDv7
  `X-SpeechKit-Request-ID`;
- `X-SpeechKit-Input-Audio-SHA256`, the lowercase SHA-256 of the exact
  transmitted body.

Only the direct TCP peer is checked against the paired device's local CIDRs.
`Forwarded`, `X-Forwarded-For`, and `X-Real-IP` are ignored. After the digest
check, the host converts L16 to PCM16LE/WAV and invokes a dependency that
explicitly attests `LocalOnly`; there is no call to the general STT router and
therefore no cloud or dynamic fallback.

Phase A contains exactly one server-owned transcript-to-`command_id` mapping.
The normalized local-STT transcript must match that mapping exactly, and the
mapping must itself match an existing G0 light rule for the same device,
pairing epoch, room, and locale. The exported host-side `ExecuteTurn` method
then reuses the durable Home Assistant claim, exact target/state readback, and
claim-bound local TTS path. The verified input-audio SHA joins the claim digest,
so replaying one request ID with different audio conflicts before another Home
Assistant dispatch.

A successful response is raw network-byte-order
`audio/L16; rate=48000; channels=1`. It includes the device, pairing, and
request IDs; stable `X-SpeechKit-Server-Instance-ID`; exact input and output
audio SHA-256 values; and `X-SpeechKit-Replayed`. The Box receives no
transcript, Home Assistant entity, Home Assistant token, TTS provider
credential, or service bearer.

`NewBoxMediaHandler` and `NewBoxMediaTLSServer` are dependency-injected and
fail closed before listening when the bridge, proven local-only STT dependency,
independent media-pairing token, single rule, explicit local IP, certificate,
key, current validity, or IP SAN is missing. The production server lifecycle is
an additional explicit opt-in under `[server.device_agent.box_media]`. It starts
and probes the concrete host-local Whisper runtime, requires one ready
local-only TTS provider, verifies the configured pinned CA digest and the
listener certificate chain, and propagates listener failure to the main server
process. It never mounts this endpoint on the general server mux.

SpeechKit still does not generate production certificates, provision the media
token, or install the CA on a Box. Those are operator actions. Source and local
TLS tests do not constitute physical microphone/playback, room, or pinned-CA
installation evidence; record that evidence separately against the exact
server and device firmware digests.

The finite request/response state machine is deliberately trigger-neutral.
Touch-to-Talk can submit a turn in v1, and a later verified MicroWakeWord model
may submit through the identical state machine. Unit fixtures and synthetic
audio prove neither physical-room wake quality nor `wakeword_local=true`;
those remain separate device evidence.

## Server configuration

The pairing token and Home Assistant token are environment-backed secrets;
their values never belong in TOML.

```toml
[tts]
enabled = true
strategy = "local-only"
format = "wav"

[assist.home_assistant]
url = "https://homeassistant.home.arpa:8123"
token_env = "SPEECHKIT_HOME_ASSISTANT_TOKEN"
agent_id = "conversation.home_assistant"
language = "de"

[server.device_agent]
enabled = true
server_instance_id = "speechkit-home-01"
claim_store_path = "/var/lib/speechkit/device-agent-claims.db"
max_request_age_sec = 600
future_skew_sec = 120
claim_retention_sec = 86400
max_claims = 10000

[[server.device_agent.devices]]
device_id = "speaker-kitchen-001"
pairing_id = "pairing-kitchen-2026-07"
room_id = "kitchen"
token_env = "SPEECHKIT_DEVICE_KITCHEN_TOKEN"
allowed_client_cidrs = ["192.168.10.42/32"]

[[server.device_agent.devices.local_rules]]
rule_id = "kitchen-light-off"
trigger_text = "turn off the kitchen light"
locale = "en-US"
action = "turn_off"
entity_id = "light.kitchen"
not_before = "2026-07-19T00:00:00Z"
expires_at = "2026-08-18T00:00:00Z"

[server.device_agent.box_media]
enabled = true
listen_addr = "192.168.10.10:8444"
certificate_file = "/etc/speechkit/box-media.crt"
private_key_file = "/etc/speechkit/box-media.key"
pinned_ca_file = "/etc/speechkit/box-media-ca.crt"
pinned_ca_sha256 = "REPLACE_WITH_64_LOWERCASE_HEX_DER_SHA256"
token_env = "SPEECHKIT_BOX_MEDIA_KITCHEN_TOKEN"
device_id = "speaker-kitchen-001"
pairing_id = "pairing-kitchen-2026-07"
room_id = "kitchen"
transcript = "turn off the kitchen light"
command_id = "kitchen-light-off"
locale = "en-US"
```

Replace the sample authorization window with a current operator-approved
window before enabling the bridge. At least one local TTS provider must also be
configured and ready. Startup rejects device-agent enablement unless
`[tts].enabled=true` and `[tts].strategy="local-only"`; cloud-first or cloud-only
speech would violate the local G0 boundary.

Box media additionally requires `[local].enabled=true`, an absolute
`[local].model_path`, and a valid local Whisper port. The server resolves
`token_env` at startup and rejects reuse of Home Assistant, general-server,
edge-HMAC, smoke, or four-route device credentials. `certificate_file`,
`private_key_file`, and `pinned_ca_file` must be distinct absolute paths. Compute
`pinned_ca_sha256` over the CA certificate's DER bytes, for example:

```bash
openssl x509 -in /etc/speechkit/box-media-ca.crt -outform DER | sha256sum
```

The configured device, pairing epoch, room, transcript, locale, and command ID
must exactly select one existing G0 rule. Removing or rotating any of those
inputs causes the listener to fail closed on the next server start.

## Device example

The fake cycle uses the same registration, event, Assist, and TTS boundaries as
a satellite integration. For a LAN server, provide the trusted local CA to the
host runtime and use HTTPS:

```powershell
go run ./cmd/speechkit-device-agent `
  --server https://speechkit.home.arpa:8443 `
  --pairing-token file:C:\ProgramData\SpeechKit\kitchen-device.token `
  --pairing-id pairing-kitchen-2026-07 `
  --server-instance-id speechkit-home-01 `
  --device-id speaker-kitchen-001 `
  --room-id kitchen `
  --command-id kitchen-light-off `
  --fake-once "turn off the kitchen light" `
  --locale en-US
```

The product MCP management boundary is documented in
[`product-mcp.md`](product-mcp.md). Home Assistant's official response and
state shapes are documented in the
[Conversation API](https://developers.home-assistant.io/docs/intent_conversation_api/)
and [REST API](https://developers.home-assistant.io/docs/api/rest/).

Bounded local verification:

```powershell
$env:GOWORK='off'
go test ./pkg/speechkit/deviceagent ./cmd/speechkit-device-agent ./internal/config ./internal/server/deviceagent/... -count=1
```
