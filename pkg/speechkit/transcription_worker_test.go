package speechkit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubTranscriber struct {
	transcript Transcript
	err        error
}

func (s stubTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, _ string) (Transcript, error) {
	if s.err != nil {
		return Transcript{}, s.err
	}
	return s.transcript, nil
}

type capturingAudioTranscriber struct {
	audio      []byte
	duration   float64
	language   string
	transcript Transcript
}

func (s *capturingAudioTranscriber) Transcribe(_ context.Context, audio []byte, durationSecs float64, language string) (Transcript, error) {
	s.audio = append([]byte(nil), audio...)
	s.duration = durationSecs
	s.language = language
	return s.transcript, nil
}

type countingTranscriber struct {
	mu         sync.Mutex
	calls      int
	transcript Transcript
}

func (s *countingTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, language string) (Transcript, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	transcript := s.transcript
	if transcript.Language == "" {
		transcript.Language = language
	}
	return transcript, nil
}

func (s *countingTranscriber) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type parallelSegmentTranscriber struct {
	expected int
	release  chan struct{}
	started  chan struct{}

	mu          sync.Mutex
	active      int
	maxActive   int
	starts      int
	startedOnce sync.Once
}

func newParallelSegmentTranscriber(expected int) *parallelSegmentTranscriber {
	return &parallelSegmentTranscriber{
		expected: expected,
		release:  make(chan struct{}),
		started:  make(chan struct{}),
	}
}

func (s *parallelSegmentTranscriber) Transcribe(ctx context.Context, audio []byte, _ float64, language string) (Transcript, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.starts++
	if s.starts == s.expected {
		s.startedOnce.Do(func() { close(s.started) })
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()

	select {
	case <-s.release:
	case <-ctx.Done():
		return Transcript{}, ctx.Err()
	}

	textByAudio := map[string]string{
		"segment-1": "kombi",
		"segment-2": "fire",
	}
	return Transcript{
		Text:     textByAudio[string(audio)],
		Language: language,
		Provider: "test",
		Model:    "parallel",
		Duration: 10 * time.Millisecond,
	}, nil
}

func (s *parallelSegmentTranscriber) maxConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

type replacingTranscriptTransformer struct{}

func (replacingTranscriptTransformer) Transform(_ context.Context, transcript Transcript) (Transcript, error) {
	transcript.Text = strings.ReplaceAll(transcript.Text, "kombi fire", "Kombify")
	return transcript, nil
}

type deliveredTranscript struct {
	transcript Transcript
	target     any
}

type recordingOutput struct {
	mu        sync.Mutex
	delivered []deliveredTranscript
	err       error
}

func (o *recordingOutput) Deliver(_ context.Context, transcript Transcript, target any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return o.err
	}
	o.delivered = append(o.delivered, deliveredTranscript{transcript: transcript, target: target})
	return nil
}

func (o *recordingOutput) snapshot() []deliveredTranscript {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]deliveredTranscript(nil), o.delivered...)
}

type blockingPersistence struct {
	started     chan struct{}
	release     chan struct{}
	done        chan struct{}
	startedOnce sync.Once
	doneOnce    sync.Once
}

func newBlockingPersistence() *blockingPersistence {
	return &blockingPersistence{
		started: make(chan struct{}),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (s *blockingPersistence) SaveQuickNote(context.Context, string, string, string, int64, int64, []byte) (int64, error) {
	return 0, nil
}

func (s *blockingPersistence) GetQuickNoteText(context.Context, int64) (string, error) {
	return "", nil
}

func (s *blockingPersistence) UpdateQuickNote(context.Context, int64, string) error {
	return nil
}

func (s *blockingPersistence) UpdateQuickNoteCapture(context.Context, int64, string, string, int64, int64, []byte) error {
	return nil
}

func (s *blockingPersistence) SaveTranscription(ctx context.Context, _, _, _, _ string, _, _ int64, _ []byte) error {
	s.startedOnce.Do(func() { close(s.started) })
	defer s.doneOnce.Do(func() { close(s.done) })

	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type recordingObserver struct {
	mu         sync.Mutex
	states     []string
	logs       []string
	drafts     []string
	committed  []string
	quickNotes []bool
}

func (o *recordingObserver) OnState(status, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.states = append(o.states, status+":"+text)
}

func (o *recordingObserver) OnLog(message, kind string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logs = append(o.logs, kind+":"+message)
}

func (o *recordingObserver) OnTranscriptCommitted(transcript Transcript, quickNote bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.committed = append(o.committed, transcript.Text)
	o.quickNotes = append(o.quickNotes, quickNote)
}

func (o *recordingObserver) OnTranscriptDraft(transcript Transcript) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.drafts = append(o.drafts, transcript.Text)
}

func (o *recordingObserver) hasLog(message string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, log := range o.logs {
		if strings.Contains(log, message) {
			return true
		}
	}
	return false
}

func TestLowConfidenceWords(t *testing.T) {
	words := []WordConfidence{
		{Text: "Ich", Confidence: 0.99},
		{Text: "Ultracord", Confidence: 0.42},
		{Text: "mit", Confidence: 0.95},
		{Text: "Stacket", Confidence: 0.55},
		{Text: "stacket", Confidence: 0.51}, // case-insensitive duplicate of "Stacket"
	}

	terms, minConf := LowConfidenceWords(words, 0.6)
	if got := strings.Join(terms, ","); got != "Ultracord,Stacket" {
		t.Fatalf("terms = %q, want \"Ultracord,Stacket\" (distinct, below-threshold only)", got)
	}
	if minConf < 0.41 || minConf > 0.43 {
		t.Fatalf("minConfidence = %v, want ~0.42 (floor across all words)", minConf)
	}

	if terms, gotMin := LowConfidenceWords(words, 0); terms != nil || gotMin != 0 {
		t.Fatalf("threshold<=0 must disable detection, got (%#v, %v)", terms, gotMin)
	}
	if terms, _ := LowConfidenceWords(nil, 0.6); terms != nil {
		t.Fatalf("nil words must yield nil terms, got %#v", terms)
	}
}

// TestTranscriptionWorkerLogsLowConfidenceWords is the regression guard for the
// "words vanish silently" complaint: when the provider reports per-word
// confidence, the worker must surface the below-threshold terms in the
// status/log feed so a mis-recognized word is visible, while high-confidence
// words stay unflagged.
func TestTranscriptionWorkerLogsLowConfidenceWords(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: Transcript{
			Text:     "Ich nutze Ultracord",
			Provider: "deepgram",
			Duration: 100 * time.Millisecond,
			Words: []WordConfidence{
				{Text: "Ich", Confidence: 0.99},
				{Text: "nutze", Confidence: 0.98},
				{Text: "Ultracord", Confidence: 0.41},
			},
		},
	}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:                time.Second,
		QueueSize:              1,
		Runner:                 runner,
		Output:                 output,
		Observer:               observer,
		LowConfidenceThreshold: 0.6,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
		Target:     "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	var found string
	observer.mu.Lock()
	for _, l := range observer.logs {
		if strings.HasPrefix(l, "warn:") && strings.Contains(l, "Low-confidence words") {
			found = l
		}
	}
	observer.mu.Unlock()

	if found == "" {
		t.Fatalf("expected a low-confidence warn log, got logs = %#v", observer.logs)
	}
	if !strings.Contains(found, "Ultracord") {
		t.Fatalf("low-confidence log must name the uncertain word, got %q", found)
	}
	if strings.Contains(found, "nutze") {
		t.Fatalf("high-confidence words must not be flagged, got %q", found)
	}
}

// A provider can answer successfully with a zero-length transcript — Deepgram
// does exactly that when the pinned language does not match the speech: HTTP
// 200, no error, no words. The worker used to run the whole commit path on that
// empty string, so the user's speech disappeared with no log line, no visible
// state and no history entry. Empty-final is now a named outcome; this test
// pins all three halves of it, plus the safety property that nothing is
// delivered.
func TestTranscriptionWorkerNamesAnEmptyFinalTranscript(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: Transcript{
			Text:     "   ",
			Provider: "deepgram",
			Model:    "nova-3",
			Language: "de",
			Duration: 100 * time.Millisecond,
		},
	}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    runner,
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
		Target:     "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	observer.mu.Lock()
	logs := append([]string(nil), observer.logs...)
	states := append([]string(nil), observer.states...)
	committed := append([]string(nil), observer.committed...)
	observer.mu.Unlock()

	var warned string
	for _, l := range logs {
		if strings.HasPrefix(l, "warn:") && strings.Contains(l, EmptyFinalTranscriptMessage) {
			warned = l
		}
	}
	if warned == "" {
		t.Fatalf("an empty final transcript must warn, got logs = %#v", logs)
	}
	// The log has to carry enough to diagnose the pinned-language case, which
	// is the whole reason this outcome exists.
	for _, want := range []string{"deepgram", "nova-3", "de"} {
		if !strings.Contains(warned, want) {
			t.Errorf("empty-final warn log must name %q for diagnosis, got %q", want, warned)
		}
	}

	var sawVisibleState bool
	for _, s := range states {
		if strings.Contains(s, EmptyFinalTranscriptMessage) {
			sawVisibleState = true
		}
	}
	if !sawVisibleState {
		t.Fatalf("an empty final transcript must set a visible state, got states = %#v", states)
	}

	if delivered := output.snapshot(); len(delivered) != 0 {
		t.Fatalf("an empty transcript must never be delivered, got %#v", delivered)
	}
	if len(committed) != 0 {
		t.Fatalf("an empty transcript must not be announced as committed, got %#v", committed)
	}
}

func TestTranscriptionWorkerSkipsLowConfidenceWhenDisabled(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: Transcript{
			Text:     "Ultracord",
			Provider: "deepgram",
			Duration: 50 * time.Millisecond,
			Words:    []WordConfidence{{Text: "Ultracord", Confidence: 0.10}},
		},
	}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:                time.Second,
		QueueSize:              1,
		Runner:                 runner,
		Output:                 output,
		Observer:               observer,
		LowConfidenceThreshold: 0, // disabled
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
		Target:     "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	worker.Close()
	worker.Wait()

	observer.mu.Lock()
	defer observer.mu.Unlock()
	for _, l := range observer.logs {
		if strings.Contains(l, "Low-confidence words") {
			t.Fatalf("threshold disabled, must not emit low-confidence log, got %q", l)
		}
	}
}

func TestTranscriptionWorkerProcessesJobs(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: Transcript{
			Text:     "hello world",
			Provider: "local",
			Duration: 1500 * time.Millisecond,
		},
	}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    runner,
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
			Prefix:       "\n\n",
			QuickNote:    true,
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	if got := observer.committed; len(got) != 1 || got[0] != "\n\nhello world" {
		t.Fatalf("observer committed = %#v", got)
	}
	if got := observer.quickNotes; len(got) != 1 || !got[0] {
		t.Fatalf("observer quickNotes = %#v", got)
	}
	if got := observer.states; len(got) < 2 || got[0] != "processing:"+DefaultProcessingMessage || got[1] != "done:\n\nhello world" {
		t.Fatalf("observer states = %#v", got)
	}
	if len(output.delivered) != 1 {
		t.Fatalf("delivered outputs = %d, want 1", len(output.delivered))
	}
	if got, want := output.delivered[0].transcript.Text, "\n\nhello world"; got != want {
		t.Fatalf("delivered transcript = %q, want %q", got, want)
	}
}

func TestTranscriptionWorkerNormalizesPCMOnlySubmission(t *testing.T) {
	pcm := []byte(strings.Repeat("a", 6400))
	transcriber := &capturingAudioTranscriber{
		transcript: Transcript{
			Text:     "pcm only",
			Provider: "test",
			Duration: 50 * time.Millisecond,
		},
	}
	output := &recordingOutput{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout: time.Second,
		Runner:  NewTranscriptionRunner(transcriber, nil),
		Output:  output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:      pcm,
			Language: "de",
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	worker.Close()
	worker.Wait()

	if len(transcriber.audio) != 44+len(pcm) {
		t.Fatalf("transcriber audio len = %d, want WAV header plus PCM", len(transcriber.audio))
	}
	if string(transcriber.audio[:4]) != "RIFF" || string(transcriber.audio[8:12]) != "WAVE" {
		t.Fatalf("transcriber audio is not a WAV payload")
	}
	if got, want := transcriber.duration, PCMDurationSecs(pcm); got != want {
		t.Fatalf("transcriber duration = %v, want %v", got, want)
	}
	if got, want := transcriber.language, "de"; got != want {
		t.Fatalf("transcriber language = %q, want %q", got, want)
	}
	if delivered := output.snapshot(); len(delivered) != 1 || delivered[0].transcript.Text != "pcm only" {
		t.Fatalf("delivered = %#v, want pcm-only transcript", delivered)
	}
}

func TestTranscriptionWorkerSkipsDuplicateSegmentCommit(t *testing.T) {
	transcriber := &countingTranscriber{
		transcript: Transcript{
			Text:     "segment text",
			Provider: "test",
			Duration: 25 * time.Millisecond,
		},
	}
	output := &recordingOutput{}
	observer := &recordingObserver{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 2,
		Runner:    NewTranscriptionRunner(transcriber, nil),
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	job := TranscriptionJob{
		Submission: Submission{
			PCM:          []byte(strings.Repeat("d", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "de",
			SessionID:    42,
			SegmentID:    7,
			SegmentFinal: true,
		},
		Target: "editor",
	}
	if err := worker.Submit(job); err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	if err := worker.Submit(job); err != nil {
		t.Fatalf("Submit(duplicate) error = %v", err)
	}
	worker.Close()
	worker.Wait()

	if got := transcriber.count(); got != 1 {
		t.Fatalf("transcriber calls = %d, want 1", got)
	}
	delivered := output.snapshot()
	if len(delivered) != 1 {
		t.Fatalf("delivered = %d, want 1", len(delivered))
	}
	if got := delivered[0].transcript.SessionID; got != 42 {
		t.Fatalf("delivered session id = %d, want 42", got)
	}
	if got := delivered[0].transcript.SegmentID; got != 7 {
		t.Fatalf("delivered segment id = %d, want 7", got)
	}
	if !delivered[0].transcript.SegmentFinal {
		t.Fatal("delivered SegmentFinal = false, want true")
	}
	if !observer.hasLog("Duplicate transcript segment skipped") {
		t.Fatalf("observer logs = %#v, want duplicate skip log", observer.logs)
	}
}

func TestTranscriptionWorkerProviderStreamDraftsDoNotDeliverAndFinalsDeduplicate(t *testing.T) {
	output := &recordingOutput{}
	observer := &recordingObserver{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    NewTranscriptionRunner(stubTranscriber{}, nil),
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx := context.Background()
	draft := DictationStreamEvent{
		SessionID:      99,
		SegmentID:      3,
		ProviderItemID: "provider:3",
		Text:           "draft text",
		Provider:       "fake",
		Model:          "stream",
	}
	if err := worker.HandleDictationStreamEvent(ctx, draft, DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
		t.Fatalf("HandleDictationStreamEvent(draft) error = %v", err)
	}
	if delivered := output.snapshot(); len(delivered) != 0 {
		t.Fatalf("draft delivered output = %d, want 0", len(delivered))
	}
	if len(observer.drafts) != 1 || observer.drafts[0] != "draft text" {
		t.Fatalf("observer drafts = %#v, want draft text", observer.drafts)
	}

	final := draft
	final.Text = "final text"
	final.IsFinal = true
	if err := worker.HandleDictationStreamEvent(ctx, final, DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
		t.Fatalf("HandleDictationStreamEvent(final) error = %v", err)
	}
	if err := worker.HandleDictationStreamEvent(ctx, final, DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
		t.Fatalf("HandleDictationStreamEvent(duplicate final) error = %v", err)
	}

	delivered := output.snapshot()
	if len(delivered) != 1 {
		t.Fatalf("delivered finals = %d, want 1", len(delivered))
	}
	if got := delivered[0].transcript.Text; got != "final text" {
		t.Fatalf("delivered text = %q, want final text", got)
	}
	if got := delivered[0].transcript.SessionID; got != 99 {
		t.Fatalf("delivered session id = %d, want 99", got)
	}
	if got := delivered[0].transcript.SegmentID; got != 3 {
		t.Fatalf("delivered segment id = %d, want 3", got)
	}
	if !delivered[0].transcript.SegmentFinal {
		t.Fatal("delivered SegmentFinal = false, want true")
	}
	if !observer.hasLog("Duplicate transcript segment skipped") {
		t.Fatalf("observer logs = %#v, want duplicate skip log", observer.logs)
	}
}

func TestTranscriptionWorkerAllowsRepeatedTextAcrossDistinctSegments(t *testing.T) {
	transcriber := &countingTranscriber{
		transcript: Transcript{
			Text:     "same repeated phrase",
			Provider: "test",
			Duration: 20 * time.Millisecond,
		},
	}
	output := &recordingOutput{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 2,
		Runner:    NewTranscriptionRunner(transcriber, nil),
		Output:    output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	for i := uint64(1); i <= 2; i++ {
		if err := worker.Submit(TranscriptionJob{
			Submission: Submission{
				PCM:          []byte(strings.Repeat("r", 6400)),
				WAV:          []byte("wav"),
				DurationSecs: 0.2,
				Language:     "de",
				SessionID:    123,
				SegmentID:    i,
				SegmentFinal: true,
			},
		}); err != nil {
			t.Fatalf("Submit(segment %d) error = %v", i, err)
		}
	}
	worker.Close()
	worker.Wait()

	if got := transcriber.count(); got != 2 {
		t.Fatalf("transcriber calls = %d, want 2 for repeated text in distinct segments", got)
	}
	if delivered := output.snapshot(); len(delivered) != 2 {
		t.Fatalf("delivered repeated segments = %d, want 2", len(delivered))
	}
}

func TestTranscriptionWorkerTranscribesSegmentsInParallelAndDeliversCombinedTranscript(t *testing.T) {
	transcriber := newParallelSegmentTranscriber(2)
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(transcriber, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:     time.Second,
		QueueSize:   1,
		Runner:      runner,
		Output:      output,
		Transformer: replacingTranscriptTransformer{},
		Observer:    observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:          []byte(strings.Repeat("x", 12800)),
			WAV:          []byte("full-session"),
			DurationSecs: 0.4,
			Language:     "de",
		},
		Segments: []Submission{
			{WAV: []byte("segment-1"), DurationSecs: 0.2, Language: "de"},
			{WAV: []byte("segment-2"), DurationSecs: 0.2, Language: "de"},
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case <-transcriber.started:
	case <-time.After(time.Second):
		t.Fatal("segments were not started in parallel")
	}
	if got := output.snapshot(); len(got) != 0 {
		t.Fatalf("delivered before all segment transcripts completed: %#v", got)
	}
	close(transcriber.release)
	worker.Close()
	worker.Wait()

	if got := transcriber.maxConcurrency(); got < 2 {
		t.Fatalf("max segment concurrency = %d, want at least 2", got)
	}
	delivered := output.snapshot()
	if len(delivered) != 1 {
		t.Fatalf("delivered outputs = %d, want 1", len(delivered))
	}
	if got, want := delivered[0].transcript.Text, "Kombify"; got != want {
		t.Fatalf("delivered transcript = %q, want %q", got, want)
	}
	if got, want := delivered[0].target, any("editor"); got != want {
		t.Fatalf("delivered target = %v, want %v", got, want)
	}
	if got := observer.committed; len(got) != 1 || got[0] != "Kombify" {
		t.Fatalf("observer committed = %#v", got)
	}
}

func TestTranscriptionWorkerHandlesTranscriberErrors(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{err: errors.New("boom")}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    runner,
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "de",
		},
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	if len(output.delivered) != 0 {
		t.Fatalf("delivered outputs = %d, want 0", len(output.delivered))
	}
	if got := observer.states; len(got) < 2 || got[0] != "processing:"+DefaultProcessingMessage || got[1] != "idle:" {
		t.Fatalf("observer states = %#v", got)
	}
	hasSTTError := false
	for _, log := range observer.logs {
		if strings.HasPrefix(log, "error:STT error: boom") {
			hasSTTError = true
			break
		}
	}
	if !hasSTTError {
		t.Fatalf("observer logs = %#v", observer.logs)
	}
}

func TestTranscriptionWorkerDeliversBeforeHistoryPersistence(t *testing.T) {
	store := newBlockingPersistence()
	output := &recordingOutput{}
	commitObserver := &testCommitObserver{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: Transcript{
			Text:     "  fast dictation  ",
			Language: "en",
			Provider: "local",
			Model:    "ggml-large-v3-turbo.bin",
			Duration: 250 * time.Millisecond,
		},
	}, store).WithObserver(commitObserver)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    runner,
		Output:    output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
			Prefix:       "\n\n",
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	worker.Close()

	var delivered []deliveredTranscript
	deadline := time.After(time.Second)
	for {
		delivered = output.snapshot()
		if len(delivered) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("output was not delivered before history persistence finished")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if got, want := delivered[0].transcript.Text, "\n\nfast dictation"; got != want {
		t.Fatalf("delivered transcript = %q, want %q", got, want)
	}

	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("history persistence did not start")
	}

	close(store.release)
	worker.Wait()

	select {
	case <-store.done:
	default:
		t.Fatal("history persistence did not finish")
	}
	if len(commitObserver.completions) != 1 {
		t.Fatalf("commit observer completions = %d, want 1", len(commitObserver.completions))
	}
	if !commitObserver.completions[0].TranscriptionPersisted {
		t.Fatal("transcription persistence notification missing")
	}
}

func TestTranscriptionWorkerRequiresTranscriber(t *testing.T) {
	_, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Runner: NewTranscriptionRunner(nil, nil),
	})
	if !errors.Is(err, ErrMissingTranscriber) {
		t.Fatalf("NewTranscriptionWorker() error = %v, want %v", err, ErrMissingTranscriber)
	}
}

func TestTranscriptionTimeoutForDurationScalesBeyondDefault(t *testing.T) {
	timeout := transcriptionTimeoutForDuration(30*time.Second, 90)

	if timeout <= 30*time.Second {
		t.Fatalf("timeout = %v, want more than legacy 30s default", timeout)
	}
	if timeout < 4*time.Minute {
		t.Fatalf("timeout = %v, want enough headroom for long local STT captures", timeout)
	}
}

func TestTranscriptionWorkerSubmitWhileClosingDoesNotPanic(t *testing.T) {
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 8,
		Runner: NewTranscriptionRunner(stubTranscriber{
			transcript: Transcript{Text: "ok", Provider: "local", Duration: 10 * time.Millisecond},
		}, nil),
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	var panicCount atomic.Int64
	var wg sync.WaitGroup
	job := TranscriptionJob{
		Submission: Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
		},
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				func() {
					defer func() {
						if recover() != nil {
							panicCount.Add(1)
						}
					}()
					_ = worker.Submit(job)
				}()
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	worker.Close()
	wg.Wait()
	worker.Wait()

	if panicCount.Load() != 0 {
		t.Fatalf("Submit panicked %d time(s)", panicCount.Load())
	}
}

func TestTranscriptionWorkerSuccessLogRedactsTranscriptText(t *testing.T) {
	observer := &recordingObserver{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner: NewTranscriptionRunner(stubTranscriber{
			transcript: Transcript{
				Text:     "secret customer text",
				Provider: "local",
				Duration: 1500 * time.Millisecond,
			},
		}, nil),
		Observer: observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
		},
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	joined := strings.Join(observer.logs, "\n")
	if strings.Contains(joined, "secret customer text") {
		t.Fatalf("expected redacted success log, got logs: %s", joined)
	}
	if !strings.Contains(joined, "transcript ready") {
		t.Fatalf("expected success log marker, got logs: %s", joined)
	}
}

// panicOutput panics on delivery, simulating a host output-injection callback
// (e.g. WebView2/text-field paste) that faults during a transcript hand-off.
type panicOutput struct{}

func (panicOutput) Deliver(_ context.Context, _ Transcript, _ any) error {
	panic("boom in output delivery")
}

// panicTranscriber panics on every call, simulating a provider adapter that
// faults while transcribing a segment.
type panicTranscriber struct{}

func (panicTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, _ string) (Transcript, error) {
	panic("boom in transcriber")
}

// A panic in synchronous delivery runs on the worker's job-loop goroutine.
// Without handleJobSafely's per-job recover it would crash the whole process
// (the desktop host installs no top-level panic handler). The worker must
// instead log the panic and keep processing the next job.
func TestTranscriptionWorkerRecoversFromOutputPanic(t *testing.T) {
	transcriber := &countingTranscriber{transcript: Transcript{Text: "hallo", Provider: "deepgram"}}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 4,
		Runner:    NewTranscriptionRunner(transcriber, nil),
		Output:    panicOutput{},
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	for i := 0; i < 2; i++ {
		if err := worker.Submit(TranscriptionJob{
			Submission: Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
			Target:     "editor",
		}); err != nil {
			t.Fatalf("Submit() job %d error = %v", i, err)
		}
	}

	worker.Close()
	worker.Wait() // must return; a leaked panic would have aborted the test binary

	if got := transcriber.count(); got != 2 {
		t.Fatalf("transcriber calls = %d, want 2 (worker must keep running after a delivery panic)", got)
	}
}

// A multi-segment job runs each segment on its own goroutine, so a panic there
// is invisible to handleJobSafely's recover and needs the segment-level recover
// added in transcribeSegmentsParallel. Without it, this crashes the process.
func TestTranscriptionWorkerRecoversFromSegmentPanic(t *testing.T) {
	output := &recordingOutput{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    NewTranscriptionRunner(panicTranscriber{}, nil),
		Output:    output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{PCM: []byte(strings.Repeat("x", 6400)), WAV: []byte("full"), DurationSecs: 0.4, Language: "de"},
		Segments: []Submission{
			{WAV: []byte("segment-1"), DurationSecs: 0.2, Language: "de"},
			{WAV: []byte("segment-2"), DurationSecs: 0.2, Language: "de"},
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait() // must return despite both segment goroutines panicking

	if got := output.snapshot(); len(got) != 0 {
		t.Fatalf("expected no delivery after all segments panicked, got %#v", got)
	}
}
