//go:build windows && cgo

package main

// Provider wiring: STT, Assist generator (kombify AI Gateway, OpenAI-
// compatible), and TTS - all composed from public SpeechKit packages.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/local"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openaicompat"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/vps"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

func buildSTT(cfg *Config) (stt.STTProvider, error) {
	key := cfg.sttKey()
	switch strings.ToLower(strings.TrimSpace(cfg.STT.Provider)) {
	case "local", "whispercpp", "whisper.cpp", "stt.local.whispercpp":
		modelPath := cfg.resolveWhisperModelPath()
		if modelPath == "" {
			return nil, fmt.Errorf("stt local: kein Whisper-Modell gefunden; erwartet %s oder setze [local].model_path", whisperModelHint(cfg))
		}
		return local.New(cfg.Local.Port, modelPath, cfg.Local.GPU), nil
	case "openai":
		return openaicompat.NewOpenAI(key), nil
	case "groq":
		return openaicompat.NewGroq(key), nil
	case "deepgram":
		if !realSecret(key) {
			return missingSTTKeyProvider{provider: "deepgram", envName: cfg.STT.APIKeyEnv}, nil
		}
		return deepgram.New(key, cfg.STT.Model), nil
	case "assemblyai":
		return assemblyai.New(key, cfg.STT.Model), nil
	case "ollama":
		return openaicompat.NewOllama(cfg.STT.BaseURL, cfg.STT.Model), nil
	case "vps", "selfhosted", "self-hosted":
		return vps.NewWithModel(cfg.STT.BaseURL, key, cfg.STT.Model), nil
	case "openai_compatible", "gateway", "":
		return openaicompat.New("kombify-gateway", cfg.STT.BaseURL, key, cfg.STT.Model), nil
	default:
		return nil, fmt.Errorf("stt: unknown provider %q", cfg.STT.Provider)
	}
}

type missingSTTKeyProvider struct {
	provider string
	envName  string
}

func (p missingSTTKeyProvider) Transcribe(context.Context, []byte, stt.TranscribeOpts) (*stt.Result, error) {
	return nil, fmt.Errorf("%s STT ist nicht konfiguriert: setze %s", p.provider, p.envName)
}

func (p missingSTTKeyProvider) Name() string {
	return p.provider
}

func (p missingSTTKeyProvider) Health(context.Context) error {
	return fmt.Errorf("%s STT ist nicht konfiguriert: setze %s", p.provider, p.envName)
}

func buildTTS(cfg *Config) (*tts.Service, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.TTS.Provider)) {
	case "piper", "local", "tts.local.piper":
		binary := findPiperBinary(cfg.TTS.Piper.Binary)
		if binary == "" {
			binary = strings.TrimSpace(cfg.TTS.Piper.Binary)
		}
		timeout := time.Duration(cfg.TTS.Piper.TimeoutSec) * time.Second
		p, err := tts.NewPiper(tts.PiperOpts{
			Binary:        binary,
			VoiceDir:      cfg.TTS.Piper.VoiceDir,
			DefaultVoices: cfg.TTS.Piper.DefaultVoices,
			Timeout:       timeout,
		})
		if err != nil {
			return nil, err
		}
		router := tts.NewRouter(tts.StrategyLocalOnly, p)
		return tts.NewService(router, tts.WithDefaultOpts(tts.SynthesizeOpts{
			Locale: cfg.STT.Language,
			Voice:  cfg.TTS.Voice,
			Format: "wav",
		}))
	}

	p := tts.NewOpenAI(tts.OpenAIOpts{
		APIKey: cfg.ttsKey(),
		Model:  cfg.TTS.Model,
		Voice:  cfg.TTS.Voice,
	})
	if cfg.TTS.BaseURL != "" && cfg.TTS.Provider != "openai" {
		p.BaseURL = cfg.TTS.BaseURL
	}
	router := tts.NewRouter(tts.StrategyCloudOnly, p)
	return tts.NewService(router)
}

// gatewayGenerator is a speechkit assist.Generator that talks to an
// OpenAI-compatible chat-completions endpoint (kombify AI Gateway).
type gatewayGenerator struct {
	baseURL string
	model   string
	apiKey  string
	system  string
	local   bool
	client  *http.Client
}

func newGatewayGenerator(cfg *Config) *gatewayGenerator {
	return &gatewayGenerator{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.Assist.BaseURL), "/"),
		model:   cfg.Assist.Model,
		apiKey:  cfg.assistKey(),
		system:  cfg.Assist.SystemPrompt,
		local:   cfg.localAssistProvider(),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (g *gatewayGenerator) GenerateAssist(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	if !g.configured() {
		return setupAssistResult(req.Locale, g.local), nil
	}
	body, _ := json.Marshal(map[string]any{
		"model": g.model,
		"messages": []map[string]string{
			{"role": "system", "content": g.system},
			{"role": "user", "content": req.Text},
		},
	})
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return speechkit.AssistResult{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if g.apiKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+g.apiKey)
	}
	resp, err := g.client.Do(hreq)
	if err != nil {
		if g.local {
			return setupAssistResult(req.Locale, true), nil
		}
		return speechkit.AssistResult{}, fmt.Errorf("assist gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		if g.local {
			return setupAssistResult(req.Locale, true), nil
		}
		return speechkit.AssistResult{}, fmt.Errorf("assist gateway: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 {
		return speechkit.AssistResult{}, fmt.Errorf("assist gateway: unexpected response: %s", truncate(string(raw), 200))
	}
	text := parsed.Choices[0].Message.Content
	return speechkit.AssistResult{
		Text:      text,
		SpeakText: text,
		Surface:   speechkit.AssistSurfacePanel,
		Locale:    req.Locale,
	}, nil
}

func (g *gatewayGenerator) configured() bool {
	if g == nil || !configuredBaseURL(g.baseURL) {
		return false
	}
	if g.local {
		return true
	}
	return realSecret(g.apiKey)
}

func setupAssistResult(locale string, local bool) speechkit.AssistResult {
	text := "Der LLM-Zugang ist noch nicht verbunden. Setze KOMBIFY_GATEWAY_BASE_URL und KOMBIFY_GATEWAY_TOKEN, dann beantworte ich offene Fragen direkt ueber das Modell."
	if local {
		text = "Das lokale LLM ist noch nicht gestartet. Lokale Skills laufen schon; fuer offene Fragen starte die SpeechKit Local-LLM-Runtime auf 127.0.0.1:8082 oder setze [assist].base_url auf dein lokales OpenAI-kompatibles Modell."
	}
	return speechkit.AssistResult{
		Text:      text,
		SpeakText: text,
		Surface:   speechkit.AssistSurfaceActionAck,
		Locale:    locale,
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// wavFromPCM16 wraps 16 kHz mono S16LE PCM in a RIFF/WAVE container.
func wavFromPCM16(pcm []byte, sampleRate int) []byte {
	var buf bytes.Buffer
	dataLen := uint32(len(pcm))
	byteRate := uint32(sampleRate * 2)
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, 36+dataLen)
	buf.WriteString("WAVEfmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buf, binary.LittleEndian, byteRate)
	binary.Write(&buf, binary.LittleEndian, uint16(2))  // block align
	binary.Write(&buf, binary.LittleEndian, uint16(16)) // bits
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, dataLen)
	buf.Write(pcm)
	return buf.Bytes()
}
