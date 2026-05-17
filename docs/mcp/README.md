# SpeechKit MCP Server

`speechkit-mcp` exposes SpeechKit to coding agents as three first-class MCP
modes:

- `docs`: searchable API and architecture documentation, no server required.
- `management`: authenticated HTTP wrapper for a running SpeechKit server.
- `test`: static validation helpers for configs, payloads, and compatibility.

Example local stdio config:

```json
{
  "mcpServers": {
    "speechkit": {
      "command": "speechkit-mcp",
      "args": ["--mode=docs,management,test"],
      "env": {
        "SPEECHKIT_SERVER_URL": "http://localhost:8080",
        "SPEECHKIT_TOKEN": "test"
      }
    }
  }
}
```

Management writes require the server to authenticate the caller as admin, for
example with `server.bearer_role = "admin"` in trusted single-operator setups.

HTTP transport is local-first:

```bash
speechkit-mcp --transport=http --addr=127.0.0.1:8090 --mode=docs,test
```

When HTTP transport exposes `management` on a non-loopback address, set
`--mcp-token` or `SPEECHKIT_MCP_TOKEN` in addition to the SpeechKit server
token. `speechkit_transcribe(audio_path=...)` is intentionally disabled over
HTTP transport because remote MCP callers cannot safely reference local files.

The MCP server does not run Voice Agent WebSocket sessions. It exposes docs,
management operations, and validation tools over the public `/v1/*` API.

## Agent prompt starter

Use the public Markdown entrypoints before browsing the SPA:

```text
Hi Codex, configure `speechkit-mcp` in docs mode and verify the SpeechKit API before writing integration code.
```

Public docs:

- `https://speechkit.cc/llms.txt`
- `https://speechkit.cc/mcp/speechkit-mcp.md`
- `https://speechkit.cc/api/openapi.v1.yaml`
- `https://speechkit.cc/api/asyncapi.v1.yaml`
- `https://speechkit.cc/getting-started/agents/voice-game-moderator.md`
- `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`
- `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`
- `https://speechkit.cc/install-server/docker-compose.example.yml`
