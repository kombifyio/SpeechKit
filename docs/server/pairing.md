# Homelab pairing (`speechkit.pairing.v1`)

A self-hosted SpeechKit server hands a client a URL and a bearer token without
putting that token on the LAN. DNS-SD `_speechkit._tcp` announces only `url`,
`modes`, `version`, the stable instance `id` and, while LAN device pairing is
open, `pair=1`. Both the server responder and `--discovery-only` announce the
pairing flag from the live admission gate. The sidecar must share the server's
state directory: it reads a non-secret availability file refreshed every second,
and omits the flag if the file is missing, unreadable or over five seconds old.
Both responders evaluate availability on each answer; clients may cache earlier
DNS answers until their TTL expires.

## Payload

```json
{
  "v": "speechkit.pairing.v1",
  "server_url": "http://192.168.1.20:8080",
  "auth": "bearer",
  "token": "sk-server-…",
  "name": "wohnzimmer"
}
```

| Field | Rule |
|---|---|
| `v` | Must be `speechkit.pairing.v1` |
| `server_url` | Dial URL. Missing URL is not a session. |
| `auth` | `bearer` only in v1 |
| `token` | Server API bearer. Missing token is not a session. |
| `name` | Optional instance label |

A client that consumes this payload stores `self_host`. It must not infer
the cloud connection mode or the tester origin from a partial or unknown
payload.

v1 tokens are shown once when generated. Per-device revocable tokens are
`speechkit.pairing.v2` below.

## Where it appears

Saving server settings with managed bearer generation returns the payload as
`pairing` and an SVG QR as `pairing_qr_svg`. `/setup` renders both so a phone
can take the URL without typing it.

Android Settings pastes the JSON through `PairingRecords.consume`.

## What stays off the wire

`_speechkit._tcp` TXT that includes `token`, `auth`, `password`, `secret`, or
`bearer` is rejected whole. Pairing material travels out of band: this JSON,
the QR that encodes it, or a typed token after the user picked a discovered URL.

## LAN device pairing (`speechkit.pairing.v2`)

v1 hands every client the one shared server bearer, which cannot be revoked
for a single device. v2 gives each LAN device (a room satellite, a phone, a
second desktop) its own revocable token after a physical confirmation, and
the shared bearer never leaves the server.

### Flow

1. **Find.** The device browses `_speechkit._tcp` and takes the `url` of an
   instance whose `modes` include what it needs (a voice satellite needs
   `voiceagent`).
   While device pairing is open the announcement also carries `pair=1`, a flag
   and never a credential.
2. **Ask.** `POST /v1/pairing/device-requests` without credentials, with
   `Content-Type: application/json` and no `Origin` header (device requests
   only; browser-origin requests are refused with `400 invalid_request`):

   ```json
   {"device_name": "Living Room Satellite 1A2B", "device_kind": "satellite", "device_id": "sat-001122331a2b"}
   ```

   `201` answers `{request_id, poll_secret, code, expires_in, poll_interval}`.
   `code` is six digits; the device **shows** it (display or app screen).
   `poll_secret` stays on the device.
3. **Confirm.** The operator compares the code on the device with the one the
   server lists and approves it: `speechkit-server pairing approve 123456`
   (or `123-456`), or refuses it with `pairing deny <code>`. The operator API is
   `POST /v1/pairing/device-requests/approve|deny {code}`. Seeing the code
   on the device in front of you is the physical confirmation; approval needs
   the operator (admin session or the server bearer).
4. **Collect.** The device polls `GET /v1/pairing/device-requests/{id}` with
   header `X-Pairing-Secret: <poll_secret>`: `pending` (with the code and the
   seconds left), `denied`, or once `approved` with
   `{device_id, device_name, token_type: "bearer", token}`. The token is
   handed out exactly once; the next poll answers `404 {"status":"expired"}`.
5. **Use.** The device sends `Authorization: Bearer skd_…` to the
   device-facing routes like any other client.

Requests expire after 10 minutes and live only in memory.

### Rules

| Rule | Behaviour |
|---|---|
| LAN only | A direct peer must be private (RFC 1918), ULA (RFC 4193) or link-local. A loopback peer is admitted only when `trusted_proxy_cidrs` explicitly lists it and it supplies a nonempty `Forwarded` or `X-Forwarded-For` chain containing LAN addresses only. Loopback is never implicitly trusted for pairing; a lone `X-Real-IP` is insufficient. Every address in any supplied forwarding header (`Forwarded`, `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`) must be LAN and the peer explicitly trusted. Otherwise `403 not_local_network`. |
| Off | `[server.pairing] device_requests = false` answers `403 device_pairing_disabled`. |
| Rate | At most 16 open requests, 3 open and 6 new per source address per 10 minutes; beyond that `429`. |
| Token | `skd_` plus 32 random bytes (base64url). The server stores only its SHA-256, with device id, name, kind, hardware id, source address, creation and last use, in `paired-devices.json` next to the server settings (mode 0600; `[server.pairing] devices_path` overrides). |
| Reach | A device token authenticates only `/v1/voiceagent/*`, `/v1/dictation/*`, `/v1/assist/*`, `/v1/tts/*` and `/v1/wakeword/*` (and their `/api` alias) as a normal client (`Source=device_token`, no role, `UserID=device:<id>`). Settings, personas, admin, transcripts and the pairing registry refuse it. |
| Re-pair | `device_id` is self-asserted, not proof of hardware identity. Approving a matching id authorizes replacement; collecting the new token revokes the previous token. Pending operator listings include `replaces_device_id`, and the CLI shows the replacement in `list` and before sending `approve`. |
| Revoke | `speechkit-server pairing revoke dev_…` or `DELETE /v1/pairing/devices/{id}`. The in-memory entry goes first, so the device's next call is `401` even if the file write fails. Revocation does not end an established voice session, dictation stream or WebSocket. A failed write can restore the old credential after restart; the API reports that failure. |

### Default

`device_requests` unset resolves to **on** only for `auth_mode = "bearer"`
on a server that is not marked as public: no hosted deployment defaults and
no `public_base_url` / `public_url` outside the LAN (a private address or a
local name such as `*.local`, `*.lan`, `*.home.arpa`, `*.fritz.box`). That is
the homelab shape, where the alternative is copying the shared bearer onto
every device. Edge-signed, OIDC and hosted deployments stay off unless the
operator sets `device_requests = true`; even then the LAN-only rule holds.

`public_url` influences this default even when it is used only to generate
WebSocket URLs: setting a public hostname closes default pairing regardless of
actual internet reach. Empty URLs and dotless names (including `localhost`)
count as local. The published installer's default loopback bind therefore has
pairing access closed until a same-host proxy is explicitly listed in
`trusted_proxy_cidrs` and forwards the client address in `Forwarded` or
`X-Forwarded-For`. A same-host reverse proxy or tunnel without that configuration
cannot admit pairing requests. A listed proxy must overwrite untrusted client
forwarding headers and report the actual client address.

### Operator commands

```bash
speechkit-server pairing list               # open requests (code, device, source) and paired devices
speechkit-server pairing approve 123456
speechkit-server pairing deny 123456
speechkit-server pairing revoke dev_AbC123
```

The CLI reads the same config and settings file as the server and
authenticates with the server bearer (`bearer_token_env`), so run it where the
server runs (`docker exec <container> speechkit-server pairing list`).
`GET /v1/pairing/device-requests` and `GET /v1/pairing/devices` are the same
lists over HTTP for the operator.

The CLI defaults to loopback HTTP. `--server` requires HTTPS for any host other
than a loopback IP or `localhost`, and redirects are refused so the shared bearer
cannot be forwarded to an insecure destination. Pairing codes are never written
to server logs; the operator gets them through the listing or from the device.
