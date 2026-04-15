# Privacy Policy — kombify SpeechKit

**Effective date:** 2026-03-30
**Last updated:** 2026-03-30

kombify SpeechKit ("SpeechKit", "the App") is a speech-to-text framework with a desktop host application and an Android companion app. This privacy policy explains what data SpeechKit processes, where it is stored, and what choices you have.

## 1. Data We Process

### Audio Recordings

SpeechKit captures microphone audio when you activate recording via hotkey or button press. Audio is:

- processed in real time for voice activity detection (VAD) and transcription
- optionally saved locally as WAV files if `save_audio` is enabled in your configuration
- automatically deleted after the configured retention period (default: 7 days)

Audio is **never** uploaded to kombify servers. When you use cloud STT or TTS providers, audio segments are sent directly to the provider you configured (see Section 3).

### Transcriptions

Transcribed text is stored locally in a SQLite database next to the application. Transcriptions include:

- the transcribed text
- language, provider name, model name
- duration and processing latency
- timestamp

### Configuration and Credentials

- Application settings are stored in a local `config.toml` file
- Provider API keys (e.g., Hugging Face, OpenAI) are stored in the Windows Credential Manager (desktop) or SharedPreferences (Android)
- SpeechKit does not transmit your credentials to kombify or any third party other than the provider you configured

## 2. Data We Do Not Collect

SpeechKit does **not** collect, transmit, or store:

- usage analytics or telemetry
- crash reports
- device identifiers or fingerprints
- location data
- contact lists or personal files
- advertising identifiers

SpeechKit operates **local-first**. No data leaves your device unless you explicitly configure a cloud provider.

## 3. Third-Party Cloud Providers

When you enable cloud providers for speech-to-text (STT), text-to-speech (TTS), or AI assistance (LLM), audio or text data is transmitted to the respective provider. SpeechKit supports:

| Provider | Data Sent | Provider Privacy Policy |
|----------|-----------|------------------------|
| Hugging Face | Audio (STT), Text (LLM/TTS) | https://huggingface.co/privacy |
| OpenAI | Audio (STT), Text (LLM/TTS) | https://openai.com/privacy |
| Google Cloud | Audio (STT/TTS), Text (LLM), Audio (Voice Agent) | https://policies.google.com/privacy |
| Groq | Audio (STT), Text (LLM) | https://groq.com/privacy-policy |

You choose which providers to enable. When all cloud providers are disabled, SpeechKit operates entirely offline using local models.

**Voice Agent mode** streams audio in real time to the configured provider (e.g., Google Gemini Live) via WebSocket. This audio stream is processed by the provider according to their privacy policy.

## 4. Local Storage

All application data is stored locally:

| Data | Location (Windows) | Location (Android) |
|------|--------------------|--------------------|
| Configuration | `config.toml` next to SpeechKit.exe | SharedPreferences |
| Transcriptions | `%APPDATA%/SpeechKit/feedback.db` | Room database (app-internal) |
| Audio files | `%APPDATA%/SpeechKit/audio/` | App-internal storage |
| Credentials | Windows Credential Manager | SharedPreferences |
| Logs | Application log directory | Logcat |

You can delete all local data by uninstalling SpeechKit or manually removing these directories.

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
