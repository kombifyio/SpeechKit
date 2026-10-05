package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/pipeline"
)

type liveRecordingStore struct {
	*SQLiteStore
	created chan struct{}
}

func (s liveRecordingStore) CreateTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audio []byte) (int64, error) {
	id, err := s.SQLiteStore.CreateTranscription(ctx, text, language, provider, model, durationMs, latencyMs, audio)
	if err == nil {
		s.created <- struct{}{}
	}
	return id, err
}

func (s liveRecordingStore) GetQuickNoteText(ctx context.Context, id int64) (string, error) {
	n, err := s.GetQuickNote(ctx, id)
	if err != nil {
		return "", err
	}
	return n.Text, nil
}

type liveRecordingTranscriber struct{}

func (liveRecordingTranscriber) Transcribe(context.Context, []byte, float64, string) (speechkit.Transcript, error) {
	return speechkit.Transcript{}, fmt.Errorf("streaming finals must not transcribe again")
}

// Regression: streaming dictation left its history row with zero duration and
// no source even with SaveAudio enabled. Exercise worker through scoped storage.
func TestLiveRecordingCompletesItsHistoryWithAudioPolicy(t *testing.T) {
	for _, saveAudio := range []bool{false, true} {
		t.Run(fmt.Sprintf("save_audio_%t", saveAudio), func(t *testing.T) {
			ctx := context.Background()
			s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "live.db"), SaveAudio: saveAudio})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			created := make(chan struct{}, 1)
			worker, err := pipeline.NewTranscriptionWorker(pipeline.TranscriptionWorkerConfig{Runner: pipeline.NewTranscriptionRunner(liveRecordingTranscriber{}, liveRecordingStore{SQLiteStore: s, created: created})})
			if err != nil {
				t.Fatal(err)
			}
			worker.Start(ctx)
			defer func() { worker.Close(); worker.Wait() }()
			opts := speechkit.DictationStreamSinkOptions{Language: "en"}
			if err := worker.HandleDictationStreamEvent(ctx, speechkit.DictationStreamEvent{SessionID: 4, SegmentID: 1, Text: "first", IsFinal: true}, opts); err != nil {
				t.Fatal(err)
			}
			select {
			case <-created:
			case <-time.After(5 * time.Second):
				t.Fatal("first live final was not persisted")
			}
			pcm := make([]byte, 32000)
			wav := speechkit.PCMToWAV(pcm)
			capture := speechkit.Submission{PCM: pcm, WAV: wav, DurationSecs: 1}
			worker.CompleteDictationStreamRecording(ctx, 4, capture, opts)
			worker.CompleteDictationStreamRecording(ctx, 4, capture, opts)
			if err := worker.HandleDictationStreamEvent(ctx, speechkit.DictationStreamEvent{SessionID: 4, SegmentID: 2, Text: "second", IsFinal: true}, opts); err != nil {
				t.Fatal(err)
			}
			worker.Close()
			worker.Wait()
			rows, err := s.ListTranscriptions(ctx, ListOpts{Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Text != "first second" || rows[0].DurationMs != 1000 {
				t.Fatalf("completed history = %+v", rows)
			}
			if !saveAudio {
				if rows[0].AudioPath != "" {
					t.Fatal("audio retained while disabled")
				}
				return
			}
			stored, err := os.ReadFile(rows[0].AudioPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, wav) {
				t.Fatal("history recording differs from the full source")
			}
		})
	}
}
