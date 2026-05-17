# Android Memo App Prompt

Stable prompt URL: `https://speechkit.cc/getting-started/agents/android-memo-app.md`

## Short prompt

```text
Hey AI Agent, go to speechkit.cc and build a native Android memo app with SpeechKit dictation and assist.
```

## Full prompt

```text
Hey AI Agent, go to speechkit.cc, understand the SpeechKit framework, and create a real native Android memo app in this current workspace/server powered by SpeechKit. The app must be an Android project with Gradle settings, app/build.gradle or app/build.gradle.kts, app/src/main/AndroidManifest.xml, and Kotlin or Java source code; a browser harness or web-only mock is not acceptable. The app should record a memo, send the audio to /v1/dictation/transcribe, save the transcript locally, and offer an Improve button that sends the memo text to /v1/assist/process for cleanup, summary, or rewrite. Set up a fresh local SpeechKit Server as part of the generated Docker Compose verification stack, add a small settings screen or config repository for SPEECHKIT_SERVER_URL and token, never hardcode secrets, handle offline/provider errors clearly, include Android tests plus a Gradle build verification, and add a Gradle task named verifySpeechKitLive that proves Dictation and Assist against the freshly generated local SpeechKit Server through the Android settings/config path. Do not connect to an existing or preconfigured SpeechKit Server. The live verification must write speechkit-one-shot-functional-result.json with status=pass, manifest_file=speechkit-one-shot-manifest.json, app_kind=android, app_transport=android, server_url_source=settings_screen or android_config, app_url matching the manifest localhost URL, and passing modes.dictation and modes.assist results; each checked mode result must include status=pass, transcript/output text, and checked_via_app=true. Also write speechkit-one-shot-manifest.json with docker_compose_file, localhost_urls, speechkit_server_url, speechkit_server_token_env, and android_project_dir.
```

## Required artifacts

- `speechkit-one-shot-manifest.json`
- `speechkit-one-shot-functional-result.json`
- JSON Schemas:
  - `https://speechkit.cc/schemas/speechkit-one-shot-manifest.schema.json`
  - `https://speechkit.cc/schemas/speechkit-one-shot-functional-result.schema.json`
