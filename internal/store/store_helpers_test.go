package store

import (
	"context"
	"time"
)

// waitForNextSecond busy-waits until time.Now() crosses the next
// whole-second boundary. Replaces flat time.Sleep(1100*time.Millisecond)
// sites where the only requirement is "the NEXT save gets a different
// SQLite CURRENT_TIMESTAMP" (audit 4.4). Worst case 1 s wait; best case
// near-zero when called right before a second tick. Use only when the
// schema actually has 1-second-resolution timestamps.
func waitForNextSecond() {
	start := time.Now().Unix()
	for time.Now().Unix() == start {
		time.Sleep(5 * time.Millisecond)
	}
}

// mockStore is a minimal Store implementation for testing RegisterBackend.
type mockStore struct{}

func (m *mockStore) SaveTranscription(_ context.Context, _, _, _, _ string, _, _ int64, _ []byte) error {
	return nil
}
func (m *mockStore) GetTranscription(_ context.Context, _ int64) (*Transcription, error) {
	return nil, nil
}
func (m *mockStore) ListTranscriptions(_ context.Context, _ ListOpts) ([]Transcription, error) {
	return nil, nil
}
func (m *mockStore) TranscriptionCount(_ context.Context) (int, error) { return 0, nil }
func (m *mockStore) SaveQuickNote(_ context.Context, _, _, _ string, _, _ int64, _ []byte) (int64, error) {
	return 0, nil
}
func (m *mockStore) GetQuickNote(_ context.Context, _ int64) (*QuickNote, error) {
	return nil, nil
}
func (m *mockStore) ListQuickNotes(_ context.Context, _ ListOpts) ([]QuickNote, error) {
	return nil, nil
}
func (m *mockStore) UpdateQuickNote(_ context.Context, _ int64, _ string) error { return nil }
func (m *mockStore) UpdateQuickNoteCapture(_ context.Context, _ int64, _, _ string, _, _ int64, _ []byte) error {
	return nil
}
func (m *mockStore) PinQuickNote(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) DeleteQuickNote(_ context.Context, _ int64) error      { return nil }
func (m *mockStore) QuickNoteCount(_ context.Context) (int, error)         { return 0, nil }
func (m *mockStore) Stats(_ context.Context) (Stats, error)                { return Stats{}, nil }
func (m *mockStore) Close() error                                          { return nil }
