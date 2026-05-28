# Agent / Prompt Getting Started

This guide is written for Codex, Claude, browser MCP tools, fetch tools, and
other coding agents.

## Best First Prompt

```text
Hi Codex, go to speechkit.cc and install the SpeechKit Server on this server.
```

For framework integration:

```text
Hi Codex, add SpeechKit as a Go framework dependency and use the documented Dictation, Assist, and Voice Agent contracts.
```

For an Assist Voice Companion:

```text
Hi Codex, read https://speechkit.cc/llms.txt and add a SpeechKit Assist Voice Companion to this Go app. Use only public component packages from github.com/kombifyio/SpeechKit/pkg/speechkit/...: companion, wakeword, assist, tts, and speechkit for events. Wire companion.NewHandsFree with TargetMode: companion.TargetAssist. The host owns microphone capture and playback; SpeechKit owns wake activation contracts, transcript request, Assist routing, optional TTS, and EventBus publication. Do not import internal/* or the Windows client.
```

For a hands-free Voice Agent companion:

```text
Hi Codex, read https://speechkit.cc/llms.txt and build a hands-free SpeechKit Voice Agent companion into this app. Use companion.TargetVoiceAgent for wake activation. Use pkg/speechkit/client when talking to a running speechkit-server, or pkg/speechkit/agentkit when embedding a Go Voice Agent harness. Keep persona/role/sequence config in the host, stream PCM 16 kHz S16LE mono into the session, and do not import internal/*.
```

For a live-validatable native Android memo app:

```text
Hi Codex, read https://speechkit.cc/llms.txt and create a real native Android memo app powered by SpeechKit. The app must be an Android project with Gradle settings, app/build.gradle or app/build.gradle.kts, app/src/main/AndroidManifest.xml, and Kotlin or Java source code; a browser harness or web-only mock is not acceptable. The app should record a memo, send the audio to /v1/dictation/transcribe, save the transcript locally, and offer an Improve button that sends the memo text to /v1/assist/process for cleanup, summary, or rewrite. Set up a fresh local SpeechKit Server as part of the generated Docker Compose verification stack, add a small settings screen or config repository for SPEECHKIT_SERVER_URL and token, never hardcode secrets, handle offline/provider errors clearly, include Android tests plus a Gradle build verification, and add a Gradle task named verifySpeechKitLive that proves Dictation and Assist against the freshly generated local SpeechKit Server through the Android settings/config path. Do not connect to an existing or preconfigured SpeechKit Server. The live verification must write speechkit-one-shot-functional-result.json with status=pass, manifest_file=speechkit-one-shot-manifest.json, app_kind=android, app_transport=android, server_url_source=settings_screen or android_config, app_url matching the manifest localhost URL, and passing modes.dictation and modes.assist results; each checked mode result must include status=pass, transcript/output text, and checked_via_app=true. Also write speechkit-one-shot-manifest.json with docker_compose_file, localhost_urls, speechkit_server_url, speechkit_server_token_env, and android_project_dir.
```

## Fetchable One-Shot Prompts

Use these static Markdown URLs when an agent needs the exact prompt text
without rendering the SPA:

- `https://speechkit.cc/getting-started/agents/tri-mode-web-demo.md`
- `https://speechkit.cc/getting-started/agents/voice-game-moderator.md`
- `https://speechkit.cc/getting-started/agents/android-memo-app.md`
- `https://speechkit.cc/getting-started/agents/go-framework-integration.md`

Each prompt writes:

- `speechkit-one-shot-manifest.json`, validated by `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`.
- `speechkit-one-shot-functional-result.json`, validated by `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`.

The functional result uses canonical `modes.voiceagent`; do not emit
`modes.voice_agent`. Optional evidence fields such as `app_url_reachable` and
`latency_seconds` are allowed.

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
9. For browser Docker Compose, use `https://speechkit.cc/install-server/docker-compose.example.yml` and set `SPEECHKIT_PUBLIC_URL` (canonical) or `SPEECHKIT_SERVER_PUBLIC_URL` (alias) to the browser-reachable server origin.
10. Use the stable install channel unless the user explicitly asks for a documented preview build.
11. For automation, prefer setting `SPEECHKIT_SERVER_TOKEN` before first start. Otherwise complete `/v1/server/settings` setup and enable `admin_auth.enabled=true` with a generated admin username/password unless the host has an authenticated edge that already protects `/setup`.

## Go Agent Harness

For hands-free experiences, start with `docs/voice-companion.md` and
`examples/embed-companion`. Hands-Free is not a fourth mode: use
`companion.TargetAssist` for Assist Voice Companion, `companion.TargetVoiceAgent`
for continuous dialogue, and `companion.TargetDictationUIAssisted` only when a
UI owns the dictation commit.

Use `pkg/speechkit/agentkit` for Go hosts that need Voice Agent sessions with
registered tools, lifecycle hooks, and session memory. Keep host tools
idempotent unless they are explicitly user-confirmed actions.

For a browser dictation starter, use `speechkitctl init browser-dictation-react`
or the read-only MCP `speechkit_scaffold_integration` tool. Review the generated
files before applying them to a repository.

## Boundaries

- Do not create release tags unless the repository release gate has passed.
- Do not change GHCR `latest` for preview testing.
- Do not commit secrets.
- Do not expose `auth_mode = "none"` on a public host.
- Keep Dictation, Assist, and Voice Agent behavior separate.
- Voice Agent WebSocket auth is subprotocol-first: use the session response `ws_url` with `ws_subprotocol` (`ticket.<value>`). The legacy `legacy_ws_url` query form exists only for compatibility; do not teach new browser clients to put tickets in URLs.
- Voice Agent tickets default to 90 seconds. Expired tickets are not refreshed in v1; create a new session instead.
