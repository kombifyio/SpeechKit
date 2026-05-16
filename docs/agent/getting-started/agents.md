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

For a live-validatable native Android memo app:

```text
Hi Codex, read https://speechkit.cc/llms.txt and create a real native Android memo app powered by SpeechKit. The app must be an Android project with Gradle settings, app/build.gradle or app/build.gradle.kts, app/src/main/AndroidManifest.xml, and Kotlin or Java source code; a browser harness or web-only mock is not acceptable. The app should record a memo, send the audio to /v1/dictation/transcribe, save the transcript locally, and offer an Improve button that sends the memo text to /v1/assist/process for cleanup, summary, or rewrite. Set up a fresh local SpeechKit Server as part of the generated Docker Compose verification stack, add a small settings screen or config repository for SPEECHKIT_SERVER_URL and token, never hardcode secrets, handle offline/provider errors clearly, include Android tests plus a Gradle build verification, and add a Gradle task named verifySpeechKitLive that proves Dictation and Assist against the freshly generated local SpeechKit Server through the Android settings/config path. Do not connect to an existing or preconfigured SpeechKit Server. The live verification must write speechkit-one-shot-functional-result.json with status=pass, manifest_file=speechkit-one-shot-manifest.json, app_kind=android, app_transport=android, server_url_source=settings_screen or android_config, app_url matching the manifest localhost URL, and passing dictation and assist results; each checked mode result must include status=pass, transcript/output text, and checked_via_app=true. Also write speechkit-one-shot-manifest.json with docker_compose_file, localhost_urls, speechkit_server_url, speechkit_server_token_env, and android_project_dir.
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
10. After the server is healthy, complete `/v1/server/settings` setup and enable `admin_auth.enabled=true` with a generated admin username/password unless the host has an authenticated edge that already protects `/setup`.

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
