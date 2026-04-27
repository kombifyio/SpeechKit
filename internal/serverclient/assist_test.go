package serverclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
)

func TestAssistProcessTextHappyPath(t *testing.T) {
	tinyAudio := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/assist/process" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req processRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Text != "hello there" {
			t.Errorf("text = %q", req.Text)
		}
		if req.Locale != "de" {
			t.Errorf("locale = %q", req.Locale)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(processRequestResponse(t, "okay", "spoken okay", "respond", "de", base64.StdEncoding.EncodeToString(tinyAudio), "mp3"))
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, BearerToken: "T", HTTPClient: srv.Client()})
	a, _ := NewAssist(c)
	res, err := a.Process(context.Background(), "hello there", assistpkg.ProcessOpts{Locale: "de"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.Text != "okay" || res.SpeakText != "spoken okay" {
		t.Errorf("text/speak = %q / %q", res.Text, res.SpeakText)
	}
	if res.Action != "respond" {
		t.Errorf("action = %q", res.Action)
	}
	if res.Format != "mp3" {
		t.Errorf("format = %q", res.Format)
	}
	if !bytes.Equal(res.Audio, tinyAudio) {
		t.Errorf("audio = %x, want %x", res.Audio, tinyAudio)
	}
}

func TestAssistDecodesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"empty_transcript","message":"audio decoded to nothing"}}`))
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, HTTPClient: srv.Client()})
	a, _ := NewAssist(c)
	_, err := a.Process(context.Background(), "x", assistpkg.ProcessOpts{})
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *ServerError", err)
	}
	if se.Status != http.StatusUnprocessableEntity || se.Code != "empty_transcript" {
		t.Errorf("envelope = (%d, %q)", se.Status, se.Code)
	}
}

// processRequestResponse builds the JSON body the server would emit for a
// happy-path Assist call. Kept as a helper so the test reads cleanly.
func processRequestResponse(_ *testing.T, text, speak, action, locale, audioB64, format string) processResponse {
	return processResponse{
		Text:        text,
		SpeakText:   speak,
		Action:      action,
		Locale:      locale,
		AudioBase64: audioB64,
		AudioFormat: format,
		LatencyMs:   42,
	}
}
