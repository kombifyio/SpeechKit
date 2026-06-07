package stt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
	"google.golang.org/api/option"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// googleSTTScope is the OAuth scope required for Cloud Speech-to-Text.
const googleSTTScope = "https://www.googleapis.com/auth/cloud-platform"

// StartSpeakerStream opens a Google Cloud Speech-to-Text v2 StreamingRecognize
// session.
//
// IMPORTANT: Google STT v2 does NOT support speaker diarization in streaming
// mode — diarization is available only in BatchRecognize/Recognize (see the
// Chirp-3 docs). This adapter therefore provides realtime TRANSCRIPTION only:
// frames carry Text without speaker labels (no Segment/Speakers). Streaming
// speaker diarization in the Voice Agent comes from Deepgram/AssemblyAI.
func (p *GoogleSTTProvider) StartSpeakerStream(ctx context.Context, opts speaker.Options, format speaker.AudioFormat) (speaker.SpeakerStream, error) {
	creds, err := p.resolveGoogleStreamingCredentials()
	if err != nil {
		return nil, err
	}
	projectID, projectIDErr := creds.ProjectID(ctx)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		projectID = firstNonEmptyTrimmed(os.Getenv("GOOGLE_CLOUD_PROJECT"), config.ResolveSecret("GOOGLE_CLOUD_PROJECT"))
	}
	if projectID == "" {
		if projectIDErr != nil {
			return nil, fmt.Errorf("google stt v2: project id unavailable from credentials: %w; set GOOGLE_CLOUD_PROJECT", projectIDErr)
		}
		return nil, fmt.Errorf("google stt v2: project id not found in credentials; set GOOGLE_CLOUD_PROJECT")
	}

	client, err := speech.NewClient(ctx, option.WithAuthCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("google stt v2 client: %w", err)
	}
	stream, err := client.StreamingRecognize(ctx)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("google stt v2 streaming open: %w", err)
	}

	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = p.Model
	}
	if model == "" {
		model = "latest_long"
	}
	f := format.Normalized()
	configReq := &speechpb.StreamingRecognizeRequest{
		Recognizer: fmt.Sprintf("projects/%s/locations/global/recognizers/_", projectID),
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{
						ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
							Encoding:          speechpb.ExplicitDecodingConfig_LINEAR16,
							SampleRateHertz:   int32(f.SampleRateHz), //nolint:gosec // audio sample rate is a small bounded value
							AudioChannelCount: int32(f.Channels),     //nolint:gosec // audio channel count is a small bounded value
						},
					},
					LanguageCodes: []string{mapLanguageCode(opts.Language)},
					Model:         model,
				},
			},
		},
	}
	if err := stream.Send(configReq); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("google stt v2 send config: %w", err)
	}

	return &googleSpeakerStream{
		client:   client,
		stream:   stream,
		provider: p.Name(),
		model:    model,
		openedAt: time.Now(),
	}, nil
}

// googleStreamClient is the minimal surface of the v2 StreamingRecognize bidi
// stream. Using an interface (rather than the generated stream type) keeps the
// adapter robust against grpc codegen changes and makes the receive loop
// testable with a fake stream.
type googleStreamClient interface {
	Send(*speechpb.StreamingRecognizeRequest) error
	Recv() (*speechpb.StreamingRecognizeResponse, error)
	CloseSend() error
}

type googleSpeakerStream struct {
	client   *speech.Client
	stream   googleStreamClient
	provider string
	model    string
	openedAt time.Time

	mu  sync.Mutex
	seq int64
}

func (s *googleSpeakerStream) SendAudio(_ context.Context, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	return s.stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_Audio{Audio: append([]byte(nil), chunk...)},
	})
}

func (s *googleSpeakerStream) EndAudio(_ context.Context) error {
	return s.stream.CloseSend()
}

func (s *googleSpeakerStream) Receive(_ context.Context) (*speaker.SpeakerFrame, error) {
	for {
		resp, err := s.stream.Recv()
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.seq++
		seq := s.seq
		s.mu.Unlock()
		latency := time.Since(s.openedAt).Milliseconds()
		if frame := googleStreamingTranscriptFrame(resp, s.provider, s.model, seq, latency); frame != nil {
			return frame, nil
		}
		// Speech-event-only / empty responses carry no transcript; keep reading.
	}
}

func (s *googleSpeakerStream) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// googleStreamingTranscriptFrame maps a v2 StreamingRecognizeResponse to a
// transcription-only SpeakerFrame. v2 streaming cannot diarize, so frames carry
// Text + IsFinal and no speaker dimensions. Returns nil when the response
// carries no transcript text.
func googleStreamingTranscriptFrame(resp *speechpb.StreamingRecognizeResponse, provider, model string, sequence, latencyMs int64) *speaker.SpeakerFrame {
	if resp == nil {
		return nil
	}
	var text strings.Builder
	final := false
	for _, result := range resp.GetResults() {
		alts := result.GetAlternatives()
		if len(alts) == 0 {
			continue
		}
		t := strings.TrimSpace(alts[0].GetTranscript())
		if t == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte(' ')
		}
		text.WriteString(t)
		if result.GetIsFinal() {
			final = true
		}
	}
	out := strings.TrimSpace(text.String())
	if out == "" {
		return nil
	}
	return &speaker.SpeakerFrame{
		Provider:  provider,
		Model:     model,
		Sequence:  sequence,
		Text:      out,
		IsFinal:   final,
		LatencyMs: latencyMs,
	}
}

// resolveGoogleStreamingCredentials loads OAuth credentials for v2 streaming:
// an inline service-account JSON (SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON) if
// present, otherwise Application Default Credentials (GOOGLE_APPLICATION_CREDENTIALS
// file path or ambient ADC). SPEECHKIT_GOOGLE_STT_API_KEY is batch REST only and
// is never sufficient for v2 streaming.
func (p *GoogleSTTProvider) resolveGoogleStreamingCredentials() (*auth.Credentials, error) {
	credentialsJSONEnv := firstNonEmptyTrimmed(p.STTCredentialsJSONEnv, config.GoogleSTTCredentialsJSONEnv)
	applicationCredentialsEnv := firstNonEmptyTrimmed(p.ApplicationCredentialsEnv, config.GoogleApplicationCredentialsEnv)

	if raw := firstNonEmptyTrimmed(os.Getenv(credentialsJSONEnv), config.ResolveSecret(credentialsJSONEnv)); raw != "" {
		// DetectDefault validates the credential configuration (the gap that deprecated the older
		// CredentialsFromJSON/WithCredentialsJSON path); the JSON itself is operator-controlled (Doppler/env).
		creds, err := credentials.DetectDefault(&credentials.DetectOptions{
			CredentialsJSON: []byte(raw),
			Scopes:          []string{googleSTTScope},
		})
		if err != nil {
			return nil, fmt.Errorf("google stt v2: parse %s: %w", credentialsJSONEnv, err)
		}
		return creds, nil
	}

	if googleStreamingCredentialsPresent(applicationCredentialsEnv, credentialsJSONEnv) {
		creds, err := credentials.DetectDefault(&credentials.DetectOptions{
			Scopes: []string{googleSTTScope},
		})
		if err != nil {
			return nil, fmt.Errorf("google stt v2: load application default credentials: %w", err)
		}
		return creds, nil
	}

	return nil, fmt.Errorf("google stt v2 streaming transcription requires service-account/ADC credentials via %s or %s; %s is batch REST only",
		credentialsJSONEnv, applicationCredentialsEnv, config.GoogleSTTDefaultAPIKeyEnv)
}

func googleStreamingCredentialsPresent(applicationCredentialsEnv, credentialsJSONEnv string) bool {
	applicationCredentialsEnv = firstNonEmptyTrimmed(applicationCredentialsEnv, config.GoogleApplicationCredentialsEnv)
	credentialsJSONEnv = firstNonEmptyTrimmed(credentialsJSONEnv, config.GoogleSTTCredentialsJSONEnv)
	if value, ok := os.LookupEnv(applicationCredentialsEnv); ok {
		return strings.TrimSpace(value) != ""
	}
	if value, ok := os.LookupEnv(credentialsJSONEnv); ok {
		return strings.TrimSpace(value) != ""
	}
	if strings.TrimSpace(config.ResolveSecret(applicationCredentialsEnv)) != "" {
		return true
	}
	return strings.TrimSpace(config.ResolveSecret(credentialsJSONEnv)) != ""
}
