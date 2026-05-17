# Go Framework Integration Prompt

Stable prompt URL: `https://speechkit.cc/getting-started/agents/go-framework-integration.md`

## Short prompt

```text
Hey AI Agent, go to speechkit.cc and add SpeechKit to a small Go app in this workspace.
```

## Full prompt

```text
Hey AI Agent, go to speechkit.cc, understand the SpeechKit framework, and add SpeechKit as a Go framework dependency in this current workspace/server using the documented Dictation, Assist, and Voice Agent contracts. Prefer pkg/speechkit for embedded Go hosts and pkg/speechkit/client for a freshly generated local SpeechKit Server. Add a small runnable example, keep secrets in environment variables, and include a live verification command against that freshly generated local SpeechKit Server that proves Dictation, Assist, and Voice Agent functionality through the generated app path instead of only checking reachability. Do not connect to an existing or preconfigured SpeechKit Server. The live verification must write speechkit-one-shot-functional-result.json with status=pass, manifest_file=speechkit-one-shot-manifest.json, app_kind=go, app_url matching the manifest localhost URL, and passing modes.dictation, modes.assist, and modes.voiceagent results; each checked mode result must include status=pass, transcript/output text or output_transcript, and checked_via_app=true. Package the runnable example and a fresh SpeechKit Server for Docker Desktop with docker compose, expose it on localhost, and write speechkit-one-shot-manifest.json with docker_compose_file, localhost_urls, speechkit_server_url, and speechkit_server_token_env.
```

## Required artifacts

- `speechkit-one-shot-manifest.json`
- `speechkit-one-shot-functional-result.json`
- JSON Schemas:
  - `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`
  - `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`
