# Go Framework Integration Prompt

Stable prompt URL: `https://speechkit.cc/getting-started/agents/go-framework-integration.md`

## Short prompt

```text
Hey AI Agent, go to speechkit.cc and add SpeechKit to a small Go app in this workspace.
```

## Full prompt

```text
Hey AI Agent, go to speechkit.cc, understand the SpeechKit framework, and add SpeechKit as a Go framework dependency in this current workspace/server using the documented Dictation, Assist, Voice Agent, and Hands-Free capability contracts. Import only the public pkg/speechkit/... components the app needs: dictation for STT-only, assist for one-shot utilities/LLM, wakeword for activation, tts for spoken output, companion for Hands-Free composition, agentkit or voiceagent/live for embedded realtime hosts, and client for a freshly generated local SpeechKit Server. Prefer the scaffold templates go-assist-voice-companion, go-voice-agent-companion, or go-dictation-handsfree-ui when they fit. If adding a Voice Companion, wire companion.NewHandsFree with TargetMode: companion.TargetAssist; if adding continuous dialogue, use companion.TargetVoiceAgent; if adding dictation activation, use companion.TargetDictationUIAssisted and keep text commit UI-assisted. Add a small runnable example, keep secrets in environment variables, never import internal/* or the Windows client from the library integration, and include a live verification command against that freshly generated local SpeechKit Server that always proves server startup plus /healthz and /readyz reachability. Functional Dictation, Assist, and Voice Agent checks should run only when the needed provider/audio fixture environment variables are present, and should otherwise be reported as skipped instead of faked. Do not connect to an existing or preconfigured SpeechKit Server. The live verification must write speechkit-one-shot-functional-result.json with status=pass, manifest_file=speechkit-one-shot-manifest.json, app_kind=go, app_url matching the manifest localhost URL, and passing checked modes; each checked mode result must include status=pass, transcript/output text or output_transcript, and checked_via_app=true. Package the runnable example and a fresh SpeechKit Server for Docker Desktop with docker compose, expose it on localhost, and write speechkit-one-shot-manifest.json with docker_compose_file, localhost_urls, speechkit_server_url, and speechkit_server_token_env.
```

## Required artifacts

- `speechkit-one-shot-manifest.json`
- `speechkit-one-shot-functional-result.json`
- JSON Schemas:
  - `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`
  - `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`
