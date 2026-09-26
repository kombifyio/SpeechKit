package local

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Transcribe implements [stt.STTProvider] against the child's
// /v1/audio/transcriptions route. When a [Provider.StartServer] call is in
// progress it waits for it to finish, and it fails when the server is not
// ready. A multilanguage request is sent as whisper.cpp's explicit "auto",
// since the server would otherwise pin English. The request timeout grows
// with the audio length (20 s plus three times the clip, between 5 and 10
// minutes) and the result Model is the model file's base name.
func (p *Provider) Transcribe(ctx context.Context, audioData []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	if !p.ready.Load() {
		// If startup is in progress, wait for it to complete before failing.
		p.processMu.Lock()
		done := p.startDone
		p.processMu.Unlock()
		if done != nil {
			slog.Info("whisper-server: waiting for startup to complete...")
			select {
			case <-done:
				// startup finished — check ready below
			case <-ctx.Done():
				return nil, fmt.Errorf("local whisper-server not ready: cancelled while waiting for startup")
			}
		}
		if !p.ready.Load() {
			return nil, fmt.Errorf("local whisper-server not ready")
		}
	}

	endpoint := fmt.Sprintf("%s/v1/audio/transcriptions", p.BaseURL)
	resolved := stt.ResolveTranscribeOptions("local", "stt.local.whispercpp", opts, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	// whisper.cpp's server defaults to English, not auto-detect:
	// "-l LANG, --language LANG  [en] spoken language ('auto' for auto-detect)".
	// Omitting the field therefore pins English rather than leaving the model
	// free, so multilanguage has to be sent explicitly as the documented
	// "auto" value. This is why the multilanguage sentinel is translated per
	// provider instead of being normalized away everywhere.
	language := resolved.APILanguage()
	if language == "" {
		language = whisperCppAutoDetectLanguage
	}
	if err := writer.WriteField("language", language); err != nil {
		return nil, fmt.Errorf("write language field: %w", err)
	}
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}
	if resolved.Prompt != "" {
		if err := writer.WriteField("prompt", resolved.Prompt); err != nil {
			return nil, fmt.Errorf("write prompt field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	requestTimeout := localTranscribeTimeout(audioData)
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	start := time.Now()
	resp, err := transcribeHTTPClient(p.client, requestTimeout, &p.Validation).Do(req)
	if err != nil {
		return nil, fmt.Errorf("local transcribe: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, localMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, netsec.ProviderStatusError("local", resp.StatusCode, respBody)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// whisper.cpp does not report which language it detected, so the label can
	// only echo what was asked for. APILanguage() is empty for a multilanguage
	// session, and reporting a locale there would be an inference — the exact
	// thing the multilanguage rule forbids.
	lang := stt.FirstNonEmptyTrimmed(resolved.APILanguage(), stt.LanguageMulti)

	return &stt.Result{
		Text:     result.Text,
		Language: lang,
		Duration: duration,
		Provider: p.Name(),
		Model:    p.displayModel(),
	}, nil
}

func localTranscribeTimeout(audioData []byte) time.Duration {
	timeout := localMinTranscribeTimeout
	if durationSecs := estimateWAVDurationSecs(audioData); durationSecs > 0 {
		scaled := 20*time.Second + time.Duration(durationSecs*3*float64(time.Second))
		if scaled > timeout {
			timeout = scaled
		}
	}
	if timeout > localMaxTranscribeTimeout {
		return localMaxTranscribeTimeout
	}
	return timeout
}

func transcribeHTTPClient(base *http.Client, timeout time.Duration, validation *netsec.ValidationOptions) *http.Client {
	if timeout <= 0 {
		timeout = localMinTranscribeTimeout
	}
	if base == nil {
		if validation == nil {
			localValidation := netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true, RequireLocal: true}
			validation = &localValidation
		}
		return netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: timeout + 5*time.Second, DialValidation: validation})
	}
	cloned := *base
	cloned.Timeout = timeout + 5*time.Second
	return &cloned
}

func estimateWAVDurationSecs(audioData []byte) float64 {
	if len(audioData) >= 44 &&
		string(audioData[0:4]) == "RIFF" &&
		string(audioData[8:12]) == "WAVE" {
		channels := int(binary.LittleEndian.Uint16(audioData[22:24]))
		sampleRate := int(binary.LittleEndian.Uint32(audioData[24:28]))
		bitsPerSample := int(binary.LittleEndian.Uint16(audioData[34:36]))
		dataSize := int(binary.LittleEndian.Uint32(audioData[40:44]))
		bytesPerFrame := channels * (bitsPerSample / 8)
		if sampleRate > 0 && bytesPerFrame > 0 && dataSize > 0 {
			return float64(dataSize/bytesPerFrame) / float64(sampleRate)
		}
	}
	return audio.PCMDurationSecs(audioData)
}
