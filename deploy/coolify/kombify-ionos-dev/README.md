# kombify-ionos-dev Coolify deploy

This is the internal dev deployment for `https://speechkit.kombify.dev`.
It is intentionally separate from the public OSS Docker compose files.

## Resource shape

- Coolify Cloud server: `kombify-ionos-dev`
- Public host: `speechkit.kombify.dev`
- Compose file: `deploy/coolify/kombify-ionos-dev/docker-compose.yml`
- Runtime config: `deploy/coolify/kombify-ionos-dev/config.toml`
- API auth: SpeechKit bearer token, optionally edge-HMAC
- Browser auth: TinyAuth with Pocket ID

## Required Coolify env vars

Set these on the Coolify service:

```text
SPEECHKIT_SERVER_IMAGE=ghcr.io/kombiverselabs/speechkit-server:git-<sha>
SPEECHKIT_SERVER_TOKEN=<shared dev bearer token>
SPEECHKIT_PUBLIC_URL=https://speechkit.kombify.dev
SPEECHKIT_PUBLIC_HOST=speechkit.kombify.dev
SPEECHKIT_TINYAUTH_MIDDLEWARE=tinyauth@docker
SPEECHKIT_TINYAUTH_OAUTH_GROUPS=admins
```

Optional provider env vars:

```text
EDGE_AUTH_SECRET=
GOOGLE_AI_API_KEY=
OPENAI_API_KEY=
GROQ_API_KEY=
OPENROUTER_API_KEY=
HF_TOKEN=
```

## Auth routing

Do not put TinyAuth in front of the whole host as a single catch-all API
middleware. Desktop/API clients call `/v1/*` with `Authorization: Bearer
<SPEECHKIT_SERVER_TOKEN>` and must receive JSON errors from SpeechKit, not
TinyAuth redirects or HTML login pages.

The compose file therefore uses two HTTPS routers:

- `speechkit-dev-api`: `/healthz`, `/readyz`, `/v1/dictation`, `/v1/assist`,
  `/v1/voiceagent`, and `/api/v1/*` mode aliases. No TinyAuth middleware.
- `speechkit-dev-ui`: same host fallback for browser surfaces. TinyAuth
  middleware is attached here.

TinyAuth/Pocket ID still owns browser login. SpeechKit owns API authorization.
TinyAuth alone does not create SpeechKit `X-Edge-Auth-Hmac` headers; only use
`EDGE_AUTH_SECRET` when an edge signer is actually deployed.

## Pocket ID / TinyAuth notes

Pocket ID must have a TinyAuth OIDC client with callback:

```text
https://<tinyauth-host>/api/oauth/callback/pocketid
```

TinyAuth needs the Pocket ID provider env vars on its own service. The
SpeechKit service only carries app labels for the `speechkit.kombify.dev`
resource and the required Pocket ID group.

## Deploy

With a configured Coolify service UUID:

```powershell
$env:COOLIFY_API_BASE = "https://app.coolify.io/api/v1"
$env:COOLIFY_API_TOKEN = "<token>"
$env:COOLIFY_SERVICE_UUID = "<speechkit service uuid>"
powershell -ExecutionPolicy Bypass -File scripts/deploy-coolify-dev.ps1
```

The script updates `docker_compose_raw` and queues a service start/deploy.
It does not print secret values.
