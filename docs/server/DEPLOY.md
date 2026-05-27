# SpeechKit Server Deploy Guide

SpeechKit is a framework. You host the server yourself when you want a network
runtime for your own products, teams, agents, or browsers.

## Image Contract

The public server image is:

```text
ghcr.io/kombifyio/speechkit-server
```

Release tags follow the repository tags, for example
`ghcr.io/kombifyio/speechkit-server:v0.40.5`. The image:

- listens on port `8080` over plain HTTP
- expects TLS at your reverse proxy or ingress
- exposes `/healthz`, `/readyz`, and `/readyz/strict`
- reads TOML config from `$SPEECHKIT_CONFIG_PATH` or `/etc/speechkit/config.toml`
- reads secrets from environment variables named in config
- writes data and local model state under `/var/lib/speechkit`
- exposes Dictation, Assist, and Voice Agent from one process

## Local Compose

```bash
export SPEECHKIT_SERVER_TOKEN="replace-with-a-local-dev-token"
export GOOGLE_AI_API_KEY="..."  # optional Gemini Assist + Gemini Live
export HF_TOKEN="..."           # optional Hugging Face Dictation
export OPENAI_API_KEY="..."     # optional OpenAI STT/LLM/TTS

docker compose -f deploy/docker/docker-compose.oss.yml up -d

curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS -H "Authorization: Bearer $SPEECHKIT_SERVER_TOKEN" \
  http://localhost:8080/v1/deployment/status
```

The placeholder token above is for local development only. Use your own secret
manager or orchestrator secret mechanism for shared deployments.

## Build A Custom Image

```bash
docker build \
  -f deploy/docker/Dockerfile.server \
  --target speechkit-server \
  --build-arg VERSION=dev \
  -t speechkit-server:dev \
  .
```

The Dockerfile builds only the self-host server. Windows desktop artifacts are
distributed through GitHub Release assets, not through this container image.

## Installer Script

For no-clone agent setup:

```bash
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

The installer creates a Compose directory, writes a config template, creates an
`.env` file, and pulls the public server image. Use it when an agent or
operator should bootstrap a self-host server without cloning the repository.

## Agent Tooling

Pair the server with MCP when a coding agent should inspect or operate it:

```bash
go run ./cmd/speechkit-mcp --mode=docs,test
go run ./cmd/speechkit-mcp --mode=docs,management,test \
  --server http://localhost:8080 \
  --token "$SPEECHKIT_SERVER_TOKEN"
```

Docs and test modes do not require a running server. Management mode uses the
same server auth rules as the HTTP API. If you expose MCP over HTTP outside
loopback, also set `SPEECHKIT_MCP_TOKEN` or pass `--mcp-token`.

## Generic OCI Or Kubernetes

Any orchestrator can run the image with:

- one container listening on port `8080`
- persistent storage mounted at `/var/lib/speechkit`
- config mounted at `/etc/speechkit/config.toml`
- secrets injected as environment variables
- readiness probe: `GET /readyz`
- liveness probe: `GET /healthz`
- TLS termination at the edge

Set `[server].public_base_url` or `SPEECHKIT_PUBLIC_URL` when browser clients
need server-generated WebSocket URLs that point at your public origin.

## Auth

For a single-operator deployment, bearer auth is enough:

```toml
[server]
auth_mode = "bearer"
bearer_token_env = "SPEECHKIT_SERVER_TOKEN"
bearer_role = "admin"
```

For product backends, put SpeechKit behind your own BFF, gateway, or
identity-aware edge. Browser clients should not receive provider API keys.

## Voice Agent Media

The default Voice Agent transport is WebSocket. LiveKit is optional and is used
only when your deployment config and client request select it. The WebSocket
control channel remains the source for start frames, transcripts, tool calls,
errors, and session-end events.

## Operational Notes

- `SIGTERM` starts HTTP drain and ends active Voice Agent sessions cleanly.
- Voice Agent tickets are short-lived and single-use. Create a new session when
  a ticket expires.
- `/readyz` is suitable for orchestrators. `/readyz/strict` is diagnostic and
  may fail on optional provider probes.
- Provider and model changes are restart-based unless your host builds a
  separate reload path.
