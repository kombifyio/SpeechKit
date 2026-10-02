# `docs/api` - Local Control API

[`openapi.v1.yaml`](./openapi.v1.yaml) ("SpeechKit Local Control API") is the
control plane served by the **desktop host** (`app/cmd/speechkit`, Wails app) on a
runtime-assigned loopback port. It covers modes, per-mode settings, provider
profiles, server-connection/LAN pairing, recording sessions and the other
`/api/v1` desktop-host routes. It is for local tools driving the desktop app.

It is **not** the SpeechKit Server API. The headless server (`app/cmd/speechkit-server`,
dictation/assist/voice-agent/TTS transport for remote clients) is described by
[`docs/server/openapi.v1.yaml`](../server/openapi.v1.yaml) plus the AsyncAPI
files next to it; see [`docs/server/README.md`](../server/README.md). The
TypeScript client `@kombifyio/speechkit-client` is generated from that server
spec, not from this one.

| Spec | Served by | Audience |
| --- | --- | --- |
| `docs/api/openapi.v1.yaml` | Desktop host, loopback | Local tools controlling the desktop app |
| `docs/server/openapi.v1.yaml` | `speechkit-server` | Remote/browser/mobile clients, SDK consumers |

The files keep their paths because many references and the OSS export manifest
point at them.
