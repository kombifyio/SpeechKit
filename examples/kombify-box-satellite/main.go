//go:build windows && cgo

// kombify box companion
//
// Testaufbau fuer die kombify-SpeechKit-Integration: die Waveshare
// ESP32-S3-Touch-LCD-1.85 ("kombify box") haengt als USB-Audiogeraet am PC,
// dieser Host lauscht lokal auf ein Wakeword (default: "hey jarvis";
// sherpa-onnx KWS ueber
// pkg/speechkit/wakeword), nimmt die Aeusserung auf, transkribiert sie
// (pkg/speechkit/stt), laesst sie vom kombify AI Gateway beantworten
// (assist.Generator) und spricht die Antwort ueber die Box (pkg/speechkit/tts).
//
// Die Runtime nutzt die oeffentlichen SpeechKit-Pakete (companion, wakeword,
// assist, stt, tts). Nur die Credential-Aufloesung kommt aus der normalen
// SpeechKit-App-Konfiguration, damit DPAPI/Doppler-Secrets nicht dupliziert
// werden muessen.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/go-mp3"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	companionskills "github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/companion"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword/sherpa"
)

func main() {
	cfgPath := "config.toml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// --- audio (host-owned, per SpeechKit hands-free contract) ---
	audio, err := NewAudioIO(cfg.Box.InputDeviceHint, cfg.Box.OutputDeviceHint)
	if err != nil {
		log.Fatal(err)
	}
	if err := audio.StartCapture(); err != nil {
		log.Fatal(err)
	}

	// --- CDC status link (Ring-UI der Box), optional ---
	boxLink, err := OpenBoxLink(cfg.Box.StatusPort)
	if err != nil {
		log.Printf("[boxlink] %v — Companion laeuft ohne Status-UI weiter", err)
	} else if boxLink != nil {
		defer boxLink.Close()
		defer boxLink.SetStage(companion.StageIdle)
		log.Printf("[boxlink] Status-UI verbunden")
		// Touch-Diagnose einmalig abfragen — bewusst VOR dem WLAN-
		// Provisioning, dessen Log-Burst den CDC-Mirror fluten kann
		// (usb_log.c droppt unter Last ganze Zeilen). Die Antwort kommt
		// best-effort ueber den Mirror als "[box] ... kbx_touch: status:";
		// fehlt sie, mit scripts\kbx-cmd.ps1 -Command "touch?" nachfragen.
		if err := boxLink.SendLine("KBX touch?"); err != nil {
			log.Printf("[boxlink] touch-diagnose: %v", err)
		}
		// WLAN-Provisioning der Box (Track B5): KOMBIFY_BOX_WIFI im
		// Format "ssid|pass" wird einmalig als CDC-Kommando gesendet;
		// die Firmware persistiert die Credentials in NVS.
		if creds := resolveCompanionSecret("KOMBIFY_BOX_WIFI"); creds != "" {
			if err := boxLink.SendLine("KBX wifi " + creds); err != nil {
				log.Printf("[boxlink] wifi provisioning: %v", err)
			} else {
				log.Printf("[boxlink] WLAN-Credentials an die Box gesendet")
			}
		}
	}

	targetMode := cfg.targetMode()
	var (
		sttProvider     stt.STTProvider
		handsFree       *companion.HandsFree
		voiceAgent      *voiceAgentRuntime
		localVoiceAgent *localVoiceAgentRuntime
	)

	if targetMode == "voice_agent" {
		if strings.EqualFold(cfg.VoiceAgent.Transport, "server") {
			voiceAgent, err = newVoiceAgentRuntime(cfg, audio)
			if err != nil {
				log.Fatal(err)
			}
			defer voiceAgent.Stop()
		} else {
			var stopAuthoritySTT func()
			if !localAuthoritySTTProvider(cfg.STT.Provider) {
				log.Printf("[voice_agent] host transcript authority requires local Whisper; configured STT %q is not trusted for realtime Home Assistant actions", cfg.STT.Provider)
			} else if cfg.sttReady() {
				sttProvider, err = buildSTT(cfg)
				if err == nil {
					stopAuthoritySTT, err = startLocalSTTIfNeeded(ctx, sttProvider)
				}
				if err != nil {
					log.Printf("[voice_agent] host transcript STT unavailable (%v); realtime Home Assistant actions remain fail-closed", err)
					sttProvider = nil
					stopAuthoritySTT = nil
				} else {
					log.Printf("[voice_agent] host transcript authority enabled through %s", sttProvider.Name())
				}
			} else {
				log.Printf("[voice_agent] host transcript STT unavailable; expected a Whisper model at %s; realtime Home Assistant actions remain fail-closed", whisperModelHint(cfg))
			}
			if stopAuthoritySTT != nil {
				defer stopAuthoritySTT()
			}
			localVoiceAgent, err = newLocalVoiceAgentRuntime(cfg, audio, boxLink, sttProvider)
			if err != nil {
				log.Fatal(err)
			}
			defer localVoiceAgent.Stop()
		}
	} else if targetMode != "wake_only" {
		// --- providers ---
		if !cfg.sttReady() {
			if cfg.localSTTProvider() {
				log.Printf("[config] warning: lokales STT ist nicht bereit; Whisper-Modell fehlt. Erwartet: %s", whisperModelHint(cfg))
			} else if cfg.directCloudSTTProvider() {
				log.Printf("[config] warning: STT %q ist nicht bereit; setze %s fuer direkte Transkription", cfg.STT.Provider, cfg.STT.APIKeyEnv)
			} else {
				log.Printf("[config] warning: STT gateway is not fully configured; set KOMBIFY_GATEWAY_BASE_URL and KOMBIFY_GATEWAY_TOKEN before real spoken turns can be transcribed")
			}
		}
		if !cfg.assistReady() {
			if cfg.localAssistProvider() {
				log.Printf("[config] warning: lokale LLM-Runtime ist nicht erreichbar; lokale Skills laufen, offene Fragen liefern einen Setup-Hinweis")
			} else {
				log.Printf("[config] warning: LLM gateway is not fully configured; local skills can run after STT, open-ended questions will return a setup hint")
			}
		}
		sttProvider, err = buildSTT(cfg)
		if err != nil {
			log.Fatal(err)
		}
		stopLocalSTT, err := startLocalSTTIfNeeded(ctx, sttProvider)
		if err != nil {
			log.Fatal(err)
		}
		defer stopLocalSTT()
		var ttsService *tts.Service
		if cfg.ttsReady() {
			ttsService, err = buildTTS(cfg)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			if cfg.localTTSProvider() {
				log.Printf("[config] warning: lokales Piper-TTS ist nicht bereit; Antworten werden geloggt, aber nicht gesprochen. Erwartet piper(.exe) und Stimmen in %s", cfg.TTS.Piper.VoiceDir)
			} else {
				log.Printf("[config] warning: TTS gateway is not fully configured; answers will be logged but not spoken until TTS is configured")
			}
		}
		// Assist routing: local companion skills and Home Assistant run first;
		// everything else falls through to the gateway LLM.
		skills := newCompanionSkillRouter(cfg, func(companionskills.Alarm) { audio.Ding() })
		defer skills.Close()
		assistOpts := assist.Options{
			Generator: newGatewayGenerator(cfg),
			Matcher:   skills,
			Executor:  skills,
		}
		log.Printf("companion skills aktiv: help/status + framework catalog (time/date/math/weather/timer/reminder/wikipedia/temperature)")
		if skills.homeAssistantConfigured() {
			log.Printf("home_assistant bridge aktiv (%s)", cfg.HomeAssistant.BaseURL)
		}
		assistService, err := assist.NewService(assistOpts)
		if err != nil {
			log.Fatal(err)
		}

		// --- SpeechKit runtime + hands-free composer ---
		runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
		defer runtime.Close()

		handsFree, err = companion.NewHandsFree(companion.Options{
			Runtime:    runtime,
			TargetMode: companion.TargetAssist,
			Assist:     assistService,
			TTS:        ttsService,
			// WakeRequest: host-eigene Aufnahme + STT. ok=false beendet den
			// Turn still (zu kurz, leer, STT-Fehler — Fehler landen im Log
			// und als EventErrorRaised auf dem Bus).
			WakeRequest: func(reqCtx context.Context, ev wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
				log.Printf("[wake] %q (keyword=%s)", ev.Phrase, ev.Keyword)
				audio.Ding()
				pcm := recordUtterance(reqCtx, cfg, audio)
				if len(pcm) < 16000 { // < 0.5 s
					log.Printf("[capture] zu kurz/leer - ignoriert")
					return speechkit.AssistRequest{}, false
				}
				log.Printf("[capture] accepted %d bytes - processing", len(pcm))
				go audio.CaptureAccepted()
				sttCtx, cancel := context.WithTimeout(reqCtx, 30*time.Second)
				defer cancel()
				res, err := sttProvider.Transcribe(sttCtx, wavFromPCM16(pcm, 16000), stt.TranscribeOpts{Language: cfg.STT.Language, Model: cfg.STT.Model})
				if err != nil {
					log.Printf("[stt] %v", err)
					return speechkit.AssistRequest{}, false
				}
				log.Printf("[stt] %q", res.Text)
				if strings.TrimSpace(res.Text) == "" {
					return speechkit.AssistRequest{}, false
				}
				return speechkit.AssistRequest{
					Text:       res.Text,
					Locale:     cfg.STT.Language,
					SessionKey: "kbx:" + boxSessionSerial(),
				}, true
			},
			// OnResult: synthetisierte Antwort auf dem Box-Speaker abspielen.
			OnResult: func(_ context.Context, result speechkit.AssistResult) {
				if result.ShortcutID != "" {
					log.Printf("[skill] %s action=%s text=%q", result.ShortcutID, result.Action, result.Text)
				} else {
					log.Printf("[llm] %q", result.Text)
				}
				if result.Audio.Len() > 0 {
					playResult(audio, result.Audio.Bytes(), result.Format)
				}
			},
			// OnStage: Ring-UI der Box ueber den CDC-Link treiben und den Turn
			// hoerbar machen — Fehlerton bei StageError, Endton ("verarbeitet,
			// Antwort kommt") direkt vor der Wiedergabe. Wake-Ding und
			// Accepted-Cue feuern bereits in WakeRequest.
			OnStage: func(s companion.Stage) {
				log.Printf("[stage] %s", s)
				boxLink.SetStage(s)
				switch s {
				case companion.StageError:
					_ = audio.PlayCue("error")
				case companion.StageSpeaking:
					_ = audio.PlayCue("done")
				}
			},
		})
		if err != nil {
			log.Fatal(err)
		}
		if err := handsFree.Start(ctx); err != nil {
			log.Fatal(err)
		}

		// --- event log (framework event bus) ---
		go func() {
			for ev := range handsFree.Events() {
				log.Printf("[event] %s mode=%s text=%q", ev.Type, ev.Mode, ev.Text)
			}
		}()
	}

	detections := make(chan wakeword.DetectionEvent, 4)
	var pipeline *wakeword.Pipeline // nil beim openwakeword-Backend (Sidecar captured selbst)

	switch cfg.wakewordBackend() {
	case "openwakeword":
		oww, err := startOpenWakeWord(cfg, targetMode, func(ev wakeword.DetectionEvent) {
			select {
			case detections <- ev:
			default:
			}
		})
		if err != nil {
			log.Fatalf("wakeword: %v", err)
		}
		defer oww.Close()
		log.Printf("kombify box companion bereit - Wakeword %q (openwakeword, %s, threshold=%.2f) -> %s",
			cfg.Wakeword.Phrase, cfg.owwPhraseModelFile(), cfg.owwThreshold(), targetMode)

	case "sherpa_kws":
		// --- wake-word pipeline (sherpa-onnx KWS) ---
		md := cfg.Wakeword.ModelDir
		detector, err := sherpa.NewDetector(sherpa.DetectorConfig{
			Encoder:      filepath.Join(md, "encoder-epoch-12-avg-2-chunk-16-left-64.onnx"),
			Decoder:      filepath.Join(md, "decoder-epoch-12-avg-2-chunk-16-left-64.onnx"),
			Joiner:       filepath.Join(md, "joiner-epoch-12-avg-2-chunk-16-left-64.onnx"),
			Tokens:       filepath.Join(md, "tokens.txt"),
			KeywordsFile: cfg.Wakeword.KeywordsFile,
			Keywords:     cfg.Wakeword.Keywords,
			Threshold:    cfg.Wakeword.Threshold,
			NumThreads:   2,
		})
		if err != nil {
			log.Fatalf("wakeword: %v (Modell fehlt? -> tools/get-model.ps1)", err)
		}
		defer detector.Close()

		pipeline, err = wakeword.NewPipeline(detector, wakeword.SinkFunc(func(ev wakeword.DetectionEvent) {
			select {
			case detections <- ev:
			default:
			}
		}), wakeword.Config{
			Phrase:               cfg.Wakeword.Phrase,
			DefaultMode:          targetMode,
			Threshold:            cfg.Wakeword.Threshold,
			MinConsecutiveFrames: cfg.Wakeword.MinFrames,
			Cooldown:             cfg.cooldown(),
		})
		if err != nil {
			log.Fatal(err)
		}
		defer pipeline.Close()

		wakeFrames := audio.Subscribe(64)
		go func() {
			var peak, frames, decodes int
			last := time.Now()
			for pcm := range wakeFrames {
				if r := rms16(pcm); r > peak {
					peak = r
				}
				frames++
				if time.Since(last) > 2*time.Second {
					log.Printf("[mic] peak RMS=%d over %d frames, kws_decodes=%d (speech should be >%d)", peak, frames, decodes, cfg.Capture.SilenceRMS)
					peak, frames, decodes = 0, 0, 0
					last = time.Now()
				}
				kwsPCM := pcm
				if cfg.Wakeword.InputGain != 1 {
					kwsPCM = scalePCM16(pcm, cfg.Wakeword.InputGain)
				}
				n, _, err := pipeline.FeedPCM(kwsPCM)
				if err != nil {
					log.Printf("wakeword feed: %v", err)
				} else {
					decodes += n
				}
			}
		}()

		source := "inline keywords"
		if cfg.Wakeword.KeywordsFile != "" {
			source = cfg.Wakeword.KeywordsFile
		}
		log.Printf("kombify box companion bereit - Wakeword %q (%s, %s, threshold=%.2f, min_frames=%d, input_gain=%.1fx) -> %s",
			cfg.Wakeword.Phrase, cfg.wakewordBackend(), source, cfg.Wakeword.Threshold, cfg.Wakeword.MinFrames, cfg.Wakeword.InputGain, targetMode)

	default:
		log.Fatalf("unsupported wakeword backend %q", cfg.Wakeword.Backend)
	}

	if targetMode == "assist" && sttProvider != nil && cfg.localSTTProvider() {
		go sttHotwordFallback(ctx, cfg, audio, sttProvider, detections, targetMode)
	}
	if testWAV := strings.TrimSpace(os.Getenv("KBX_WAKE_TEST_WAV")); testWAV != "" {
		go playWakeTestWAV(audio, testWAV)
	}

	for {
		select {
		case <-ctx.Done():
			if handsFree != nil {
				_ = handsFree.Stop(context.Background())
			}
			return
		case ev := <-detections:
			// Suppress detection during ding + capture + TTS playback so the
			// box's own output cannot self-trigger the wakeword. Resume()
			// re-enables and resets the debounce/stream state; beim
			// openwakeword-Sidecar (pipeline == nil) uebernimmt das Draining
			// unten dieselbe Aufgabe.
			if pipeline != nil {
				pipeline.Pause()
			}
			if localVoiceAgent != nil {
				handleLocalVoiceAgentDetection(ctx, cfg, audio, localVoiceAgent, ev)
			} else if targetMode == "voice_agent" {
				handleVoiceAgentDetection(ctx, cfg, audio, voiceAgent, ev)
			} else if targetMode == "wake_only" {
				handleWakeOnlyDetection(ctx, audio, ev)
			} else if err := handsFree.HandleWake(ctx, ev); err != nil {
				log.Printf("[assist] %v", err)
			}
			if pipeline != nil {
				pipeline.Resume()
			}
			// Waehrend des Turns aufgelaufene Detections (Selbst-Trigger durch
			// eigene TTS-Ausgabe) verwerfen statt sie als neue Turns abzuarbeiten.
			for drained := false; !drained; {
				select {
				case <-detections:
				default:
					drained = true
				}
			}
		}
	}
}

func handleWakeOnlyDetection(ctx context.Context, audio *AudioIO, ev wakeword.DetectionEvent) {
	log.Printf("[wake] %q (keyword=%s)", ev.Phrase, ev.Keyword)
	audio.Ding()
	select {
	case <-ctx.Done():
	case <-time.After(900 * time.Millisecond):
	}
	log.Printf("[event] wake_only_done mode=wake_only text=%q", ev.Phrase)
}

func playWakeTestWAV(audio *AudioIO, path string) {
	time.Sleep(3 * time.Second)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[selftest] wake wav read: %v", err)
		return
	}
	pcm, rate, channels, err := pcmFromWAV(data)
	if err != nil {
		log.Printf("[selftest] wake wav decode: %v", err)
		return
	}
	log.Printf("[selftest] playing wake wav %s @%dHz/%dch", path, rate, channels)
	if err := audio.PlayPCM(pcm, rate, channels); err != nil {
		log.Printf("[selftest] wake wav playback: %v", err)
	}
}

// boxSessionSerial liefert die Session-Key-Kennung der Box. Heute ist die
// USB-Seriennummer firmwareseitig konstant "KBX-0001"; sobald die Firmware
// "KBX IDENT?" beantwortet (CDC v2), liest der Companion sie von dort.
func boxSessionSerial() string {
	return "KBX-0001"
}

// recordUtterance captures 16k mono PCM until silence or the max duration.
func recordUtterance(ctx context.Context, cfg *Config, audio *AudioIO) []byte {
	frames := audio.Subscribe(64)
	defer audio.Unsubscribe(frames)

	var buf bytes.Buffer
	maxDur := time.Duration(cfg.Capture.MaxUtteranceSec) * time.Second
	silenceCut := time.Duration(cfg.Capture.SilenceCutoffMS) * time.Millisecond
	deadline := time.NewTimer(maxDur)
	defer deadline.Stop()

	var lastVoice = time.Now()
	spoke := false
	for {
		select {
		case <-ctx.Done():
			return buf.Bytes()
		case <-deadline.C:
			return buf.Bytes()
		case pcm, ok := <-frames:
			if !ok {
				return buf.Bytes()
			}
			buf.Write(pcm)
			if rms16(pcm) >= cfg.Capture.SilenceRMS {
				lastVoice = time.Now()
				spoke = true
			}
			if spoke && time.Since(lastVoice) > silenceCut {
				return buf.Bytes()
			}
			if !spoke && time.Since(lastVoice) > 5*time.Second {
				return nil // nothing said after wake
			}
		}
	}
}

func sttHotwordFallback(ctx context.Context, cfg *Config, audio *AudioIO, sttP stt.STTProvider, detections chan<- wakeword.DetectionEvent, targetMode string) {
	frames := audio.Subscribe(96)
	defer audio.Unsubscribe(frames)

	const (
		minHotwordPCMBytes = 16000 * 2 / 2 // 0.5 s at 16 kHz mono s16
		maxHotwordPCMBytes = 16000 * 2 * 4 // 4 s
	)
	silenceCut := 650 * time.Millisecond
	cooldown := 2500 * time.Millisecond
	if cfg.Wakeword.CooldownMS > 0 {
		cooldown = time.Duration(cfg.Wakeword.CooldownMS) * time.Millisecond
	}
	silenceRMS := cfg.Capture.SilenceRMS
	if silenceRMS < 300 {
		silenceRMS = 300
	}

	var buf bytes.Buffer
	inSpeech := false
	lastVoice := time.Now()
	lastTrigger := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case pcm, ok := <-frames:
			if !ok {
				return
			}
			r := rms16(pcm)
			if r >= silenceRMS {
				if !inSpeech {
					buf.Reset()
					inSpeech = true
				}
				lastVoice = time.Now()
			}
			if inSpeech {
				buf.Write(pcm)
			}
			if !inSpeech {
				continue
			}
			tooLong := buf.Len() >= maxHotwordPCMBytes
			silentEnough := buf.Len() >= minHotwordPCMBytes && time.Since(lastVoice) >= silenceCut
			if !tooLong && !silentEnough {
				continue
			}

			pcmSegment := append([]byte(nil), buf.Bytes()...)
			buf.Reset()
			inSpeech = false
			if len(pcmSegment) < minHotwordPCMBytes || time.Since(lastTrigger) < cooldown {
				continue
			}
			text, ok := transcribeHotwordSegment(ctx, cfg, sttP, pcmSegment)
			if !ok {
				continue
			}
			keyword := hotwordFromTranscript(text)
			if keyword == "" {
				log.Printf("[wake-fallback] no hotword text=%q", text)
				continue
			}
			lastTrigger = time.Now()
			log.Printf("[wake-fallback] %q -> %s", text, keyword)
			select {
			case detections <- wakeword.DetectionEvent{
				Phrase:      cfg.Wakeword.Phrase,
				Keyword:     "stt_" + keyword,
				Mode:        targetMode,
				Probability: 1,
				At:          lastTrigger,
			}:
			default:
			}
		}
	}
}

func transcribeHotwordSegment(ctx context.Context, cfg *Config, sttP stt.STTProvider, pcm []byte) (string, bool) {
	wav := wavFromPCM16(pcm, 16000)
	sttCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := sttP.Transcribe(sttCtx, wav, stt.TranscribeOpts{Language: cfg.STT.Language, Model: cfg.STT.Model})
	if err != nil {
		log.Printf("[wake-fallback] stt: %v", err)
		return "", false
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return "", false
	}
	return text, true
}

func hotwordFromTranscript(text string) string {
	t := strings.ToLower(text)
	replacer := strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ":", " ", ";", " ", "-", " ")
	t = " " + strings.Join(strings.Fields(replacer.Replace(t)), " ") + " "
	switch {
	case strings.Contains(t, " jarvis ") || strings.Contains(t, " javis ") || strings.Contains(t, " dscharvis "):
		return "jarvis"
	case strings.Contains(t, " alexa ") || strings.Contains(t, " alex ") || strings.Contains(t, " alessa "):
		return "alexa"
	case strings.Contains(t, " siri "):
		return "siri"
	default:
		return ""
	}
}

func rms16(pcm []byte) int {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var acc float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
		acc += v * v
	}
	return int(math.Sqrt(acc / float64(n)))
}

func scalePCM16(pcm []byte, gain float32) []byte {
	if gain <= 0 || gain == 1 || len(pcm) == 0 {
		return pcm
	}
	out := make([]byte, len(pcm))
	copy(out, pcm)
	for i := 0; i+1 < len(out); i += 2 {
		v := int16(binary.LittleEndian.Uint16(out[i:]))
		scaled := int(float32(v) * gain)
		if scaled > math.MaxInt16 {
			scaled = math.MaxInt16
		} else if scaled < math.MinInt16 {
			scaled = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i:], uint16(int16(scaled)))
	}
	return out
}

func playResult(audio *AudioIO, data []byte, format string) {
	normalized := strings.ToLower(strings.TrimSpace(format))
	switch {
	case normalized == "" && isLikelyWAV(data):
		pcm, rate, channels, err := pcmFromWAV(data)
		if err != nil {
			log.Printf("[tts] wav decode: %v", err)
			return
		}
		_ = audio.PlayPCM(pcm, rate, channels)
	case normalized == "" || normalized == "mp3" || strings.Contains(normalized, "mpeg"):
		dec, err := mp3.NewDecoder(bytes.NewReader(data))
		if err != nil {
			log.Printf("[tts] mp3 decode: %v", err)
			return
		}
		raw, err := io.ReadAll(dec)
		if err != nil {
			log.Printf("[tts] mp3 read: %v", err)
			return
		}
		// go-mp3 outputs S16LE stereo at dec.SampleRate()
		_ = audio.PlayPCM(raw, int(dec.SampleRate()), 2)
	case normalized == "wav" || strings.Contains(normalized, "wav"):
		pcm, rate, channels, err := pcmFromWAV(data)
		if err != nil {
			log.Printf("[tts] wav decode: %v", err)
			return
		}
		_ = audio.PlayPCM(pcm, rate, channels)
	case normalized == "pcm" || normalized == "raw":
		_ = audio.PlayPCM(data, 24000, 1)
	default:
		log.Printf("[tts] unbekanntes Format %q", format)
	}
}

func pcmFromWAV(data []byte) ([]byte, int, int, error) {
	if !isLikelyWAV(data) {
		return nil, 0, 0, fmt.Errorf("not a RIFF/WAVE file")
	}
	var sampleRate, channels, bitsPerSample int
	var audioFormat uint16
	var pcm []byte
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		off += 8
		if size < 0 || off+size > len(data) {
			return nil, 0, 0, fmt.Errorf("invalid chunk %q size %d", id, size)
		}
		chunk := data[off : off+size]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, 0, fmt.Errorf("short fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(chunk[0:2])
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(chunk[14:16]))
		case "data":
			pcm = append([]byte(nil), chunk...)
		}
		off += size
		if size%2 == 1 {
			off++
		}
	}
	if audioFormat != 1 {
		return nil, 0, 0, fmt.Errorf("unsupported wav format %d", audioFormat)
	}
	if bitsPerSample != 16 {
		return nil, 0, 0, fmt.Errorf("unsupported wav bit depth %d", bitsPerSample)
	}
	if channels <= 0 || sampleRate <= 0 || len(pcm) == 0 {
		return nil, 0, 0, fmt.Errorf("missing wav fmt/data")
	}
	return pcm, sampleRate, channels, nil
}

func isLikelyWAV(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE"))
}
