package xai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

var loopbackValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true, AllowPrivate: true}

type capturedForm struct {
	order  []string
	values map[string][]string
}

// xAI reads recognition options only when they precede the file, so the file
// must be the last multipart field. A pinned language goes on the wire and
// labels the result; a multilanguage request sends no language, and the
// answer's own (unreliable) language label is not trusted.
func TestTranscribeSendsOptionsBeforeFileAndKeepsMultilanguageOpen(t *testing.T) {
	var forms []capturedForm
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stt" || r.Header.Get("Authorization") != "Bearer xai-test" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		form := capturedForm{values: map[string][]string{}}
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(part)
			form.order = append(form.order, part.FormName())
			form.values[part.FormName()] = append(form.values[part.FormName()], string(data))
		}
		forms = append(forms, form)
		_, _ = w.Write([]byte(`{"text":" Hallo Welt. ","language":"en","duration":1.6}`))
	}))
	defer server.Close()

	p := New(Options{APIKey: "xai-test"})
	p.BaseURL = server.URL
	p.Validation = loopbackValidation

	pinned, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{
		Language: "de",
		Keyterms: []string{"kombify", " SpeechKit ", strings.Repeat("x", keytermMaxChars+1)},
	})
	if err != nil {
		t.Fatalf("Transcribe(de): %v", err)
	}
	open, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Language: "auto"})
	if err != nil {
		t.Fatalf("Transcribe(auto): %v", err)
	}
	if len(forms) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(forms))
	}

	if last := forms[0].order[len(forms[0].order)-1]; last != "file" {
		t.Errorf("file must be the last multipart field, order = %v", forms[0].order)
	}
	if got := forms[0].values["model"]; !slices.Equal(got, []string{DefaultModel}) {
		t.Errorf("model = %v, want %s", got, DefaultModel)
	}
	if got := forms[0].values["language"]; !slices.Equal(got, []string{"de"}) {
		t.Errorf("language = %v, want [de]", got)
	}
	if got := forms[0].values["keyterm"]; !slices.Equal(got, []string{"kombify", "SpeechKit"}) {
		t.Errorf("keyterm = %v, want the two terms within xAI's 50-character limit", got)
	}
	if pinned.Text != "Hallo Welt." || pinned.Language != "de" || pinned.Provider != ProviderName || pinned.Model != DefaultModel {
		t.Errorf("pinned result = %+v", pinned)
	}

	if _, sent := forms[1].values["language"]; sent {
		t.Errorf("multilanguage request must omit language, got %v", forms[1].values["language"])
	}
	if open.Language != stt.LanguageMulti {
		t.Errorf("multilanguage result language = %q, want %q", open.Language, stt.LanguageMulti)
	}
}
