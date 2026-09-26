package assemblyai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

type assemblyAISpeakerStream struct {
	conn      *websocket.Conn
	provider  string
	model     string
	opts      speaker.Options
	openedAt  time.Time
	sequence  atomic.Int64
	closeOnce atomic.Bool
}

func (s *assemblyAISpeakerStream) SendAudio(ctx context.Context, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, chunk)
}

func (s *assemblyAISpeakerStream) EndAudio(ctx context.Context) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Terminate"}`))
}

func (s *assemblyAISpeakerStream) Receive(ctx context.Context) (*speaker.SpeakerFrame, error) {
	for {
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			if stt.IsWebSocketClose(err) {
				return nil, io.EOF
			}
			return nil, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var event assemblyAIStreamingTurn
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("assemblyai speaker stream parse: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(event.Type), "Turn") {
			continue
		}
		frame := event.speakerFrame(s.provider, s.model, s.sequence.Add(1), time.Since(s.openedAt).Milliseconds(), s.opts)
		if frame.Text == "" && len(frame.Words) == 0 {
			continue
		}
		return &frame, nil
	}
}

func (s *assemblyAISpeakerStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "speaker stream close")
}

// assemblyAIDictationStream adapts a Universal-3.5 Pro realtime session to
// speechkit.DictationStream.
type assemblyAIDictationStream struct {
	conn         *websocket.Conn
	provider     string
	model        string
	language     string
	sessionID    uint64
	interim      bool
	diarize      bool
	llm          bool
	pendingFinal *speechkit.DictationStreamEvent
	openedAt     time.Time
	sequence     atomic.Int64
	closeOnce    atomic.Bool
}

func (s *assemblyAIDictationStream) SendPCM(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, pcm)
}

// Finalize asks the provider to flush and end the session. Universal-3.5 Pro
// realtime replies with the trailing (formatted) Turn, a Termination event,
// and a socket close — Receive surfaces the final transcript first and then
// io.EOF, which is exactly the per-segment drain the kernel expects.
func (s *assemblyAIDictationStream) Finalize(ctx context.Context) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Terminate"}`))
}

func (s *assemblyAIDictationStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	for {
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			if stt.IsWebSocketClose(err) {
				return speechkit.DictationStreamEvent{}, io.EOF
			}
			return speechkit.DictationStreamEvent{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var event assemblyAIStreamingTurn
		if err := json.Unmarshal(payload, &event); err != nil {
			return speechkit.DictationStreamEvent{}, fmt.Errorf("assemblyai dictation stream parse: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(event.Type)) {
		case "turn":
			frame := event.dictationEvent(s.provider, s.model, s.language, s.sessionID, s.sequence.Add(1), s.diarize)
			if frame.Text == "" && len(frame.Words) == 0 {
				continue
			}
			if !frame.IsFinal && !s.interim {
				continue
			}
			if frame.IsFinal && s.llm {
				held := frame
				s.pendingFinal = &held
				continue
			}
			return frame, nil
		case "llmgatewayresponse":
			if s.pendingFinal == nil {
				continue
			}
			out := *s.pendingFinal
			s.pendingFinal = nil
			if cleaned := assemblyAILLMGatewayContent(event.Data); cleaned != "" {
				out.Text = cleaned
			}
			return out, nil
		case "termination":
			if s.pendingFinal != nil {
				out := *s.pendingFinal
				s.pendingFinal = nil
				return out, nil
			}
			return speechkit.DictationStreamEvent{}, io.EOF
		default:
			// Begin, SpeechStarted, and future event types are not
			// transcript-bearing.
			continue
		}
	}
}

func (s *assemblyAIDictationStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "dictation stream close")
}
