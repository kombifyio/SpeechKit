# SpeechKit Product MCP Boundary

SpeechKit has two MCP entry modes:

- SaaS/Gateway: `https://api.kombify.io/v1/mcp/public/speechkit`
- Self-host: local `stdio` or loopback HTTP endpoint beside `speechkit-server`

Both modes expose management and diagnostics only. Media capture/playback,
provider credentials, Home Assistant credentials, device pairing material, and
local rule contents are never returned by MCP tools.

## Tools

| Tool | Scope |
|---|---|
| `speechkit_readiness` | Runtime readiness, deployment mode, and boundary checks. |
| `speechkit_list_devices` / `speechkit_get_device` | Device-agent inventory, room mapping, wake state, and health. |
| `speechkit_list_sessions` / `speechkit_get_session` | VoiceSurface lifecycle state for Dictation, Assist, and Voice Agent sessions. |
| `speechkit_list_personas` | Companion personas and routing targets. |
| `speechkit_install_plan` | Self-host, hosted, or hybrid install steps. |
| `speechkit_scaffold_device_agent` | Local device-agent scaffold filenames and commands. |
| `speechkit_diagnostics` | Boundary, Gateway, Home Assistant bridge, and device-agent diagnostics. |

## Auth boundary

SaaS mode uses an Auth0 MCP resource token for
`https://api.kombify.io/v1/mcp/public/speechkit` with scope `mcp:speechkit`.
Each tool is still gated by SpeechKit feature entitlements, for example
`speechkit.device.fleet`, `speechkit.voiceagent.live`,
`speechkit.server.hosted`, and `speechkit.homeassistant.bridge`.

Those identities and entitlements do not authorize local smart-home actions.
The G0 device bridge has its own per-device pairing credential, stable server
identity, pairing epoch, authoritative room, direct-source CIDR restriction,
and immutable local light rules. A local rule is not a Workbench approval,
Cloud standing grant, MCP capability, or federation route.

The local `speechkit-server` alone holds the Home Assistant origin/token and
TTS configuration. A device agent holds only its local SpeechKit origin and
pairing material. Home Assistant remains the sole smart-home semantic
authority.

## Self-host contract

Recommended local MCP transports:

```text
stdio:    speechkit-server mcp --transport stdio
loopback: speechkit-server mcp --transport http --bind 127.0.0.1:8789
```

Recommended safe operations:

- Report server readiness and configured deployment mode.
- List known device-agent IDs, rooms, health, and wake status.
- List VoiceSurface session IDs, modes, lifecycle state, and timestamps.
- Return install/scaffold plans with placeholder file paths only.
- Return diagnostics that say whether local dependencies are configured.

Forbidden operations:

- Sending capture frames, playback streams, wake buffers, or recordings over
  MCP.
- Returning provider credentials, Home Assistant credentials, pairing
  material, or local command phrases.
- Creating, modifying, approving, or executing local smart-home rules through
  MCP.
- Treating a Gateway entitlement, Workbench approval, Cloud grant, or
  federation capability as authorization for a device-agent request.
- Routing the local device-agent action path through the general Assist/LLM
  pipeline or through cloud federation.

The independently paired device bridge exposes only the four exact local POST
routes `/v1/device-agent/register`, `/v1/device-agent/events`,
`/v1/device-agent/assist`, and `/v1/device-agent/tts`. They are not MCP tools,
are not published through Gateway or federation, and are intentionally absent
from the `/api/v1` compatibility alias. See
[`device-agent.md`](device-agent.md) for the complete G0 contract.

Bounded validation target:

```powershell
$env:GOWORK='off'
go test ./pkg/speechkit/deviceagent ./cmd/speechkit-device-agent ./internal/server/deviceagent/... -count=1
```
