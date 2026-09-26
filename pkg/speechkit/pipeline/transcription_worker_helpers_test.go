package pipeline

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type stubTranscriber struct {
	transcript speechkit.Transcript
	err        error
}

func (s stubTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, _ string) (speechkit.Transcript, error) {
	if s.err != nil {
		return speechkit.Transcript{}, s.err
	}
	return s.transcript, nil
}

type capturingAudioTranscriber struct {
	audio      []byte
	duration   float64
	language   string
	transcript speechkit.Transcript
}

func (s *capturingAudioTranscriber) Transcribe(_ context.Context, audio []byte, durationSecs float64, language string) (speechkit.Transcript, error) {
	s.audio = append([]byte(nil), audio...)
	s.duration = durationSecs
	s.language = language
	return s.transcript, nil
}

type countingTranscriber struct {
	mu         sync.Mutex
	calls      int
	transcript speechkit.Transcript
}

type countingPersistence struct {
	mu       sync.Mutex
	saves    int
	audioLen []int
}

func (s *countingPersistence) SaveQuickNote(context.Context, string, string, string, int64, int64, []byte) (int64, error) {
	return 0, nil
}

func (s *countingPersistence) GetQuickNoteText(context.Context, int64) (string, error) {
	return "", nil
}

func (s *countingPersistence) UpdateQuickNote(context.Context, int64, string) error {
	return nil
}

func (s *countingPersistence) UpdateQuickNoteCapture(context.Context, int64, string, string, int64, int64, []byte) error {
	return nil
}

func (s *countingPersistence) SaveTranscription(_ context.Context, _, _, _, _ string, _, _ int64, audio []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	s.audioLen = append(s.audioLen, len(audio))
	return nil
}

func (s *countingPersistence) snapshot() (int, []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves, append([]int(nil), s.audioLen...)
}

func (s *countingTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, language string) (speechkit.Transcript, error) {
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

func (s *parallelSegmentTranscriber) Transcribe(ctx context.Context, audio []byte, _ float64, language string) (speechkit.Transcript, error) {
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
		return speechkit.Transcript{}, ctx.Err()
	}

	textByAudio := map[string]string{
		"segment-1": "kombi",
		"segment-2": "fire",
	}
	return speechkit.Transcript{
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

func (replacingTranscriptTransformer) Transform(_ context.Context, transcript speechkit.Transcript) (speechkit.Transcript, error) {
	transcript.Text = strings.ReplaceAll(transcript.Text, "kombi fire", "Kombify")
	return transcript, nil
}

type deliveredTranscript struct {
	transcript speechkit.Transcript
	target     any
}

type recordingOutput struct {
	mu        sync.Mutex
	delivered []deliveredTranscript
	err       error
}

func (o *recordingOutput) Deliver(_ context.Context, transcript speechkit.Transcript, target any) error {
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
	mu           sync.Mutex
	states       []string
	logs         []string
	drafts       []string
	committed    []string
	quickNotes   []bool
	finalization speechkit.TranscriptionFinalization
}

func (o *recordingObserver) OnTranscriptionFinalized(_ speechkit.Transcript, f speechkit.TranscriptionFinalization, _ any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finalization = f
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

func (o *recordingObserver) OnTranscriptCommitted(transcript speechkit.Transcript, quickNote bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.committed = append(o.committed, transcript.Text)
	o.quickNotes = append(o.quickNotes, quickNote)
}

func (o *recordingObserver) OnTranscriptDraft(transcript speechkit.Transcript) {
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
