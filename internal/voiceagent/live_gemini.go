package voiceagent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"google.golang.org/genai"
)

// GeminiLive implements LiveProvider using the Google GenAI Live API.
type GeminiLive struct {
	mu      sync.RWMutex
	client  *genai.Client
	session *genai.Session
}

// NewGeminiLive creates a Gemini Live provider.
func NewGeminiLive() *GeminiLive {
	return &GeminiLive{}
}

func (g *GeminiLive) Connect(ctx context.Context, cfg LiveConfig) error {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return fmt.Errorf("gemini live: create client: %w", err)
	}
	g.client = client

	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt(cfg.Locale)
	}
	if cfg.VocabularyHint != "" {
		systemPrompt += "\n\n" + cfg.VocabularyHint
	}

	voiceName := cfg.Voice
	if voiceName == "" {
		voiceName = "Kore"
	}

	connectCfg := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio, genai.ModalityText},
		SpeechConfig: &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voiceName,
				},
			},
		},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(systemPrompt)},
		},
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-3.1-flash-live-preview"
	}

	session, err := client.Live.Connect(ctx, model, connectCfg)
	if err != nil {
		return fmt.Errorf("gemini live: connect to %s: %w", model, err)
	}

	g.mu.Lock()
	g.session = session
	g.mu.Unlock()
	slog.Info("Gemini Live connected", "model", model, "voice", voiceName)
	return nil
}

func (g *GeminiLive) SendAudio(chunk []byte) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: not connected")
	}

	return session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			MIMEType: "audio/pcm;rate=16000",
			Data:     chunk,
		},
	})
}

func (g *GeminiLive) Receive(ctx context.Context) (*LiveMessage, error) {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("gemini live: not connected")
	}

	// genai.Session.Receive() blocks until a message arrives.
	// Context cancellation is handled by the caller closing the session/WebSocket.
	resp, err := session.Receive()
	if err != nil {
		return nil, fmt.Errorf("gemini live: receive: %w", err)
	}

	msg := &LiveMessage{}

	if resp.ServerContent != nil {
		if resp.ServerContent.ModelTurn != nil {
			for _, part := range resp.ServerContent.ModelTurn.Parts {
				if part.InlineData != nil && len(part.InlineData.Data) > 0 {
					msg.Audio = append(msg.Audio, part.InlineData.Data...)
				}
				if part.Text != "" {
					msg.Text += part.Text
				}
			}
		}
		msg.Done = resp.ServerContent.TurnComplete
	}

	return msg, nil
}

func (g *GeminiLive) SendText(text string) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: not connected")
	}

	return session.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{
			{
				Role:  "user",
				Parts: []*genai.Part{genai.NewPartFromText(text)},
			},
		},
	})
}

func (g *GeminiLive) Close() error {
	g.mu.Lock()
	session := g.session
	g.session = nil
	g.client = nil
	g.mu.Unlock()

	if session != nil {
		if err := session.Close(); err != nil {
			return fmt.Errorf("gemini live: close session: %w", err)
		}
	}
	return nil
}

func (g *GeminiLive) Name() string { return "gemini-live" }

func defaultSystemPrompt(locale string) string {
	switch locale {
	case "de", "de-DE":
		return `Du bist ein hilfreicher Sprachassistent. Antworte auf Deutsch, es sei denn der Nutzer wechselt die Sprache.
Sei natuerlich, freundlich und praezise. Du fuehrst ein Gespraech — behalte den Kontext im Kopf.
Wenn der Nutzer eine Aufgabe beschreibt, hilf effizient. Wenn er eine Frage stellt, antworte kurz und verstaendlich.`
	default:
		return `You are a helpful voice companion. Respond in English unless the user switches language.
Be natural, friendly, and concise. You are having a conversation — maintain context throughout.
When the user describes a task, help efficiently. When they ask a question, answer briefly and clearly.`
	}
}
