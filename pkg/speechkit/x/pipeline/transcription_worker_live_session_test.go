package pipeline

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// sessionHistory is a Persistence that can keep one live dictation as one
// entry, like the desktop SQL store.
type sessionHistory struct {
	countingPersistence
	mu      sync.Mutex
	nextID  int64
	entries map[int64]string
}

func (h *sessionHistory) CreateTranscription(_ context.Context, text, _, _, _ string, _, _ int64, _ []byte) (int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	if h.entries == nil {
		h.entries = map[int64]string{}
	}
	h.entries[h.nextID] = text
	return h.nextID, nil
}

func (h *sessionHistory) UpdateTranscriptionText(_ context.Context, id int64, text string, _, _ int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[id] = text
	return nil
}

func (h *sessionHistory) texts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.entries))
	for _, text := range h.entries {
		out = append(out, text)
	}
	sort.Strings(out)
	return out
}

// switchableOutput refuses deliveries while blocked is set.
type switchableOutput struct {
	recordingOutput
	blocked bool
}

func (o *switchableOutput) Deliver(ctx context.Context, transcript speechkit.Transcript, target any) error {
	o.mu.Lock()
	blocked := o.blocked
	o.mu.Unlock()
	if blocked {
		return speechkit.ErrOutputBlocked
	}
	return o.recordingOutput.Deliver(ctx, transcript, target)
}

func (o *switchableOutput) setBlocked(blocked bool) {
	o.mu.Lock()
	o.blocked = blocked
	o.mu.Unlock()
}

func newLiveSessionWorker(t *testing.T, history *sessionHistory, output speechkit.TranscriptOutput) *TranscriptionWorker {
	t.Helper()
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    NewTranscriptionRunner(stubTranscriber{}, history),
		Output:    output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}
	worker.Start(context.Background())
	t.Cleanup(func() {
		worker.Close()
		worker.Wait()
	})
	return worker
}

func sendFinal(t *testing.T, worker *TranscriptionWorker, session, segment uint64, text string) {
	t.Helper()
	event := speechkit.DictationStreamEvent{SessionID: session, SegmentID: segment, Text: text, IsFinal: true, Provider: "deepgram"}
	if err := worker.HandleDictationStreamEvent(context.Background(), event, speechkit.DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
		t.Fatalf("HandleDictationStreamEvent(%q) error = %v", text, err)
	}
}

// A live dictation commits one final per pause; the history must still hold
// the whole recording as one copyable entry (2026-10-03: every final became
// its own Library row).
func TestTranscriptionWorkerLiveSessionIsOneHistoryEntry(t *testing.T) {
	history := &sessionHistory{}
	worker := newLiveSessionWorker(t, history, &recordingOutput{})

	sendFinal(t, worker, 7, 1, "Back-ups managen kann")
	sendFinal(t, worker, 7, 2, "beziehungsweise für den User")
	sendFinal(t, worker, 7, 3, "zugreifbar machen kann.")
	sendFinal(t, worker, 8, 1, "Neue Aufnahme.")
	worker.Close()
	worker.Wait()

	got := history.texts()
	want := []string{"Back-ups managen kann beziehungsweise für den User zugreifbar machen kann.", "Neue Aufnahme."} // sorted
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("history = %q, want %q", got, want)
	}
}

// Once the target refuses a live insertion, the rest of the dictation is held
// and inserted once when the recording ends, like a full-capture dictation.
func TestTranscriptionWorkerBlockedLiveOutputIsInsertedWhenSessionEnds(t *testing.T) {
	output := &switchableOutput{}
	worker := newLiveSessionWorker(t, &sessionHistory{}, output)

	sendFinal(t, worker, 3, 1, "Erster Satz.")
	output.setBlocked(true)
	sendFinal(t, worker, 3, 2, "Zweiter Satz.")
	output.setBlocked(false)
	sendFinal(t, worker, 3, 3, "Dritter Satz.")
	if delivered := output.snapshot(); len(delivered) != 1 {
		t.Fatalf("deliveries while recording = %d, want only the first final", len(delivered))
	}

	worker.EndDictationStreamSession(context.Background(), 3, speechkit.DictationStreamSinkOptions{})

	delivered := output.snapshot()
	if len(delivered) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(delivered))
	}
	if got := delivered[1].transcript.Text; got != " Zweiter Satz. Dritter Satz." {
		t.Fatalf("held insertion = %q, want the rest of the dictation in one paste", got)
	}
}
