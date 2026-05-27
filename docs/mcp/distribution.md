# MCP Distribution

Supported distribution paths:

- Self-hosted or local docs MCP: `docs,test`, no SpeechKit server credentials.
- Local MCP with server container: `docs,management`, using the same bearer
  token or edge-auth model as the HTTP API.
- Self-hosted MCP: any combination of `docs,management,test`.

Security defaults:

- stdio MCP can use local `audio_path` for transcription checks.
- HTTP MCP binds to `127.0.0.1:8090` by default.
- HTTP MCP with `management` on a non-loopback address requires
  `SPEECHKIT_MCP_TOKEN` or `--mcp-token`.
- HTTP MCP uses explicit server limits: `ReadHeaderTimeout=15s`,
  `ReadTimeout=30s`, `IdleTimeout=120s`, and `MaxHeaderBytes=1MiB`.
  `WriteTimeout` remains unset so streamable MCP responses are not cut off.
- Management tools still pass through SpeechKit server auth. Write tools need
  an admin identity where the HTTP API requires one.
- Remote HTTP MCP deployments do not expose local `audio_path` file access.

Docs and Test modes are self-contained: OpenAPI, architecture docs, MCP docs,
and examples are embedded into the binary at build time.
