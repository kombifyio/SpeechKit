# Technical Getting Started

This guide is for engineers setting up SpeechKit directly.

## Install the SpeechKit Server

Stable:

```sh
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

v0.30 Preview:

```sh
curl -fsSL https://speechkit.cc/install-server.sh | sh -s -- --channel preview
```

The installer writes:

- `docker-compose.yml`
- `config.toml`
- `.env`
- a persistent `data/` directory

Default install directory: `/opt/speechkit`.

The default Docker Compose port binding is `127.0.0.1:8080:8080`. For public
servers, put SpeechKit behind a TLS reverse proxy and run the installer with
`--public-bind` only when the host should listen on all interfaces.

## Verify

```sh
cd /opt/speechkit
docker compose ps
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

Authenticated API calls use the generated `SPEECHKIT_SERVER_TOKEN` from
`/opt/speechkit/.env`.

## Use the API

OpenAPI:

```text
https://speechkit.cc/api/openapi.v1.yaml
```

Voice Agent AsyncAPI:

```text
https://speechkit.cc/api/asyncapi.v1.yaml
```

Core endpoints:

- `POST /v1/dictation/transcribe`
- `POST /v1/assist/process`
- `POST /v1/voiceagent/sessions`
- `GET /v1/catalog/profiles`
- `GET /v1/config`
- `POST /v1/tts/synthesize`

## Use Go

```sh
go get github.com/kombifyio/SpeechKit/pkg/speechkit
```

Use `pkg/speechkit/client` when you want to call a running SpeechKit Server.

## Use MCP

```json
{
  "mcpServers": {
    "speechkit": {
      "command": "speechkit-mcp",
      "args": ["--mode=docs,test"]
    }
  }
}
```
