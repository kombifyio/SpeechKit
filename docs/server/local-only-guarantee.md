# Local-Only Guarantee

Both delivery surfaces of SpeechKit — the Windows desktop installer
(`SpeechKit-Setup.exe`) and the Linux Docker server (`speechkit-server`)
— MUST work end-to-end on a fresh machine with zero cloud API keys.
This page documents the contract, the tests that enforce it, and how
the release pipeline is gated on the result.

## What "local-only" means

After install + first-run, every one of the three modes succeeds
using local models only:

- **STT:** whisper.cpp. The Windows installer ships the local runtime, while
  Whisper models are downloaded on demand by the Setup Wizard or Settings. A
  Local onboarding path must start the selected model immediately; when the
  user has not selected anything else, it starts `whisper.ggml-small`.
  `/app/complete-setup` must not require `ggml-small.bin` or any other model
  to be present on disk; model readiness is reported by the local STT
  readiness path after setup. See
  [`docs/setup/bundled-starter-model.md`](../setup/bundled-starter-model.md)
  for the full model-download policy and setup gate contract.
- **LLM:** Gemma 4 E4B IT Q4_K_M via llama-server (llama.cpp).
- **TTS:** server's local TTS provider when one is configured. The
  cascaded Voice Agent path runs text-only when no local TTS is wired
  (Device-Target plays audio via its own oto/v3 stack).
- **Voice Agent:** turn-based cascaded provider
  ([`internal/voiceagent/cascaded`](../../internal/voiceagent/cascaded))
  combining STT + LLM + optional TTS.

A "leaked" cloud key — any of `GOOGLE_AI_API_KEY`, `GOOGLE_STT_API_KEY`,
`OPENAI_API_KEY`, `GROQ_API_KEY`, `OPENROUTER_API_KEY`, `HF_TOKEN` —
flags the deployment as cloud-touching. The local-only gate fails when
any of those resolves to a non-empty value.

## What's gated

`.github/workflows/release.yml`'s `publish-release` step depends on:

- `install-e2e-linux-gate` — runs
  [`.github/workflows/install-e2e-linux.yml`](../../.github/workflows/install-e2e-linux.yml).
- `install-e2e-windows-gate` — runs
  [`.github/workflows/install-e2e-windows.yml`](../../.github/workflows/install-e2e-windows.yml).

If either fails, the GitHub release is NOT published and stays draft.
The Linux gate runs unconditionally on every tag; the Windows gate
runs only when `should_build_windows` is true (the same condition as
the existing `build-and-release` job).

## What's tested per mode

| Mode | Assertion | Implementation |
|---|---|---|
| Dictation | Transcript matches the Go regex in `testdata/e2e/dictation/*.expected.txt` (e.g. `(?i)hello,?\s+world`). | Whisper.cpp local subprocess. |
| Assist (codeword utility) | Output contains the regex in `testdata/e2e/assist/utility-uppercase.expected.txt` after the kernel's codeword pipeline. | STT + assist flow against the server. |
| Assist (LLM free-form) | Response is non-empty AND contains the substring in `testdata/e2e/assist/llm-shortq.expected-prefix.txt` (e.g. `four`). | Local llama-server. |
| Voice Agent | Two-turn conversation: turn 2 yields a non-empty assistant response. Context retention is exercised by the audio fixtures (`my name is Marcel` → `what is my name`). | `internal/voiceagent/cascaded.Provider`. |
| Cloud-key leak guard (Linux) | `GET /v1/deployment/status` reports `providers.cloud_keys_present == false`. | `internal/server/core.anyCloudKeyEnvSet`. |
| Cloud-key leak guard (Windows) | Runner shell unsets all cloud-key env vars before `sk-localprobe` runs; the probe's deployment-style assertion would fail at the runner level if any leak slipped through. | `Remove-Item env:...` in the workflow. |

## Cadence

| Trigger | Behavior |
|---|---|
| `push: tags: ['v*']` (release.yml) | Both gates run; blocking. |
| `workflow_dispatch` (install-e2e-*) | Manual re-run by maintainers without cutting a new tag. |
| `schedule: nightly` (install-e2e-*) | Both gates run on `main`; failures emit alerts but do not block anything. |

PR-level CI deliberately does NOT run install-E2E. The fast PR loop
stays under five minutes; install-E2E sits on the slower release lane.

## Cost

GitHub Actions monthly spending is capped at **50 EUR** on the
`kombifyio` org. The cap is set via
[`scripts/set-actions-billing-cap.sh`](../../scripts/set-actions-billing-cap.sh)
(documented fallback to the GitHub UI when the REST endpoint returns
404, which it does today).

Per-run cost guard:
- Models are cached in GitHub Actions cache (~5 GB total). Warm runs
  download only deltas.
- Compose stack on Linux is brought up and torn down per run; volumes
  are removed.
- Windows runner has a 60 min timeout; Linux 30 min.

## Files

- `cmd/sk-localprobe/main.go` — Windows-side probe client (local-only kernel verifier).
- `cmd/sk-e2e/main.go` — Linux-side E2E client (`--local-only` flag).
- `internal/voiceagent/cascaded/` — cross-platform cascaded provider.
- `internal/voiceagent/local_provider.go` — Device-Target wrapper.
- `internal/server/core/providers_env.go` — `anyCloudKeyEnvSet` helper.
- `testdata/e2e/` — audio fixtures + expected outputs.
- `.github/workflows/install-e2e-windows.yml` — Windows gate.
- `.github/workflows/install-e2e-linux.yml` — Linux gate.
- `deploy/docker/docker-compose.test.yml` — `local-only` compose profile.
- `scripts/install-server.sh` — `--strict-local-only` flag refuses to write `.env` when any cloud key is set.
- `scripts/set-actions-billing-cap.sh` — 50 EUR/month cap helper.

## Out of scope

- **macOS install-E2E.** No macOS target today.
- **Non-cascaded local VA providers (Moshi).** Only the cascaded path is
  exercised. Real-time providers like Gemini Live remain cloud-only.
- **Per-PR install-E2E.** Cadence is release-tag, nightly, manual.
- **Wails-specific kernel init paths on Windows.** `cmd/sk-localprobe`
  drives the kernel libraries directly (whisper.cpp + llama-server +
  cascaded provider). Wails-side init is covered by
  `.github/workflows/windows-build.yml` build verification.
