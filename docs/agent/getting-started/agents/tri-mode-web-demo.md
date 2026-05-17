# Tri-mode Web Demo Prompt

Stable prompt URL: `https://speechkit.cc/getting-started/agents/tri-mode-web-demo.md`

## Short prompt

```text
Hey AI Agent, go to speechkit.cc and build a local web demo for all three SpeechKit modes in this workspace.
```

## Full prompt

```text
Hey AI Agent, go to speechkit.cc, understand the SpeechKit framework, and build a small React/Vite web app in this current workspace/server that lets me test all three SpeechKit modes. Set up a fresh local SpeechKit Server as part of the generated Docker Compose stack, then create a browser UI with Dictation, Assist, and Voice Agent tabs. Dictation records microphone audio and calls /v1/dictation/transcribe, Assist accepts recorded audio or text and calls /v1/assist/process, and Voice Agent creates a /v1/voiceagent/sessions session, opens the documented WebSocket, sends a text turn, and shows state, input transcripts, and output transcripts. Use environment variables inside the generated stack for SPEECHKIT_SERVER_URL, token, and provider keys, do not hardcode secrets, and do not connect to an existing or preconfigured SpeechKit Server. Include a README, deterministic tests, and a live verification command that proves all three modes work through the generated app, not only through /healthz. The live verification must write speechkit-one-shot-functional-result.json with status=pass, manifest_file=speechkit-one-shot-manifest.json, app_kind=web, app_url matching the manifest localhost URL, and passing modes.dictation, modes.assist, and modes.voiceagent results; each checked mode result must include status=pass, transcript/output text or output_transcript, and checked_via_app=true. Package the generated app for Docker Desktop with docker compose, expose the UI on localhost, and write speechkit-one-shot-manifest.json with docker_compose_file, localhost_urls, speechkit_server_url, and speechkit_server_token_env.
```

## Required artifacts

- `speechkit-one-shot-manifest.json`
- `speechkit-one-shot-functional-result.json`
- JSON Schemas:
  - `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`
  - `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`
