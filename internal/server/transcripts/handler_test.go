//go:build linux

package transcripts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/store"
)

func TestTranscriptsRejectsInvalidLimit(t *testing.T) {
	h := New(fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/v1/transcripts?limit=bogus", nil)
	rec := httptest.NewRecorder()
	h.transcripts(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_limit") {
		t.Fatalf("body = %s, want invalid_limit", rec.Body.String())
	}
}

type fakeStore struct{}

func (fakeStore) SaveTranscription(context.Context, string, string, string, string, int64, int64, []byte) error {
	return nil
}

func (fakeStore) GetTranscription(context.Context, int64) (*store.Transcription, error) {
	return nil, nil
}

func (fakeStore) ListTranscriptions(context.Context, store.ListOpts) ([]store.Transcription, error) {
	return nil, nil
}

func (fakeStore) TranscriptionCount(context.Context) (int, error) {
	return 0, nil
}

func (fakeStore) SaveQuickNote(context.Context, string, string, string, int64, int64, []byte) (int64, error) {
	return 0, nil
}

func (fakeStore) GetQuickNote(context.Context, int64) (*store.QuickNote, error) {
	return nil, nil
}

func (fakeStore) ListQuickNotes(context.Context, store.ListOpts) ([]store.QuickNote, error) {
	return nil, nil
}

func (fakeStore) UpdateQuickNote(context.Context, int64, string) error {
	return nil
}

func (fakeStore) UpdateQuickNoteCapture(context.Context, int64, string, string, int64, int64, []byte) error {
	return nil
}

func (fakeStore) PinQuickNote(context.Context, int64, bool) error {
	return nil
}

func (fakeStore) DeleteQuickNote(context.Context, int64) error {
	return nil
}

func (fakeStore) QuickNoteCount(context.Context) (int, error) {
	return 0, nil
}

func (fakeStore) Stats(context.Context) (store.Stats, error) {
	return store.Stats{}, nil
}

func (fakeStore) Close() error {
	return nil
}
