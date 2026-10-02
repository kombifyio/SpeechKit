# Privacy Policy — kombify SpeechKit

**Effective date:** 2026-03-30
**Last updated:** 2026-10-01

kombify SpeechKit ("SpeechKit", "the App") is a speech-to-text framework with a desktop host application and an Android companion app. This privacy policy explains what data SpeechKit processes, where it is stored, and what choices you have.

## 1. Data We Process

### Audio Recordings

SpeechKit captures microphone audio when you activate recording via hotkey or button press. Audio is:

- processed in real time for voice activity detection (VAD) and transcription
- saved locally as WAV files while `save_audio` is enabled (the default)
- automatically deleted after the configured retention period (default: 7 days);
  the retention and the `max_audio_storage_mb` cap keep applying to recordings
  saved earlier after you turn `save_audio` off

Audio is **never** uploaded to kombify servers. When you use cloud STT or TTS providers, audio segments are sent directly to the provider you configured (see Section 3).

### Meeting Recordings

Meetings are the one case where SpeechKit records people other than you, so they
are handled more strictly than dictation:

- **Meeting audio is never saved**, whatever `save_audio` is set to. A meeting
  keeps what was said, not the sound of anyone saying it. This is enforced in
  the capture pipeline rather than by a setting, so it cannot be switched off.
- A meeting records two separate sources: your microphone and your computer's
  own audio output, which is what the other participants are heard through. It
  captures whatever is playing on your machine while a meeting runs.
- Meeting transcripts and notes are kept until you delete them. Set
  `store.meeting_retention_days` to discard finished meetings automatically
  after that many days; individual meetings can be pinned to keep them anyway.

Recording other people may require their knowledge or consent where you live.
SpeechKit does not judge that for you.

### Transcriptions

Transcribed text is stored locally in a SQLite database next to the application. Transcriptions include:

- the transcribed text
- language, provider name, model name
- duration and processing latency
- timestamp

Dictation and Assist transcripts and Voice Agent conversations are kept until
you delete them, one by one from the Library or all at once with the privacy
erasure. Set `store.transcript_retention_days` to discard them, with their
audio, automatically after that many days; pinned transcripts are kept.

### Clipboard and Selected Text

Dictation and Assist insert text by pasting it through the system clipboard by
default. When SpeechKit writes a transcript to the clipboard it asks the system
not to keep it: on Windows it is excluded from clipboard history (Win+V),
cloud clipboard sync and clipboard monitors; on macOS it is marked concealed,
transient and current-host-only (no Universal Clipboard). Clipboard managers
that ignore these flags can still record it. SpeechKit restores your previous
clipboard text after pasting, or clears the transcript if there was nothing to
restore, including when the paste fails.

Assist copies the text selected in the foreground app and sends it, with the
window title, to the Assist provider you configured. Set `[assist]
capture_selection = false` to stop that. SpeechKit never sends the copy
shortcut to terminals, remote-desktop clients or VM consoles.

### Wake-Word Clips

Wake-word clips are written locally only when you enable
`local_capture_enabled`. Uploading them to a SpeechKit Server is a separate
opt-in (`upload_enabled`). The uploader follows the network scope: it never
runs in `device_only`, reaches only private addresses in `local_network`,
requires HTTPS off the local network, follows no redirects, and by default
(`upload_only_labeled = true`) sends only clips you labelled. Local clips are
removed after `local_retention_days` (default 30) or beyond `local_max_files`
(default 500). The "STT phrase" wake-word backend listens through the on-device
Whisper model only; without a local speech model it does not start.

### Configuration and Credentials

- Application settings are stored in a local `config.toml` file
- Provider API keys (e.g., Hugging Face, OpenAI) are stored in the Windows Credential Manager (desktop) or SharedPreferences (Android)
- SpeechKit does not transmit your credentials to kombify or any third party other than the provider you configured

## 2. Data We Do Not Collect

The SpeechKit desktop host and Android app do **not** collect, transmit, or store:

- usage analytics or telemetry
- crash reports
- device identifiers or fingerprints
- location data
- contact lists or personal files
- advertising identifiers

SpeechKit operates **local-first**. No data leaves your device unless you explicitly configure a cloud provider.

The one network request SpeechKit makes on its own is the update check: by
default the desktop app contacts the release host every few hours to look for a
new version and sends no audio, text or identifiers beyond what any HTTPS
request exposes. Turn automatic checks off with `[telemetry] update_check =
false` (an explicit "check now" in Settings still works) or turn updates off
with `[update] enabled = false`. Administrators can enforce both with the
`Telemetry\UpdateCheck` and `Update\Enabled` Group Policy values. In the
`local_network` and `device_only` network scopes update checks run only with
`allow_setup_traffic = true` (see `docs/privacy/network-scope.md`).

The public SpeechKit website is separate from the local apps. The default OSS
website build also has analytics disabled. The Kombify-hosted SaaS website may
capture privacy-safe PostHog product metrics only after explicit analytics
consent, using an anonymous marketing identifier and the EU proxy
`https://e.kombify.io`; it does not use Session Replay.

## 3. Third-Party Cloud Providers

When you enable cloud providers for speech-to-text (STT), text-to-speech (TTS), or AI assistance (LLM), audio or text data is transmitted to the respective provider. SpeechKit supports:

| Provider | Data Sent | Provider Privacy Policy |
|----------|-----------|------------------------|
| Hugging Face | Audio (STT), Text (LLM/TTS) | https://huggingface.co/privacy |
| OpenAI | Audio (STT), Text (LLM/TTS) | https://openai.com/privacy |
| Deepgram | Audio (STT/TTS), Audio (Voice Agent) | https://deepgram.com/privacy |
| AssemblyAI | Audio (STT), Audio (Voice Agent) | https://www.assemblyai.com/legal/privacy-policy |
| Groq | Audio (STT), Text (LLM) | https://groq.com/privacy-policy |
| Google Cloud / Gemini (opt-in BYOK, your own credentials) | Audio (STT/TTS), Audio (Voice Agent) | https://policies.google.com/privacy |

You choose which providers to enable. When all cloud providers are disabled, SpeechKit operates entirely offline using local models.

On managed Windows machines the Group Policy `Providers\EnforceLocalOnly`
caps the network scope at `local_network` and pins speech-to-text to the
on-device model for the life of the process; users cannot change it in Settings.

**Voice Agent mode** streams audio in real time to the configured provider (e.g., OpenAI Realtime, Deepgram, or opt-in Google Gemini Live) via WebSocket. This audio stream is processed by the provider according to their privacy policy.

## 4. Local Storage

All application data is stored locally:

| Data | Location (Windows) | Location (Android) |
|------|--------------------|--------------------|
| Configuration | `config.toml` next to SpeechKit.exe | SharedPreferences |
| Transcriptions | `%APPDATA%/SpeechKit/feedback.db` | Room database (app-internal) |
| Audio files (dictation only) | `%APPDATA%/SpeechKit/audio/` | App-internal storage |
| Meeting transcripts and notes | `%APPDATA%/SpeechKit/feedback.db` | — |
| Credentials | Windows Credential Manager | SharedPreferences |
| Logs (off by default; `[logging] level`) | Application log directory | Logcat |

You can delete all local data by uninstalling SpeechKit or manually removing these directories.
The privacy erasure (`POST /api/v1/privacy/delete`, see
`docs/compliance/dsgvo-subject-rights.md`) removes the database content, audio,
meeting screenshots, wake-word training clips and the app log, and compacts
the database so deleted text does not linger in free pages. The audit log, a
metadata-only compliance record, is kept until its own retention expires.
Rotated app logs are deleted after 14 days.

## 5. Android Permissions

The Android app requests the following permissions:

- **RECORD_AUDIO**: Required for speech recognition
- **INTERNET**: Required for cloud provider communication (when enabled)
- **FOREGROUND_SERVICE_MICROPHONE**: Required for background recording during keyboard use

No permissions are used for purposes other than stated above.

## 6. Children

SpeechKit is not directed at children under 16. We do not knowingly collect data from children.

## 7. Changes to This Policy

We may update this policy when SpeechKit adds new features. Changes will be noted in the CHANGELOG and this document will be updated with a new "Last updated" date.

## 8. Contact

For privacy-related questions, open an issue at the SpeechKit repository or contact the kombify team at the address listed in the repository's SECURITY.md file.
