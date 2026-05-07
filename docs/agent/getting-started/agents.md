# Agent / Prompt Getting Started

This guide is written for Codex, Claude, browser MCP tools, fetch tools, and
other coding agents.

## Best First Prompt

```text
Hi Codex, go to speechkit.cc and install the SpeechKit Server on this server.
```

For v0.30 Preview:

```text
Hi Codex, read speechkit.cc/llms.txt, then install the v0.30 Preview SpeechKit Server with Docker Compose.
```

For framework integration:

```text
Hi Codex, add SpeechKit as a Go framework dependency and use the documented Dictation, Assist, and Voice Agent contracts.
```

For MCP:

```text
Hi Codex, configure `speechkit-mcp` in docs mode and verify the SpeechKit API before writing integration code.
```

## Agent Instructions

1. Fetch `https://speechkit.cc/llms.txt`.
2. Follow links to Markdown files before parsing the SPA.
3. Use `https://speechkit.cc/api/openapi.v1.yaml` for API shape.
4. Use `https://speechkit.cc/api/asyncapi.v1.yaml` for Voice Agent WebSocket shape.
5. Use `https://speechkit.cc/install-server.sh` for server installation.
6. Use `speechkit_install_plan` before changing a host.
7. Use `speechkit_self_check_plan` after installation and before writing integration code.
8. Use `speechkit_scaffold_integration` for a starter app before inventing boilerplate.
9. Use `--channel preview` only when the user asks for v0.30 Preview.

## Go Agent Harness

Use `pkg/speechkit/agentkit` for Go hosts that need Voice Agent sessions with
registered tools, lifecycle hooks, and session memory. Keep host tools
idempotent unless they are explicitly user-confirmed actions.

For a browser dictation starter, use `speechkitctl init browser-dictation-react`
or the read-only MCP `speechkit_scaffold_integration` tool. Review the generated
files before applying them to a repository.

## Boundaries

- Do not create a `v0.30.0` release tag.
- Do not change GHCR `latest` for preview testing.
- Do not commit secrets.
- Do not expose `auth_mode = "none"` on a public host.
- Keep Dictation, Assist, and Voice Agent behavior separate.
