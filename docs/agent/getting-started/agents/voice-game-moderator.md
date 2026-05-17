# Voice Game Moderator Prompt

Stable prompt URL: `https://speechkit.cc/getting-started/agents/voice-game-moderator.md`

## Short prompt

```text
Hey AI Agent, go to speechkit.cc and build a Voice Agent game moderator in this workspace.
```

## Full prompt

```text
Hey AI Agent, go to speechkit.cc, understand the SpeechKit framework, and create a React/Vite app in this current workspace/server that starts a SpeechKit Voice Agent session for a one-hour game-moderator experience. Set up a fresh local SpeechKit Server as part of the generated Docker Compose stack. Use /v1/voiceagent/sessions and the AsyncAPI WebSocket contract, provide Start/Stop controls, microphone permission handling, transcript display, a game timer, and a custom system prompt that makes the agent explain rules, moderate turns, keep the session entertaining, and summarize the session at the end. Keep all secrets in environment variables, do not connect to an existing or preconfigured SpeechKit Server, and add deterministic tests plus a live verification command that starts the generated app, creates a Voice Agent session, sends a text turn through the WebSocket, and verifies a real output transcript. The live verification must write speechkit-one-shot-functional-result.json with status=pass, manifest_file=speechkit-one-shot-manifest.json, app_kind=web, app_url matching the manifest localhost URL, and a passing modes.voiceagent result including status=pass, output_transcript, and checked_via_app=true. Package the generated app for Docker Desktop with docker compose, expose the UI on localhost, and write speechkit-one-shot-manifest.json with docker_compose_file, localhost_urls, speechkit_server_url, and speechkit_server_token_env.
```

## Required artifacts

- `speechkit-one-shot-manifest.json`
- `speechkit-one-shot-functional-result.json`
- JSON Schemas:
  - `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`
  - `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`
