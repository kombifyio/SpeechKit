# SpeechKit Architecture V2.0

## Drei-Modi-Framework

SpeechKit wird zu einem Drei-Modi-Framework, das vom reinen Diktat bis zum dauerhaften Voice Companion reicht.

| Modus | Hotkey | Interaktion | LLM | TTS | Output | Zustand |
|-------|--------|-------------|-----|-----|--------|---------|
| **Dictation** | `dictate_hotkey` (PTT) | Einmalig: Sprache → Text mit User-Kontext | Nein | Nein | Text only | V23 User Intelligence |
| **Assist** | `assist_hotkey` (PTT) | Einmalig: Sprache → Aktion/Antwort | Ja (Utility) | Ja | Text + Audio | V23 Utility Intelligence |
| **Voice Agent** | `voice_agent_hotkey` (Toggle oder Fallback-PTT) | Dauerhaft: Companion-Loop oder Pipeline-Fallback | Ja (Agent) | Ja | Dialog + Summary | V23 Brainstorming Intelligence |

### V23 Intelligence Contracts

- **Dictation / Transcribe = User Intelligence.** Der Modus bleibt strikt audio-to-text. Er darf User-Dictionary, Custom Words, Phrase Hints und deterministische Korrekturen nutzen, aber keine Utility Tools, Codewords oder Assist-Antworten ausfuehren.
- **Assist = Utility Intelligence.** Der Modus ist die einzige One-Shot-Utility-Schicht: STT, Codewords, host-seitige Tools, LLM-Ergebnis, optional TTS und Panel-Surface.
- **Voice Agent = Brainstorming Intelligence.** Der Modus ist fuer Dialog, Ideenentwicklung und schnelle Follow-ups zustandshaft. Jede Session muss eine strukturierte Zusammenfassung liefern koennen.

### V23 Provider Standard

Jeder der drei Modi bietet dieselben vier Provider-Gruppen. Eine Provider-Gruppe ist keine Ein-Modell-Grenze: sie enthaelt mindestens ein empfohlenes oder unterstuetztes Modell und kann mehrere Varianten enthalten.

| Provider-Gruppe | Bedeutung | V23 Standard |
|-----------------|-----------|--------------|
| Local Built-in | SpeechKit-verwalteter Runtime-/Modellpfad aus App oder Installer | Dictation ist nativ downloadbar; Assist/Voice Agent expose GGUF-Artefakte und benoetigen bis zur Runtime-Buendelung einen separaten OpenAI-kompatiblen lokalen LLM-Server |
| Local Provider | User-verwalteter lokaler oder self-hosted Provider | Ollama als kanonischer V23 Provider |
| Cloud Provider | SpeechKit-routed Cloud Provider | Hugging Face |
| Direct Provider | Direkte Hersteller-API | Ein fokussiertes Direct-Modell pro Modus als Startpunkt |

Damit ergeben sich mindestens 12 Standard-Bindings: 3 Modi x 4 Provider-Gruppen. Tatsaechlich sind es mehr, weil z.B. Local Built-in Dictation mehrere Whisper/whisper.cpp Varianten anbietet.

### V23 Implementierungsstatus

- **Dictation User Intelligence ist implementiert.** Das User Dictionary liegt strukturiert im Store, wird aus den Settings synchronisiert, als Provider-Hint uebergeben und per deterministischer Korrektur nachverarbeitet. Dictation bleibt text-only.
- **Ollama ist in allen drei Modi als Local Provider angebunden.** Fuer Dictation nutzt SpeechKit einen OpenAI-kompatiblen Transcription Adapter gegen Ollama. Assist nutzt Genkit/Ollama. Voice Agent nutzt Ollama als Pipeline-Fallback ueber STT -> Agent LLM -> optional TTS.
- **Local Built-in ist fuer Dictation nativ downloadbar.** Die Whisper.cpp Modelle erscheinen als mehrere Varianten im Local-Built-in-Profil; nach dem Download wird das Modell aktiviert und der lokale Whisper-Server gestartet.
- **Local Built-in fuer Assist/Voice Agent ist als OpenAI-kompatibler SpeechKit-Runtime-Vertrag modelliert.** Der aktuelle Installerpfad enthaelt Whisper/whisper-talk-llama, aber keinen separaten OpenAI-kompatiblen LLM-Server. Bis dieser Runtime-Server gebundelt ist, darf die UI/Doku diesen Pfad nicht als vollstaendigen One-Click-Download ausgeben.
- **Voice Agent Brainstorming Intelligence ist implementiert.** Finalisierte Dialogturns werden gesammelt und auf Session-Ende als strukturierte Zusammenfassung in den Prompter gegeben. Die Summary kann ueber den Utility/Agent-Fallback laufen und ist unabhaengig vom nativen Realtime-Modell.
- **V23 API-First Boundary ist implementiert.** `pkg/speechkit` exportiert Mode Contracts, Provider Profiles, Provider Groups, Readiness-Modelle und Validierung. Der Desktop Host spiegelt dieselbe Boundary ueber `/api/v1/*`, damit externe Tools SpeechKit als lokales Framework steuern koennen.

### V23 API-First Framework Boundary

V23 trennt drei Ebenen sauber:

| Ebene | Paket / Surface | Zweck |
|-------|-----------------|-------|
| Framework SDK | `pkg/speechkit` | Wiederverwendbare Contracts, Provider Profiles, Mode Settings, Readiness und Command-Typen fuer Go-Hosts |
| Desktop Runtime | `cmd/speechkit` + `internal/*` | Windows App, Audio, Provider-Router, Config, Secrets, UI und konkrete Runtime-Adapter |
| Local Control Plane | `/api/v1/*` | Lokale HTTP API fuer bestehende Tools, Automationen und fremde Softwareloesungen; mutierende Routen sind token-geschuetzt |

Die SDK-Boundary darf keine Desktop-Abhaengigkeit und keine Host-Secrets voraussetzen. Host-Anwendungen koennen den Catalog lesen, Profile gegen Mode Contracts validieren und eigene UIs oder Integrationen bauen, ohne `internal/*` zu importieren.

Der Framework-Katalog fuer die drei Hauptmodi lebt in `pkg/speechkit`. `internal/models` ist nur noch der Adapter fuer die Windows-Runtime und haengt host-spezifische Support-Profile wie TTS, Utility und Embeddings an. Damit ist das Backend der eigentliche Framework-Contract; das Windows-Modul ist ein Client und Referenzhost auf diesem Contract.

Die Local-Built-in-Grenze ist absichtlich eng formuliert: Dictation kann Whisper.cpp-Modelle direkt herunterladen und mit der gebundelten lokalen Transcription-Runtime verwenden. Assist und Voice Agent duerfen GGUF-Modelle herunterladen und auswaehlen, aber diese Modelldateien sind nicht selbst die Runtime. Bis SpeechKit einen OpenAI-kompatiblen lokalen LLM-Server buendelt, muss dieser Server separat konfiguriert und erreichbar sein.

#### Production Trust Boundaries

| Boundary | Default | Produktionsregel |
|----------|---------|------------------|
| Desktop Control Plane | Loopback + Control-Plane-Token | Mutierende Routen bleiben token-geschuetzt und duerfen nicht als public API behandelt werden. |
| SpeechKit Server | Bearer Auth | `auth_mode = "none"` ist nur fuer Loopback/Dev gueltig; public binds benoetigen Bearer oder Edge-HMAC. |
| MCP HTTP Transport | `127.0.0.1:8090` | Non-loopback Management-Modus benoetigt `SPEECHKIT_MCP_TOKEN`; der HTTP-Server setzt Header-/Read-/Idle-Timeouts. |
| Scaffold Templates | Embedded repo-owned templates | Nur Templates mit `template.toml` werden gelistet; Post-Init-Hooks sind opt-in und auf feste Binary/Argument-Paare begrenzt. |
| Provider Boundary | User-/Install-/Env-Secrets | Provider-Antworten, Tool-Call-Daten und externes Audio/Text-Material bleiben untrusted Input. |

#### SDK Contracts

`pkg/speechkit` stellt folgende stabile Typen und Helfer bereit:

- `Mode`, `IntelligenceKind`, `ProviderKind`, `ExecutionMode`, `Capability`
- `ModeContract`, `ModeSettings`, `ProviderProfile`, `ModelVariant`, `Readiness`
- `DefaultModeContracts()`
- `DefaultProviderProfiles()`
- `ProfilesForMode(mode)`
- `ProviderKindsForMode(mode)`
- `ValidateProfileForMode(profile, mode)`

Die Validierung erzwingt die wichtigsten V23-Regeln:

- Dictation akzeptiert nur Transcription/STT-Profile und verbietet LLM sowie Tool Calling.
- Assist benoetigt LLM-Faehigkeit und bleibt One-Shot statt Realtime-Dialog.
- Voice Agent benoetigt entweder natives Realtime-Audio oder einen expliziten Pipeline-Fallback mit Session Summary.
- Jeder Hauptmodus muss alle vier Provider-Gruppen exponieren: Local Built-in, Local Provider, Cloud Provider und Direct Provider.

#### Local Control Plane

Der Desktop Host registriert eine versionierte API:

| Endpoint | Methoden | Vertrag |
|----------|----------|---------|
| `/api/v1/modes` | `GET` | Liefert `contracts` und aktuelle `settings`. |
| `/api/v1/modes/{mode}/settings` | `GET`, `PATCH` | Liest oder aktualisiert Enablement, Hotkeys, Provider-Auswahl und mode-spezifische Intelligence-Settings. |
| `/api/v1/modes/{mode}/start` | `POST` | Startet Dictation, Assist oder Voice Agent ueber den Framework Command Bus. |
| `/api/v1/modes/{mode}/stop` | `POST` | Stoppt den aktiven Mode ueber den Framework Command Bus. |
| `/api/v1/providers/profiles` | `GET` | Liefert Catalog, aktive Profile, Provider-Gruppen und Contracts. |
| `/api/v1/providers/readiness` | `GET` | Liefert versionierte Credential-, Runtime-, Model-Artefakt- und Capability-Readiness pro Profil. |
| `/api/v1/providers/artifacts` | `GET` | Liefert Local-Built-in/Local-Provider-Artefakte und Download/Pull-Jobs. |
| `/api/v1/providers/artifacts/{artifactId}/download` | `POST` | Startet Download oder Pull eines Provider-Artefakts. |
| `/api/v1/providers/artifacts/{artifactId}/select` | `POST` | Waehlt ein vorhandenes Provider-Artefakt aus. |
| `/api/v1/providers/{profileId}/activate` | `POST` | Aktiviert ein Profil fuer seinen Modus und persistiert die Auswahl. |

Mode-Aliase werden an der API normalisiert: `dictation`, `dictate`, `transcribe`, `assist`, `voice_agent`, `voiceAgent`, `voice-agent`.

GET-Routen dienen lokaler Introspection. Mutierende `PATCH`- und `POST`-Routen verlangen den Control-Plane-Token ueber Header oder Cookie.

#### Per-Mode Intelligence Settings

Die API bildet die drei Intelligence-Komponenten als steuerbare Settings ab:

| Modus | Settings |
|-------|----------|
| Dictation | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `dictionaryEnabled` |
| Assist | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `ttsEnabled`, `utilityRegistry` |
| Voice Agent | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `sessionSummary`, `pipelineFallback`, `closeBehavior` |

Damit koennen externe Tools z.B. Dictation lokal und text-only halten, Assist auf einen schnellen Utility-Provider stellen und Voice Agent auf Gemini Live oder einen Pipeline-Fallback schalten, ohne die Desktop UI zu automatisieren.

#### Readiness Model

Provider Readiness ist bewusst mehrteilig:

- `configured`: Profil ist in Config und Runtime sinnvoll adressierbar.
- `credentialsReady`: benoetigte Secrets oder Tokens sind vorhanden oder nicht erforderlich.
- `runtimeReady`: lokaler Runtime-Pfad, lokaler Server oder Build-Faehigkeit ist vorhanden.
- `capabilityReady`: Profil erfuellt den Mode Contract.
- `ready`: alle obigen Bedingungen sind erfuellt.

Ab v0.23.1 enthaelt jedes Readiness-Item `schemaVersion = provider-readiness.v1`, `requirements`, `actions` und `artifacts`. Damit koennen Host-Tools fehlende Schritte wie Credential-Konfiguration, Local-Runtime-Installation, Modell-Download oder Modell-Auswahl maschinenlesbar anzeigen und ausloesen.

Die API darf nicht versuchen, fehlende Secrets zu erraten. Sie benennt stattdessen fehlende Voraussetzungen, damit Host-Tools eigene Setup-Flows anbieten koennen.

Artefakte sind ebenfalls zweigeteilt: `internal/downloads.ArtifactCatalog()` beschreibt statische Downloads/Pulls, waehrend ein separater Status-Resolver Verfuegbarkeit, Auswahl und optionale Runtime-Probes ermittelt. `/api/v1/providers/readiness` nutzt diese Trennung, damit ein Readiness-Read keine unnoetigen lokalen Netzwerk-Probes ausloest.

### Namensgebung

- **Dictation Mode** — Reine Sprache-zu-Text-Transkription.
- **Assist Mode** — Einmalige intelligente Verarbeitung. Codeword-Erkennung + LLM-Antwort mit Sprachausgabe. Gibt immer **Text + Audio** zurueck — das Framework liefert beides, die UI entscheidet, wie es dargestellt wird (z.B. Sprechblase + TTS, nur Text, nur Audio).
- **Voice Agent Mode** — Dauerhafter Companion mit kontinuierlichem Zuhoeren, Konversationsgedaechtnis und proaktivem Verhalten. Gibt ebenfalls **Text + Audio** zurueck.

### Modus-Exklusivitaet

Alle drei Modi haben jetzt eigene Hotkeys. Ein leerer Hotkey deaktiviert den Modus vollständig. `active_mode` darf `none` sein, damit kein Modus manuell vorselektiert ist.

```
dictate_hotkey     → immer Dictation Mode
assist_hotkey      → immer Assist Mode
voice_agent_hotkey → immer Voice Agent Mode
active_mode        → none | dictate | assist | voice_agent
```

Bubble-Hover und Dot-Kontextmenue zeigen nur die konfigurierten Modi. Ein Klick auf den aktiven Modus schaltet sauber auf `none` zurueck.

Wenn `overlay_movable = true`, speichert SpeechKit die freie Overlay-Position monitorbezogen, damit ein Wechsel des aktiven Monitors die zuletzt genutzte Position pro Display wiederherstellen kann.

---

## Mode 1: Dictation / Transcribe (User Intelligence)

```
Hotkey(PTT) → Record → VAD Auto-Stop → Audio → Transcription Adapter
  → User Dictionary / Phrase Hints / Provider Hints
  → Text → Deterministic Corrections → Output(clipboard/paste)
```

V23 erweitert Dictation um einen first-class User-Kontext. Schwer erkennbare Woerter, Namen, Fachbegriffe und benutzerdefinierte Schreibweisen werden als Dictionary gepflegt. Provider bekommen diese Information nativ, wenn ihr Adapter Phrase Hints oder Custom Vocabulary unterstuetzt. Sonst nutzt SpeechKit Prompt-Hints und deterministische Nachkorrekturen.

Dictation bleibt trotz Local Provider und Ollama strikt text-only: Audio rein, finaler Transkriptionstext raus. Kein Codeword, kein Assist Tool, keine Antwortgenerierung.

**Komponenten:**
- Audio Capture (malgo WASAPI)
- VAD (Silero ONNX)
- Transcription Adapter Layer ueber STT Router und provider-spezifischen Adaptern
- User Dictionary mit strukturierten Entries, Prompt-Hints und Korrekturen
- STT Providers / Adapters (whisper.cpp, Ollama, HF, Direct Provider)
- Output (Clipboard + Ctrl+V)
- Overlay UI (Pill/Dot/Radial)

---

## Mode 2: Assist (Utility Intelligence)

### Flow

```
Hotkey(PTT) → Record → VAD Auto-Stop → Audio → STT Router → Transcript
  → ShortcutResolver.Resolve(transcript)
    → MATCH:  Execute Action (copy, insert, summarize, custom...)
              → AssistResult { Text, Audio, Action }
    → NO MATCH: LLM Utility Flow(transcript, context)
              → AssistResult { Text, Audio, Action }
  → Framework liefert IMMER: Text + Audio (TTS)
  → UI entscheidet Darstellung:
      - Sprechblase mit Text + TTS Playback
      - Nur TTS (minimale UI)
      - Nur Text (stummer Modus)
      - Clipboard Paste
```

Assist ist der einzige Modus fuer Utility-Ausfuehrung. Built-ins wie Zusammenfassung, E-Mail-Formatierung, Rewrite, Copy Last und Insert Last laufen ueber eine explizite Utility Registry. Exakte Codewords werden vor LLM-Routing aufgeloest. Das Ergebnis entscheidet ueber Panel, Aktion, Clipboard, Audio oder Silent Surface.

### Was existiert

| Komponente | Status | Datei |
|------------|--------|-------|
| Agent Hotkey | Done | `config.go` → `AgentHotkey` |
| Agent Flow (Genkit) | Done | `internal/ai/flows/agent.go` |
| Shortcut Resolver | Done | `internal/shortcuts/resolver.go` |
| Genkit Runtime | Done | `internal/ai/genkit.go` |
| Model Catalog | Done | `internal/models/catalog.go` |
| Utility + Agent Models | Done | 5 Providers konfiguriert |

### Was fehlt

| Komponente | Beschreibung | Aufwand |
|------------|--------------|---------|
| **TTS Provider Interface** | Analog zu `stt.Provider` — `Synthesize(ctx, text, opts) ([]byte, error)` | S |
| **TTS Providers** | OpenAI TTS, Google Cloud TTS, Kokoro Local, VPS | M |
| **TTS Router** | Analog zu STT Router — Local-First, Cloud-Fallback | S |
| **Audio Playback** | Windows Audio Output (WASAPI Render via malgo oder `oto`) | M |
| **Erweiterte Codewords** | Mehr Intents: open_app, set_timer, web_search, custom user commands | S |
| **Sprache-Detection** | Locale aus STT-Ergebnis oder erstem Utterance ableiten | S |
| **Config: TTS Section** | `[tts]` Block in config.toml | S |
| **Config: Assist Section** | TTS-enabled, response_display, codeword_list | S |

### Genkit Flow: Assist

```go
// internal/ai/flows/assist.go — NEU
type AssistInput struct {
    Utterance     string `json:"utterance"`
    Locale        string `json:"locale"`
    Selection     string `json:"selection,omitempty"`
    Context       string `json:"context,omitempty"` // Last transcription, aktives Fenster
}

// AssistResult ist das Framework-Ergebnis — liefert IMMER Text + Audio.
// Die UI entscheidet, was davon angezeigt/abgespielt wird.
type AssistResult struct {
    Text      string `json:"text"`          // Vollstaendige LLM Response (immer vorhanden)
    Audio     []byte `json:"audio"`         // TTS Audio Bytes (immer vorhanden wenn TTS enabled)
    SpeakText string `json:"speakText"`     // TTS-optimierter Text (kuerzer, natuerlicher als Text)
    Action    string `json:"action"`        // "respond", "execute", "silent"
    Locale    string `json:"locale"`        // Antwortsprache
}

// AssistOutput ist der Genkit Flow Output (vor TTS-Synthese).
type AssistOutput struct {
    Text      string `json:"text"`          // LLM Response
    SpeakText string `json:"speakText"`     // TTS-optimiert
    Action    string `json:"action"`        // "respond", "execute", "silent"
    Locale    string `json:"locale"`        // Antwortsprache
}
```

**Framework-Prinzip: Text + Audio immer verfuegbar.**
Die Pipeline produziert immer `AssistResult` mit Text UND Audio. Die UI-Schicht (Overlay, Sprechblase, CLI, etc.) entscheidet, was davon dem User praesentiert wird. Das haelt das Framework flexibel fuer verschiedene UI-Implementierungen.

Der Assist Flow unterscheidet sich vom bestehenden Agent Flow:
- **Agent Flow** (bestehend): Multi-Step Reasoning mit Tools. Bleibt fuer komplexe Aufgaben.
- **Assist Flow** (neu): Schnelle, einmalige Antwort. Optimiert auf Latenz. Utility Models.
- Der Assist Flow nutzt Utility Models (GPT-4o Mini, Gemini Flash Lite, Groq LLaMA 8B) — schnell und guenstig.
- Der Agent Flow (fuer komplexe Fragen) bleibt verfuegbar als Fallback bei erkannter Komplexitaet.

---

## Mode 3: Voice Agent (Brainstorming Intelligence) — Native Real-Time Audio

### Voice Agent Standard

Der Framework-Standard fuer den Voice Agent ist eine **direkte Gemini-Live-Integration ueber `google.golang.org/genai`**. Genkit bleibt fuer nicht-realtime Flows relevant: fuer Voice-Agent-Session-Summaries und fuer Pipeline-Fallbacks, wenn der ausgewaehlte Provider kein natives Realtime-Audio bereitstellt.

V23 ergaenzt den Voice Agent um strukturierte Session-Zusammenfassungen. Brainstorming- und Konzeptdialoge werden in Summary, Ideen, Entscheidungen, offene Fragen und naechste Schritte aufbereitet. Die Summary kann ueber den Utility/Assist-Fallback laufen und ist nicht an das native Realtime-Modell gebunden.

Der Host integriert den Voice Agent ueber genau diese Inputs:

- `APIKey` fuer Gemini Live
- `Instruction` als optionaler Ablauf- oder Rollen-Guide
- Session-Policies fuer Thinking, serverseitige VAD/Turn Detection, Transkription und Context Compression

Wenn der Host **keine eigene Instruction** mitgibt, verwendet SpeechKit eine allgemeine Default-Instruction fuer hilfreiche, knappe Unterstuetzung. Damit bleibt der Voice Agent sofort nutzbar, ohne dass jede Integration zuerst einen Prompt entwerfen muss.

Das Framework soll damit dieselbe Runtime fuer unterschiedliche Host-Szenarien tragen:

- Desktop-Dialoge mit Ergebnisextraktion und Zusammenfassung
- Spielmoderation mit host-spezifischer Instruction
- Produktdialoge mit domaenenspezifischen Host-Aktionen

Die Host-spezifische Steuerung passiert ueber `Instruction` und optionale Tool-/Action-Seams, nicht ueber einen zweiten Realtime-Modus.

### Architektur-Paradigma: Native Audio-to-Audio plus Pipeline-Fallback

Voice Agent bevorzugt **Native Real-Time Models**, die Audio direkt verarbeiten und Audio zurueckgeben. Das eliminiert die Latenz von drei separaten API-Calls und liefert sub-sekuendige Antwortzeiten.

Nicht jeder V23 Provider kann natives Realtime-Audio. Local Built-in, Ollama und Hugging Face sind deshalb gueltige Voice-Agent-Optionen als **Pipeline-Fallback**: STT erfasst den Turn, der Agent Flow erzeugt die Antwort, optionales TTS gibt sie aus, und die Session Summary wird wie bei nativen Sessions gepflegt.

```
Activate(Toggle-Hotkey) →
  ┌─ WebSocket Session zu Real-Time Model ─────────────────────┐
  │                                                              │
  │  Audio Capture → PCM 16kHz → SendRealtimeInput(audio)       │
  │                                                              │
  │  Model: Server-seitige VAD + Turn Detection                 │
  │    → Model erkennt End-of-Turn automatisch                   │
  │    → Audio Response streamt zurueck (PCM 24kHz)              │
  │    → Audio Playback (sofort, chunk-weise)                    │
  │    → Optional: Text-Transkript parallel verfuegbar           │
  │    → Reset Idle Timer                                        │
  │                                                              │
  │  Barge-In: User spricht waehrend Model antwortet             │
  │    → Model stoppt Audio-Output automatisch                   │
  │    → Verarbeitet neuen User-Input                            │
  │                                                              │
  │  Idle Timer:                                                 │
  │    → Nach reminder_after_idle (default 5min):                │
  │      Model-Prompt: "Remind the user you're still here"       │
  │      → Audio Response in Konversationssprache                │
  │    → Nach deactivate_after_idle (default 15min):             │
  │      Model-Prompt: "Say goodbye, session ending"             │
  │      → Deactivate + WebSocket close                          │
  │                                                              │
  │  Deactivate: Toggle-Hotkey ODER Idle-Timeout                 │
  │    → WebSocket Session schliessen                            │
  │    → Audio Capture stoppen                                   │
  └──────────────────────────────────────────────────────────────┘
```

### Real-Time Models

| Modell | Model ID | Provider | Latenz | Preis | Rolle |
|--------|----------|----------|--------|-------|-------|
| **Gemini 2.5 Flash Native Audio** | `gemini-2.5-flash-native-audio-preview-12-2025` | Google | Sub-Sekunde | TBD | **Current Default** |
| **Gemini 3.1 Flash Live** | `gemini-3.1-flash-live-preview` | Google | Sub-Sekunde | TBD (Preview) | Explicit preview candidate |
| **OpenAI gpt-realtime-mini** | `gpt-realtime-mini` | OpenAI | ~300ms | ~$0.02/min in, ~$0.08/min out | Guenstiger Fallback |
| **OpenAI gpt-realtime** | `gpt-realtime` | OpenAI | ~300ms | ~$0.06/min in, ~$0.24/min out | Qualitaets-Fallback |
| **Groq Pipeline** | STT+LLM+TTS | Groq | ~500-800ms | Guenstigste Option | Budget-Fallback |

**Warum Gemini 2.5 Flash Native Audio als aktueller Default:**
- Native Audio-to-Audio (kein STT→LLM→TTS Umweg)
- Server-seitige VAD + Turn Detection (weniger Client-Logik)
- Barge-In nativ unterstuetzt
- Go SDK (`google.golang.org/genai` v1.52+) hat vollstaendige Live API Unterstuetzung
- 30 HD Voices, 24 Sprachen
- Affective Dialog (erkennt Emotionen im Tonfall)

**Fallback-Strategie:**
1. Gemini 2.5 Flash Native Audio → Wenn nicht verfuegbar:
2. Gemini 3.1 Flash Live nur wenn explizit konfiguriert → Wenn nicht verfuegbar:
3. OpenAI gpt-realtime-mini (guenstiger als gpt-realtime) → Wenn nicht verfuegbar:
4. Groq Pipeline Fallback (STT: whisper-large-v3-turbo + LLM: llama-3.1-8b-instant + TTS: PlayAI Dialog)

Der Groq Pipeline Fallback nutzt die bestehende STT→LLM→TTS Architektur als Notloesung wenn kein nativer Real-Time Provider verfuegbar ist.

### Go SDK Integration: Gemini Live API

```go
// internal/voiceagent/live_session.go — NEU
import "google.golang.org/genai"

type LiveSession struct {
    session    *genai.Session
    client     *genai.Client
    idleTimer  *IdleTimer
    locale     string
    onText     func(text string)   // Callback fuer Text-Output (optional, fuer UI)
    onAudio    func(audio []byte)  // Callback fuer Audio-Output
}

func (ls *LiveSession) Start(ctx context.Context, apiKey string) error {
    client, _ := genai.NewClient(ctx, &genai.ClientConfig{
        APIKey: apiKey,
        Backend: genai.BackendGeminiAPI,
    })
    ls.client = client

    config := &genai.LiveConnectConfig{
        ResponseModalities: []genai.Modality{genai.ModalityAudio, genai.ModalityText},
        SpeechConfig: &genai.SpeechConfig{
            VoiceConfig: &genai.VoiceConfig{
                PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
                    VoiceName: "Kore", // Default Voice
                },
            },
        },
        SystemInstruction: &genai.Content{
            Parts: []genai.Part{genai.Text(ls.systemPrompt())},
        },
    }

    session, _ := client.Live.Connect(ctx, "gemini-2.5-flash-native-audio-preview-12-2025", config)
    ls.session = session
    return nil
}

// SendAudio streamt PCM-Chunks zum Model (16kHz, 16-bit, little-endian).
func (ls *LiveSession) SendAudio(chunk []byte) error {
    return ls.session.SendRealtimeInput(genai.LiveRealtimeInput{
        Audio: &genai.Blob{
            MIMEType: "audio/pcm;rate=16000",
            Data:     chunk,
        },
    })
}

// ReceiveLoop empfaengt Audio + Text Responses vom Model.
func (ls *LiveSession) ReceiveLoop(ctx context.Context) {
    for {
        msg, err := ls.session.Receive(ctx)
        if err != nil { return }

        for _, part := range msg.ServerContent.ModelTurn.Parts {
            if part.InlineData != nil {
                // Audio-Chunk empfangen → sofort abspielen
                ls.onAudio(part.InlineData.Data)
            }
            if part.Text != "" {
                // Text-Transkript empfangen → optional an UI weiterleiten
                ls.onText(part.Text)
            }
        }
        ls.idleTimer.Reset()
    }
}
```

### Text-Output im Voice Agent

Real-Time Models liefern Audio als primaeren Output. Text ist **optional und nicht immer vollstaendig** bei nativen Audio-Modellen. Strategie:

| Szenario | Text verfuegbar? | Verhalten |
|----------|-------------------|-----------|
| Gemini Live mit `ModalityText` | Ja, parallel zum Audio | Text in UI anzeigen (Sprechblase) |
| OpenAI gpt-realtime | Ja, als Transkript | Text in UI anzeigen |
| Groq Pipeline Fallback | Ja, vollstaendig | Text immer verfuegbar |
| Komplexe Anfrage (User will Text) | Bei Bedarf | User kann per Codeword "zeig mir das" Text-Output erzwingen |

Wenn der User explizit eine textuelle Antwort braucht (z.B. Code, Liste, Adresse), kann ein staerkeres Nicht-Realtime-Model herangezogen werden. Das Real-Time Model erkennt dies und delegiert.

### Conversation State Machine

```
INACTIVE → [Hotkey] → CONNECTING (WebSocket) → LISTENING
  ↑                                                 ↑
  │                              Audio-in ↓          │
  │                            MODEL PROCESSING      │
  │                              Audio-out ↓         │
  │                              SPEAKING ───────────┘
  │                              (Barge-In → LISTENING)
  │
  └── DEACTIVATING ← [Timeout/Hotkey] → WebSocket Close
```

Vereinfacht gegenueber der STT→LLM→TTS Pipeline: Das Model uebernimmt VAD, Turn Detection und Antwortgenerierung. Der Client muss nur Audio streamen und empfangen.

### Was fehlt

| Komponente | Beschreibung | Aufwand |
|------------|--------------|---------|
| **Live Session Manager** | WebSocket Lifecycle, Reconnect, Model-Auswahl | M |
| **Gemini Live Provider** | `google.golang.org/genai` Live API Integration | M |
| **OpenAI Realtime Provider** | WebSocket Client fuer gpt-realtime API | M |
| **Groq Pipeline Fallback** | STT→LLM→TTS Fallback mit bestehenden Providern | S |
| **Audio Streaming Bridge** | Mic PCM → WebSocket Send + WebSocket Receive → Speaker | M |
| **Idle Timer Manager** | Reminder + Auto-Deactivate, konfigurierbar | S |
| **Language/Voice Config** | Sprache + Voice aus erster Aeusserung / Config | S |
| **Config: Voice Agent** | `[voice_agent]` Block: Model, Timeouts, Fallback | S |
| **Overlay: Companion UI** | Persistente Anzeige: Listening/Speaking/Thinking | M |

### Idle Timer

```go
type IdleTimer struct {
    reminderAfter    time.Duration // Default: 5 Minuten
    deactivateAfter  time.Duration // Default: 15 Minuten
    session          *LiveSession
    locale           string
}
```

- **Reminder:** Nach `reminder_after_idle` sendet der Client einen System-Prompt an das Model: "The user has been silent for 5 minutes. Gently ask if they need anything, in {locale}." Das Model generiert die Erinnerung als Audio.
- **Deactivate:** Nach `deactivate_after` sendet der Client: "Say a brief goodbye, the session is ending." Danach wird die WebSocket-Session geschlossen.

---

## TTS Architecture (Neu — benoetigt fuer Assist + Voice Agent)

### Provider Interface

```go
// internal/tts/provider.go — NEU
type Provider interface {
    Synthesize(ctx context.Context, text string, opts SynthesizeOpts) ([]byte, error)
    Name() string
    Health(ctx context.Context) error
}

type SynthesizeOpts struct {
    Locale string // "de-DE", "en-US"
    Voice  string // Provider-spezifische Voice ID
    Speed  float64 // 0.5 - 2.0, default 1.0
    Format string // "wav", "mp3", "opus"
}
```

### Providers

| Provider | Typ | Modell | Latenz | Qualitaet | Status |
|----------|-----|--------|--------|-----------|--------|
| **OpenAI TTS** | Cloud | `tts-1` / `tts-1-hd` | ~500ms | Hoch | Prio 1 |
| **Google Cloud TTS** | Cloud | WaveNet / Neural2 | ~400ms | Hoch | Prio 2 |
| **Kokoro 82M** | Local | `hexgrad/Kokoro-82M` | ~200ms | Mittel | Prio 3 |
| **VPS TTS** | Self-Hosted | Kokoro Docker | ~300ms | Mittel | Prio 4 |
| **ElevenLabs** | Cloud | Multilingual v2 | ~600ms | Sehr hoch | Optional |

### TTS Router

```go
// internal/tts/router.go — NEU
type Strategy string
const (
    TTSStrategyCloudFirst Strategy = "cloud-first" // Default: OpenAI/Google fuer Qualitaet
    TTSStrategyLocalFirst Strategy = "local-first" // Kokoro fuer Geschwindigkeit
    TTSStrategyCloudOnly  Strategy = "cloud-only"
    TTSStrategyLocalOnly  Strategy = "local-only"
)
```

- **Assist Mode:** Cloud-First (Qualitaet wichtiger, einmalige Antwort).
- **Voice Agent Mode:** TTS Router wird NICHT genutzt — Real-Time Models liefern Audio nativ. Nur der Groq Pipeline Fallback nutzt den TTS Router.
- Fallback-Kette: Primary → Secondary → Tertiary.

### Audio Playback

```go
// internal/audio/player.go — NEU
type Player interface {
    Play(ctx context.Context, audio []byte, format string) error
    Stop() error            // Fuer Barge-In
    IsPlaying() bool
    OnFinished(fn func())   // Callback wenn Playback endet
}
```

- Windows: WASAPI Render via `oto` (pure Go, kein CGo) oder `beep` Library.
- Format: PCM 24kHz Mono (OpenAI TTS native) oder 22050Hz (Kokoro native).
- Resampling falls noetig.

---

## Config V2 Erweiterungen

```toml
# config.toml — Neue Sections

[general]
dictate_hotkey = "ctrl+win"      # Immer Dictation Mode
assist_hotkey = "win+alt"        # Immer Assist Mode
voice_agent_hotkey = "ctrl+shift" # Immer Voice Agent Mode

[tts]
enabled = true
strategy = "cloud-first"         # "cloud-first" | "local-first" | "cloud-only" | "local-only"
voice = ""                       # Provider-spezifisch, leer = Default
speed = 1.0
format = "wav"

[tts.local]
enabled = false
model = "hexgrad/Kokoro-82M"
model_path = ""
port = 8081

[tts.openai]
enabled = true                   # Nutzt providers.openai.api_key_env
model = "tts-1"                  # "tts-1" | "tts-1-hd"
voice = "nova"                   # alloy, echo, fable, onyx, nova, shimmer

[tts.google]
enabled = false                  # Nutzt providers.google.api_key_env
voice = "de-DE-Neural2-B"

[voice_agent]
enabled = true
model = "gemini-2.5-flash-native-audio-preview-12-2025"  # Real-Time Model
fallback_model = "gpt-realtime-mini"      # Fallback wenn Primary nicht verfuegbar
voice = "Kore"                            # Gemini Voice Name
reminder_after_idle_sec = 300             # 5 Minuten
deactivate_after_idle_sec = 900           # 15 Minuten
pipeline_fallback = true                  # STT->Agent LLM->TTS fuer nicht-native Voice-Agent-Profile
```

---

## Genkit Integration: Gesamtbild

### Bestehende Flows (Bleiben)

| Flow | Zweck | Models | Datei |
|------|-------|--------|-------|
| `summarize` | Text zusammenfassen | Utility | `flows/summarize.go` |
| `agent` | Multi-Step Reasoning mit Tools | Agent | `flows/agent.go` |

### Neue Flows

| Flow | Zweck | Models | Datei |
|------|-------|--------|-------|
| `assist` | Schnelle einmalige Antwort | Utility | `flows/assist.go` |
| `codeword` | Codeword-Erkennung via LLM (optional) | Utility | `flows/codeword.go` |

Native Direct Voice-Agent-Profile nutzen keinen Genkit Flow fuer die laufende Audio-Session; die Live API ist eine persistente WebSocket Session. Pipeline-Fallback-Profile und Session-Summaries nutzen dagegen bewusst die bestehenden Genkit Agent/Utility Flows.

### Model-Zuordnung

| Aufgabe | Model-Tier | Default | Fallbacks | Warum |
|---------|------------|---------|-----------|-------|
| Codeword Check | Pattern Matching | Shortcut Resolver | — | Latenz: 0ms |
| Assist Response | Utility LLM | `gpt-4o-mini` | Gemini Flash Lite, Groq LLaMA 8B, Qwen 9B | Schnell, guenstig |
| Voice Agent | **Real-Time** | `gemini-2.5-flash-native-audio-preview-12-2025` | `gpt-realtime-mini`, Groq Pipeline | Sub-Sekunde, nativ |
| Summarize | Utility | Gleich wie Assist | — | Schnell |
| TTS (nur Assist) | TTS-spezifisch | OpenAI `tts-1` | Google Neural2, Kokoro 82M | Sprachqualitaet |
| STT (nur Dictation+Assist) | STT-spezifisch | Whisper (HF Routed) | Groq, OpenAI, Google Chirp | Transkriptionsqualitaet |

**Wichtig:** Native Direct Voice Agent Profile nutzen weder den STT Router noch den TTS Router — das Real-Time Model uebernimmt beides nativ. Local Built-in, Ollama und Hugging Face Voice-Agent-Profile duerfen dagegen bewusst den STT->Agent->TTS Pipeline-Fallback nutzen.

### Provider Config → Genkit Model Mapping

Die bestehende `ProvidersConfig` wird erweitert:

```toml
[providers.openai]
enabled = true
api_key_env = "OPENAI_API_KEY"
stt_model = "gpt-4o-transcribe"
utility_model = "gpt-4o-mini"
realtime_model = "gpt-realtime-mini"  # NEU — fuer Voice Agent Fallback
tts_model = "tts-1"                   # NEU — fuer Assist Mode
tts_voice = "nova"                    # NEU
```

---

## Product Integration Boundary

SpeechKit bleibt ein eigenstaendiges Open-Source-Framework mit Windows-Referenzapp
und optionalem Server-Target. Kombify-Produkte duerfen dieses Framework spaeter
downstream wiederverwenden, aber host-produkt-spezifische Themen gehoeren nicht
in den SpeechKit-Core.

### Nicht Teil des SpeechKit-Cores

| Thema | Warum nicht im Core |
|------------|--------------|
| Account-Login, Device-Onboarding und Account-Sync | Produktidentitaet und Login-Flows sind Host-App-Verantwortung. |
| Private Host-Packages | SpeechKit muss aus einem sauberen OSS-Checkout buildbar bleiben. |
| Produktseitige Feature-Gates, Preisplaene und Nutzungsmessung | Diese Regeln sind Produkt-Policy, nicht Framework-Contract. |
| Admin-Model-Kataloge oder zentrale Feature-Flag-Backends | Der Core kennt Provider-Profile und lokale/serverseitige Config, aber keine hosted Product-Control-Plane. |

### Was der Core bereitstellt

- Mode-Contracts fuer Dictation, Assist und Voice Agent.
- Provider-Profile, Routing-Policy und Readiness-Metadaten.
- Lokale Credential-/Secret-Hierarchie fuer Provider-Keys.
- Server-Auth-Modi fuer Deployment-Schutz (`bearer`, `edge_hmac`, `bearer_or_edge`, `none` fuer lokale Entwicklung).
- Store- und Feature-Extension-Seams, die ohne private Dependencies inert bleiben.

Downstream-Produktmodule koennen eigene Login-, Account-, Abrechnungs- oder
Cloud-Sync-Logik ueber diese Seams registrieren. Diese Module sind separate
Integrationen und duerfen nicht als Voraussetzung fuer das Framework, den
Windows-Client oder die OSS-Release-Doku beschrieben werden.

---

## Shortcut/Codeword System (Erweiterung)

### Bestehende Intents

| Intent | Aliases | Status |
|--------|---------|--------|
| `copy_last` | "copy last", "copy last transcription" | Done |
| `insert_last` | "insert last", "insert last transcription" | Done |
| `summarize` | "summarize", "zusammenfassen", etc. | Done |

### Neue Intents (Assist + Voice Agent)

| Intent | Aliases (DE/EN) | Aktion |
|--------|-----------------|--------|
| `open_app` | "oeffne [App]", "open [App]" | App starten via Windows API |
| `web_search` | "suche nach [X]", "search for [X]" | Browser mit Suche oeffnen |
| `set_reminder` | "erinnere mich [X]", "remind me [X]" | System-Notification |
| `read_clipboard` | "lies Zwischenablage", "read clipboard" | Clipboard → TTS |
| `translate` | "uebersetze [X]", "translate [X]" | LLM Translation → TTS |
| `dictate_email` | "schreibe Email an [X]", "write email to [X]" | Strukturierte Email-Erfassung |
| `quick_note` | "notiz [X]", "note [X]" | Quick Note speichern |

### Codeword-Erkennung: Zwei Stufen

1. **Pattern Matching** (bestehend, `shortcuts/resolver.go`): Exakte Alias-Matches. Latenz: 0ms.
2. **LLM Codeword Flow** (neu, optional): Fuer unscharfe Matches. Utility Model erkennt Intent aus natuerlicher Sprache. Latenz: ~200-500ms.

Stufe 2 wird nur aktiviert wenn Pattern Matching keinen Treffer hat UND der Assist/Voice Agent Mode aktiv ist.

---

## Release Status v0.23.1

### Implementiert

- V23 Framework SDK Boundary in `pkg/speechkit`
- Versionierte `/api/v1` Local Control Plane fuer Modi, Settings, Provider, Readiness und Artefakte
- Dictation mit Whisper.cpp Local Built-in, Hugging Face, OpenAI, Groq, Google und VPS
- Assist Pipeline mit Shortcut/Codeword-Erkennung, LLM-Routing, optionalem TTS und Panel-Surface
- Voice Agent mit Gemini Live, Session Prompts, Live Transcript Surface und Pipeline-Fallback-Grenze
- TTS Provider, Router und Audio Playback fuer Assist/Voice-Agent-Ausgabe
- Provider Catalog, Model Selection, Readiness Requirements, Download Jobs und Local/Provider Artifact Actions
- Windows Credential Manager und host-managed Provider Secrets; keine plaintext fallback secret store fuer Releases

### Bewusste Release-Grenzen

- Local Built-in Dictation ist der einzige voll gebuendelte lokale Runtime-Pfad in v0.23.1.
- Local Built-in Assist und Voice Agent liefern GGUF-Modelldateien, setzen aber einen separat erreichbaren OpenAI-kompatiblen lokalen LLM-Server voraus.
- Native Voice Agent nutzt aktuell `gemini-2.5-flash-native-audio-preview-12-2025` als empfohlenen Default; andere Live-Modelle muessen explizit konfiguriert werden.
- Der Public Release muss aus `kombifyio/SpeechKit` gegen den letzten vorhandenen Public Tag verglichen werden, nicht gegen eine nicht veroeffentlichte Zwischenversion.

---

## Risiken und Entscheidungen

### Aktuelle Entscheidungen

| # | Frage | Entscheidung |
|---|-------|--------------|
| 1 | Audio Playback Library | `oto` v3 ist der aktuelle Playback-Pfad. |
| 2 | Lokales TTS (Assist) | Lokale TTS-Routen bleiben optional und sind kein v0.23.1 Default-Versprechen. |
| 3 | Voice Agent Primary | Gemini Live bleibt die native Voice-Agent-Schicht; der aktuelle Default ist `gemini-2.5-flash-native-audio-preview-12-2025`. |
| 4 | Barge-In Implementierung | Native Model-/Provider-Unterstuetzung hat Vorrang; Pipeline-Fallbacks bleiben explizit begrenzt. |

### Bekannte Risiken

| Risiko | Impact | Mitigation |
|--------|--------|------------|
| Gemini Live Preview Stabilitaet | Session-Abbrueche | OpenAI gpt-realtime-mini als Fallback |
| WebSocket Verbindungsverlust | Voice Agent Unterbrechung | Auto-Reconnect mit Session Resume |
| Kosten bei langem Voice Agent | Providerkosten | Session-Timeout (default 15min), User-Warnung |
| Gleichzeitige Audio Ein-/Ausgabe | Echo/Feedback | Real-Time Models handlen das serverseitig |
| API Rate Limits bei Dauerbetrieb | Service-Unterbrechung | Provider-Rotation, Session-Limits |
| Gemini Live Model-Wechsel | Alte Model IDs werden deprecated | Model-ID in Config, nicht hardcoded |

---

## Zusammenfassung: Was ist Done vs Release-Grenze

### Done (kann sofort genutzt werden)

- Audio Capture + VAD
- STT (6 Provider, Router)
- LLM Integration (Genkit, Provider-Routing, Assist und Agent Flows)
- Shortcut/Codeword System
- Desktop Output (Clipboard, Paste)
- Config System (TOML)
- Credential Management (Windows Credential Manager; external secret managers optional)
- Overlay UI (Pill, Dot, Radial)
- System Tray
- Model Catalog
- Backend: AI Model Katalog, Inference Providers, Feature Flags
- OpenAPI contract and public framework API docs

### Nicht als fertig bewerben

| Bereich | Release-Grenze |
|---------|----------------|
| Local Built-in Assist | GGUF Download/Selection ist vorhanden; lokaler LLM Server muss separat laufen. |
| Local Built-in Voice Agent | Pipeline-Modell ist waehlbar; voll gebuendelte Voice-Agent-Runtime ist noch kein v0.23.1-Versprechen. |
| Offline TTS | Cloud TTS und optionale lokale Routen existieren, aber kein als Default gebuendelter Offline-TTS-Pfad. |
| Release Assets | Der finale Installer wird im Public-Release-Workflow mit NSIS gebaut und signiert; lokale `-SkipInstaller` Builds sind nur Preflight. |
