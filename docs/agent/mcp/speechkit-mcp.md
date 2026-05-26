# SpeechKit MCP

`speechkit-mcp` exposes SpeechKit to coding agents.

Modes:

- `docs`: embedded docs, OpenAPI, AsyncAPI, architecture, examples.
- `test`: request, response, config, and compatibility validation helpers.
- `management`: authenticated wrapper for a running SpeechKit Server.

Useful agent planning tools:

- `speechkit_install_plan`: returns stable install steps without mutating the host.
- `speechkit_self_check_plan`: returns the health, readiness, config, catalog, OpenAPI, and AsyncAPI probes an agent should run.
- `speechkit_scaffold_templates`: lists available starter integration templates.
- `speechkit_scaffold_integration`: renders starter integration files in memory; it does not write to the host.

Contracts:

- OpenAPI: `https://speechkit.cc/api/openapi.v1.yaml`
- Voice Agent AsyncAPI: `https://speechkit.cc/api/asyncapi.v1.yaml`
- One-shot manifest schema: `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`
- One-shot functional result schema: `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`

Static prompt and install artifacts indexed by docs mode:

- `https://speechkit.cc/getting-started/agents/tri-mode-web-demo.md`
- `https://speechkit.cc/getting-started/agents/voice-game-moderator.md`
- `https://speechkit.cc/getting-started/agents/android-memo-app.md`
- `https://speechkit.cc/getting-started/agents/go-framework-integration.md`
- `https://speechkit.cc/install-server/docker-compose.example.yml`
- `https://speechkit.cc/install-server/config.browser.example.toml`

Docs-only config:

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

Management config:

```json
{
  "mcpServers": {
    "speechkit": {
      "command": "speechkit-mcp",
      "args": ["--mode=docs,management,test"],
      "env": {
        "SPEECHKIT_SERVER_URL": "http://localhost:8080",
        "SPEECHKIT_TOKEN": "replace-with-generated-token"
      }
    }
  }
}
```

HTTP transport:

```sh
speechkit-mcp --transport=http --addr=127.0.0.1:8090 --mode=docs,test
```

For Go agent harnesses, pair `speechkit-mcp` with `pkg/speechkit/agentkit`:
use MCP to inspect contracts and `agentkit` to embed Voice Agent sessions,
tool registration, lifecycle hooks, and session memory.

For UI or browser starters, ask `speechkit-mcp` for
`speechkit_scaffold_integration` or run `speechkitctl init
browser-dictation-react --output ./speechkit-demo`.

Security notes:

- Management mode requires SpeechKit Server auth.
- HTTP management on non-loopback addresses also requires `--mcp-token` or
  `SPEECHKIT_MCP_TOKEN`.
- Remote HTTP MCP does not expose local `audio_path` transcription.
