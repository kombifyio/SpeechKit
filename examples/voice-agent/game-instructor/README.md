# Voice Agent — Game Instructor (15 min)

End-to-end reference for embedding a SpeechKit Voice Agent into a third-party
Go program. The agent runs a fully realtime, audio-to-audio "game
instructor": it greets the player, explains rules, runs a 15-minute trivia
session, and wraps up with a final score.

This example is **the artifact a coding agent should adapt** when asked to
"build a voice agent into my app using SpeechKit." Everything below is the
single-prompt path.

## The single prompt

> Build a 15-minute voice-agent game instructor into my app using SpeechKit.
> Use `github.com/kombifyio/SpeechKit/pkg/speechkit/client` to talk
> to a running `speechkit-server`. Reuse the persona/role/sequence IDs
> `game-instructor` / `game-moderator` / `game-flow-15min` defined in
> `examples/voice-agent/game-instructor/config.example.toml`. Open a Voice Agent
> WebSocket, send the start frame, pump duplex audio + control frames, and
> exit cleanly at the 15-minute deadline or `session_end`.

That prompt plus this directory is sufficient. The example's `main.go`
delivers exactly that. Audio capture/playback is OS-specific and left to the
host: feed the session raw PCM 16 kHz S16LE mono via
`VoiceAgentSession.SendAudio` and consume the 24 kHz output from
`VoiceAgentMessage.Audio`.

## Layout

| File         | Purpose                                                                                       |
| ------------ | --------------------------------------------------------------------------------------------- |
| `config.example.toml`| TOML preset that seeds the Voice Agent persona, role, and 15-min sequence into the server.    |
| `main.go`    | Minimal embedder. Connects, ensures persona, opens WS, runs the dialogue loop with deadline.  |
| `README.md`  | This file.                                                                                    |

## Pre-flight

1. Start a self-hosted SpeechKit Server with `deploy/docker/docker-compose.oss.yml`, the published `ghcr.io/kombifyio/speechkit-server` image, or a custom image built from `deploy/docker/Dockerfile.server`.
2. Provide a Gemini Live API key (`GOOGLE_AI_API_KEY`) and a static bearer
   token (`SPEECHKIT_SERVER_TOKEN`) through your shell, CI secret store, or
   deployment environment.
3. Merge `config.example.toml` into the server's config or pass it via `--config`.

## Run

```bash
# Terminal 1 — server.
SPEECHKIT_SERVER_TOKEN=devtoken \
GOOGLE_AI_API_KEY=...           \
docker compose -f deploy/docker/docker-compose.oss.yml up -d

# Terminal 2 — embedder.
SPEECHKIT_SERVER_URL=http://localhost:8080 \
SPEECHKIT_SERVER_TOKEN=devtoken            \
go run ./examples/voice-agent/game-instructor

# Optional flags
#   --duration 5m       shorten the wall-clock cap (default 15m)
#   --bootstrap=false   skip the runtime persona upsert (seeded via TOML)
```

You should see, within a second or two:

```
session=… expires_at=…
[state=connecting]
[state=listening]
[sequence_step intro #0 → entered]
agent: Hey! Ready to play a quick trivia round? …
```

Type a turn and press Enter to feed text into the live model. An empty line
advances the sequence step (`advance_step` frame). `/quit` ends the session.

## Going voice

The session API is duplex from the first frame. To go from text demo →
fully voice:

1. Capture mic audio at 16 kHz mono S16LE (e.g. via malgo, miniaudio,
   PortAudio, sox).
2. Stream chunks with `session.SendAudio(ctx, chunk)`. ~20–40 ms chunks
   (640–1280 samples) keep latency low.
3. Render `msg.Audio` (24 kHz S16LE mono) through any audio sink (oto v3,
   beep, the OS default device).
4. Leave `automatic_activity_detection = true` on the role (as set in
   `config.example.toml`); the server handles turn boundaries and barge-in via
   Gemini Live's VAD.

## Knobs you usually want to tune

- **Session length**: `[server] voiceagent_idle_timeout_sec` caps a runaway
  session at the server. The example's `--duration` is the client-side
  deadline. Keep them aligned.
- **Number of turns**: `[[sequences]] max_turns` is the deterministic ceiling.
  Set lower than wall-clock for a snappier game.
- **Pace**: lower `temperature` for stricter moderation, raise for a livelier
  host. `thinking_level = "low"` keeps end-of-turn latency tight.
- **Voice**: Gemini Live voice names — see Google's
  `gemini-3.1-flash-live-preview` docs. The TOML defaults to `Puck`.

## Wire protocol (cheat sheet)

`pkg/speechkit/client/voiceagent_session.go` hides this, but if you ever
need to talk to the server without the SDK:

| Direction       | Type                 | Payload                                                                                       |
| --------------- | -------------------- | --------------------------------------------------------------------------------------------- |
| Client → Server | `start`              | `persona_id`, `role_id`, `sequence_id`, `locale`, `media_transport`, `system_prompt_override` |
| Client → Server | `text`               | `text` — injects a turn                                                                       |
| Client → Server | `audio_end`          | marks end of current mic turn (only when auto-VAD off)                                        |
| Client → Server | `advance_step`       | advances the sequence step                                                                    |
| Client → Server | `stop`               | graceful end                                                                                  |
| Client → Server | binary               | PCM 16 kHz S16LE mono                                                                         |
| Server → Client | `state`              | `state` — connecting / listening / processing / speaking / …                                  |
| Server → Client | `output_transcript`  | model speech in text                                                                          |
| Server → Client | `input_transcript`   | mic speech transcribed                                                                        |
| Server → Client | `sequence_step`      | step transition signal                                                                        |
| Server → Client | `error`              | `code`, `message`                                                                             |
| Server → Client | `session_end`        | `reason`                                                                                      |
| Server → Client | binary               | PCM 24 kHz S16LE mono                                                                         |

Full wire reference: [docs/server/asyncapi.v1.yaml](../../../docs/server/asyncapi.v1.yaml).
