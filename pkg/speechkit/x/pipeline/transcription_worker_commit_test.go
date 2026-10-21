package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestTranscriptionWorkerProcessesJobs(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: speechkit.Transcript{
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

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
			Prefix:       "\n\n",
			QuickNote:    true,
		},
		Target: speechkit.TargetRef{Kind: speechkit.TargetKindEditor, ID: "notes"},
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
	if got := observer.states; len(got) < 2 || got[0] != "processing:"+speechkit.DefaultProcessingMessage || got[1] != "done:\n\nhello world" {
		t.Fatalf("observer states = %#v", got)
	}
	if len(output.delivered) != 1 {
		t.Fatalf("delivered outputs = %d, want 1", len(output.delivered))
	}
	if got, want := output.delivered[0].transcript.Text, "\n\nhello world"; got != want {
		t.Fatalf("delivered transcript = %q, want %q", got, want)
	}
	// The typed OutputTarget must reach the host output unchanged so a host can
	// route by kind without asserting its own concrete type.
	if got, want := speechkit.TargetKind(output.delivered[0].target), speechkit.TargetKindEditor; got != want {
		t.Fatalf("delivered target kind = %q, want %q (target=%#v)", got, want, output.delivered[0].target)
	}
	if ref, ok := output.delivered[0].target.(speechkit.TargetRef); !ok || ref.ID != "notes" {
		t.Fatalf("delivered target = %#v, want the submitted TargetRef", output.delivered[0].target)
	}
}

func TestTranscriptionWorkerNormalizesPCMOnlySubmission(t *testing.T) {
	pcm := []byte(strings.Repeat("a", 6400))
	transcriber := &capturingAudioTranscriber{
		transcript: speechkit.Transcript{
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

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
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
	if got, want := transcriber.duration, speechkit.PCMDurationSecs(pcm); got != want {
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
		transcript: speechkit.Transcript{
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

	job := speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
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

func TestTranscriptionWorkerProcessesSequentialMeetingsWithReusedControllerCounters(t *testing.T) {
	transcriber := &countingTranscriber{transcript: speechkit.Transcript{
		Text:     "meeting words",
		Provider: "test",
		Duration: time.Millisecond,
	}}
	persistence := &countingPersistence{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 2,
		Runner:    NewTranscriptionRunner(transcriber, persistence),
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	submitMeeting := func(recordingSessionID int64) {
		t.Helper()
		err := worker.Submit(speechkit.TranscriptionJob{Submission: speechkit.Submission{
			WAV:                []byte("private meeting audio"),
			DurationSecs:       0.1,
			RecordingSessionID: recordingSessionID,
			CaptureChannel:     speechkit.CaptureChannelMicrophone,
			SessionID:          1,
			SegmentID:          1,
			SegmentFinal:       true,
		}})
		if err != nil {
			t.Fatalf("Submit(recording session %d) error = %v", recordingSessionID, err)
		}
	}
	submitMeeting(301)
	submitMeeting(302)
	worker.Close()
	worker.Wait()

	if got := transcriber.count(); got != 2 {
		t.Fatalf("provider calls = %d, want both meetings", got)
	}
	saves, audioLengths := persistence.snapshot()
	if saves != 2 {
		t.Fatalf("persisted transcripts = %d, want both meetings", saves)
	}
	for _, audioLen := range audioLengths {
		if audioLen != 0 {
			t.Fatal("meeting audio reached persistence")
		}
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
	draft := speechkit.DictationStreamEvent{
		SessionID:      99,
		SegmentID:      3,
		ProviderItemID: "provider:3",
		Text:           "draft text",
		Provider:       "fake",
		Model:          "stream",
	}
	if err := worker.HandleDictationStreamEvent(ctx, draft, speechkit.DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
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
	if err := worker.HandleDictationStreamEvent(ctx, final, speechkit.DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
		t.Fatalf("HandleDictationStreamEvent(final) error = %v", err)
	}
	if err := worker.HandleDictationStreamEvent(ctx, final, speechkit.DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
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

func TestTranscriptionWorkerLiveInjectsKeepSentenceGap(t *testing.T) {
	output := &recordingOutput{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    NewTranscriptionRunner(stubTranscriber{}, nil),
		Output:    output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}
	ctx := context.Background()
	first := speechkit.DictationStreamEvent{SessionID: 4, SegmentID: 1, ProviderItemID: "a", Text: "Das ist ein Satz.", IsFinal: true}
	second := speechkit.DictationStreamEvent{SessionID: 4, SegmentID: 2, ProviderItemID: "b", Text: "Und weiter.", IsFinal: true}
	if err := worker.HandleDictationStreamEvent(ctx, first, speechkit.DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
		t.Fatalf("first final: %v", err)
	}
	if err := worker.HandleDictationStreamEvent(ctx, second, speechkit.DictationStreamSinkOptions{Target: "editor", Language: "de"}); err != nil {
		t.Fatalf("second final: %v", err)
	}
	delivered := output.snapshot()
	if len(delivered) != 2 {
		t.Fatalf("delivered = %d, want 2", len(delivered))
	}
	paste := delivered[0].transcript.Text + delivered[1].transcript.Text
	if !strings.Contains(paste, "Satz. Und") {
		t.Fatalf("injected paste = %q, want a space between live sentences", paste)
	}
}

func TestTranscriptionWorkerAllowsRepeatedTextAcrossDistinctSegments(t *testing.T) {
	transcriber := &countingTranscriber{
		transcript: speechkit.Transcript{
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
		if err := worker.Submit(speechkit.TranscriptionJob{
			Submission: speechkit.Submission{
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
