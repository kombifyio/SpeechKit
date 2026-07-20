# kombify-companion

Go-Host fuer die kombify box nach SpeechKit-Hands-Free-Vertrag.
Der aktuelle Standardpfad ist lokal auf der Host-Maschine und bewusst noch
ohne speechkit-server:
Box-USB-Mikro -> sherpa-KWS-Wakeword -> optional SpeechKit
Hands-Free/Assist-Flow -> Box-Speaker.

Der Voice-Agent-Modus (`[mode].target = "voice_agent"`) hat zwei Transporte:

- **`[voice_agent].transport = "local"` (Default)**: die Box spricht direkt
  mit dem Realtime-Provider (Deepgram Voice Agent, Gemini Live, AssemblyAI,
  OpenAI) — kein speechkit-server noetig. Tools (`home_assistant` via
  toolbridge) laufen client-seitig; `think_endpoint_url`/`think_model`/
  `think_api_key_env` schalten das kombify AI Gateway als BYO-Brain des
  Deepgram-Agenten. Provider-Key kommt aus `DEEPGRAM_API_KEY` bzw.
  `ASSEMBLYAI_API_KEY`/`GOOGLE_AI_API_KEY`/`OPENAI_API_KEY`.
  Idle-Teardown default 90 s Reminder / 3 min Deactivate (Kostenkontrolle,
  Deepgram bepreist Verbindungszeit).
- **`transport = "server"`**: Mic-PCM 16 kHz S16LE -> speechkit-server
  Voice-Agent-WebSocket -> 24 kHz PCM zurueck auf den Box-Speaker.

Fuer reine Geraete-/Wakeword-Tests nutzt `[mode].target = "wake_only"` nur
Mikrofon, lokalen KWS und den Box-Status, ohne STT/Assist/TTS aufzurufen.

## Setup

```powershell
powershell -File tools\get-model.ps1       # sherpa KWS-Modell (einmalig)
powershell -File tools\make-keywords.ps1   # keywords.txt + keywords.jarvis.txt
powershell -ExecutionPolicy Bypass -File examples\kombify-box-satellite\run-companion.ps1
```

Hinweise:
- cgo noetig (sherpa-onnx). Auf Windows ein MinGW-Toolchain verwenden.
- Zur **Laufzeit** muessen die sherpa-onnx-DLLs (`sherpa-onnx-c-api.dll`,
  `onnxruntime.dll`, ...) auf dem `PATH` liegen. Sie liegen im Go-Modul-Cache
  unter `sherpa-onnx-go-windows@<ver>\lib\x86_64-pc-windows-gnu\`; diesen
  Ordner vor dem Start auf den `PATH` legen (sonst `0xc0000135` /
  "DLL not found").
- `config.toml` wird beim Start erwartet (Pfad optional als erstes Argument).
- Die Ring-UI der Box wird direkt vom Companion getrieben: der
  `companion.Options.OnStage`-Hook schreibt `KBX <state>`-Zeilen auf den
  CDC-Statusport (boxlink.go, Autodetect ueber USB VID/PID 303A:8000;
  Override via `[box].status_port` oder `KOMBIFY_BOX_STATUS_PORT`, `"off"`
  deaktiviert). run-companion.ps1 staged nur noch Env/DLLs.
- Geraete werden per Namens-Substring gewaehlt (`[box]` in config.toml);
  Standard "kombify box" matcht das USB-Geraet der Firmware.
- Secrets werden ueber die normale SpeechKit-Secret-Konvention aufgeloest:
  Umgebungsvariable zuerst (Deepgram immer `DEEPGRAM_API_KEY`), optional
  Doppler-CLI-Fallback via `DOPPLER_PROJECT`/`DOPPLER_CONFIG`.
  Der Companion setzt `SPEECHKIT_DISABLE_PORTABLE=1`,
  damit er trotz Beispiel-`config.toml` den globalen SpeechKit-Store nutzt.
- `[speechkit_server]` zeigt per Default auf `https://speechkit.kombify.io`.
  Das wird erst genutzt, wenn `[mode].target = "voice_agent"` gesetzt ist.
  `[voice_agent].provider = "deepgram"` waehlt Deepgram auf dem Server aus;
  der Deepgram-Key gehoert auf den speechkit-server, nicht in diesen Companion.
- Short feedback signals are synthesized by the Companion. Assist and the
  server Voice Agent use the wake cue before capture; the local realtime HA
  path deliberately uses only the Ring UI so self-generated audio cannot enter
  the authority capture. The two-stage understood cue follows only after the
  capture is closed. No SD card is required.
- STT kann lokal oder direkt ueber Deepgram laufen. Fuer Deepgram im lokalen
  `config.toml` setzen:

  ```toml
  [stt]
  provider    = "deepgram"
  model       = "nova-3"
  api_key_env = "DEEPGRAM_API_KEY"
  language    = "de"
  ```

  Danach muss `DEEPGRAM_API_KEY` entweder im Environment, im lokalen
  SpeechKit-Secret-Store oder ueber die konfigurierte Doppler-Quelle
  verfuegbar sein; der Companion startet dann keinen lokalen
  `whisper-server.exe`.
- Der lokale STT-Alternativpfad nutzt `whisper-server.exe` aus
  `%LOCALAPPDATA%\SpeechKit`.
- Assist/TTS laufen im aktuellen Box-Profil lokal-first: lokaler
  OpenAI-kompatibler LLM-Endpunkt auf `http://127.0.0.1:8082/v1` und optional
  Piper-TTS.
- Das lokale Whisper-Modell wird automatisch unter
  `%LOCALAPPDATA%\SpeechKit\models\ggml-small.bin` gesucht. Alternativ
  `[local].model_path` setzen.
- Piper wird automatisch unter `%LOCALAPPDATA%\SpeechKit\piper...` gesucht;
  Stimmen liegen standardmaessig in
  `%LOCALAPPDATA%\SpeechKit\piper-voices`.
- Ist die lokale LLM-Runtime noch nicht gestartet, laufen lokale Skills
  weiter; offene Fragen liefern einen Setup-Hinweis statt eines Gateway-Fehlers.
- Der lokale Standard nutzt `keywords.jarvis.txt` fuer "hey jarvis"/"jarvis".
  Die Datei wurde mit `tools/encode-keywords` aus dem aktuellen GigaSpeech
  KWS-`tokens.txt` erzeugt, damit kein Python-Tokenizer noetig ist.
- Inline-`Keywords` im Wakeword-Detektor werden vom Framework automatisch
  BPE-tokenisiert (`wakeword.EncodeKeywords`); eine explizite `keywords_file`
  wird beim Start validiert (Rohtext -> klarer Fehler statt stillem Nie-Match).

## Wakeword-Smoke-Test

Das kleine Testtool prueft eine 16-kHz-Mono-PCM16-WAV gegen exakt denselben
SpeechKit/Sherpa-Pipeline-Code wie der Companion:

```powershell
go build -o examples\kombify-box-satellite\kws-smoke.exe ./examples/kombify-box-satellite/tools/kws-smoke
examples\kombify-box-satellite\kws-smoke.exe `
  --model-dir examples\kombify-box-satellite\models\sherpa-onnx-kws-zipformer-gigaspeech-3.3M-2024-01-01 `
  --keywords examples\kombify-box-satellite\keywords.jarvis.txt `
  --wav C:\path\to\hey-jarvis-16k-mono.wav `
  --phrase "hey jarvis"
```

Erwartung: mindestens eine `DETECTED`-Zeile. Wenn der Smoke-Test feuert, aber
der Live-Test nicht, liegt es am realen Mikrofon-Audio, Abstand/Aussprache oder
am Tuning von `[wakeword].threshold` und `[wakeword].input_gain`. `input_gain`
verstaerkt nur den Wakeword-Detector-Pfad; die eigentliche Aufnahme bleibt roh.

## Smart-Home: Home Assistant

Home Assistant is the sole semantic authority for commands recognized by the
deterministic Home Assistant lexicon in one-shot Assist and the realtime Voice
Agent. This is an intentionally exact capability boundary, not a claim that an
open-ended semantic classifier can recognize every implied smart-home phrase.
Missing configuration, no-match, and Home Assistant failures produce a
terminal local result. A recognized request is never reinterpreted by the
Gateway, an MCP, or the realtime model.

```toml
[home_assistant]
base_url  = "https://homeassistant.home.arpa:8123"
token_env = "KOMBIFY_HA_TOKEN"   # Long-Lived Access Token; never commit it
language  = "de"                  # optional; otherwise request locale
```

```powershell
$env:KOMBIFY_HA_TOKEN = "<long-lived-access-token>"
```

The bearer token may reach only a local Home Assistant origin. Plain HTTP is
accepted only for literal loopback/`localhost`; LAN addresses and local DNS
names require HTTPS. DNS targets are checked again after resolution, environment
proxies and redirects are disabled, and raw Home Assistant response bodies are
never surfaced.

The realtime `home_assistant` tool is installed even when the integration is
not configured. Its description and framework instruction require the model to
call it for every command recognized by the deterministic lexicon. Prompting is
not the authorization boundary: synchronously when a tool event arrives and
before asynchronous dispatch, the host requires a write-once transcript seal
derived by batch STT from the complete local PCM capture. Only a normal silence
boundary with zero local fan-out drops is eligible for a seal; cancellation,
source closure, maximum-duration truncation, empty/error STT, or any dropped
microphone frame leaves Home Assistant fail-closed. Streaming provider
transcripts are diagnostic input only and can neither create nor replace the
seal. The host also requires `EndAudioStream` to have completed for the exact
turn and the model's query to match the sealed text after whitespace-only
normalization. It replaces all model arguments with the sealed host transcript
and locale and authorizes at most one Home Assistant call for that turn. A
server-VAD tool call arriving before the host capture closes is terminally
denied and never reaches the executor. Box playback then remains buffered until
the
host has a terminal result receipt for the exact tool call, session, and turn,
and the final output transcript matches the authoritative Home Assistant text.
Missing host seals, stale sessions or receipts, early audio, mismatches,
additional calls, and indeterminate turns discard all model audio and use only
the local error cue. Late or multiple provider transcript segments do not alter
the host seal. General, deterministically non-HA turns continue through the
realtime conversation unchanged unless they contain a rejected smart-home tool
call.

The Box accepts only local Whisper as the batch STT authority for this seal and
starts it before opening a local realtime session. A configured remote/gateway
STT provider is never reused and never becomes a fallback for realtime Home
Assistant authorization. This keeps transcript authority independent from the
conversation provider and preserves the local SpeechKit-to-Home-Assistant
boundary without cloud federation. If local Whisper or its model is
unavailable, the local realtime runtime refuses to start. It does not fall back
to provider transcripts even for apparently general turns: otherwise a remote
provider could mislabel a smart-home request and speak a simulated result
without a Home Assistant receipt.

The HA capture subscribes atomically at the wake event before provider
connection, but deliberately excludes all pre-detection audio from authority.
It first requires 250 ms of clean, sample-counted silence and then real user
speech; a command already in progress when detection arrives is rejected rather
than partially transcribed. Local realtime uses the Ring UI instead of an
audible wake cue. Every host playback epoch marks all overlapping capture
subscriptions contaminated, so provider/TTS echo can never authorize a turn.
Buffer overflow is counted and invalidates authority instead of silently
truncating the command. Silence and maximum duration are measured from ordered
PCM sample counts, not consumer wall-clock time, so a queued correction is
consumed before the silence boundary can seal.

Audio buffers are generation-scoped. Out-of-turn audio is accepted only after
the local idle timer synchronously announces a trusted reminder/deactivation
prompt; a late audio callback or stale `listening` transition cannot create or
complete a playback generation. Every HA-classified or indeterminate turn
retires its realtime session after the decision, so callbacks from a provider
response without a portable response ID cannot be mistaken for a later idle
reminder. The next wake starts a fresh session. Providers without final output
transcription fail closed for the realtime HA surface even when the host input
seal is valid.

> **Migration (v0.49, angewandt):** `skills.go` delegiert die deterministischen
> Skills (Zeit, Datum, Mathe, Wetter, Timer, Erinnerung, Wikipedia, Temperatur)
> jetzt an das oeffentliche Katalog-Paket `pkg/speechkit/assist/skills`
> (`companionskills.New(...)`); box-lokal bleiben nur `help`/`status`. Timer und
> Erinnerung feuern echt ueber den eingebauten Scheduler (`OnAlarm` → `audio.Ding()`),
> `router.Close()` cleans them up during shutdown. `hass.go` adapts the same hardened
> public Home Assistant boundary for the realtime tool; it no longer contains a
> separate HTTP implementation or a no-match-to-model fallback.
>
> Verifiziert per lokalem cgo-Build+Test (`CGO_ENABLED=1 CC="zig cc" go
> build/vet/test ./examples/kombify-box-satellite/`). CI
> (`.github/workflows/example-box-satellite.yml`) baut+vettet das Beispiel
> (Windows/mingw); `go test` laeuft dort nicht, weil das Binary sherpa-onnx +
> onnxruntime als Shared Libraries linkt (Runtime-DLLs = Device-Target-Sache).

## Wakeword-Robustheit
- Wake-Erkennung pausiert waehrend Ding + Aufnahme + TTS-Wiedergabe
  (`Pipeline.Pause()/Resume()`), verhindert Selbst-Trigger.
- Der schnelle lokale Test nutzt "hey jarvis", weil die Phrase phonemisch
  distincter ist als das Ein-Wort-Wakeword "kombify" und sofort mit dem
  vorhandenen KWS-Modell tokenisiert werden kann.
- Ein-Wort-Wakewords haben mehr False-Accepts als "hey ..."-Phrasen;
  `[wakeword].threshold` und `min_consecutive_frames` in config.toml tunen.
