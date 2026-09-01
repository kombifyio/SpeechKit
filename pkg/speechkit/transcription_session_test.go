package speechkit

import "testing"

// A meeting records several sources at once and each source numbers its own
// segments from one, so the first segment of the microphone and the first
// segment of the system loopback carry identical session and segment ids. They
// are different speech and must both be admitted.
func TestLedgerAdmitsTheSameSegmentNumberOnDifferentCaptureChannels(t *testing.T) {
	ledger := NewTranscriptSessionLedger()

	mic := transcriptSegmentKey(Submission{
		CaptureChannel: CaptureChannelMicrophone,
		SessionID:      1,
		SegmentID:      1,
	})
	system := transcriptSegmentKey(Submission{
		CaptureChannel: CaptureChannelSystem,
		SessionID:      1,
		SegmentID:      1,
	})

	if !ledger.Begin(mic) {
		t.Fatal("the microphone segment was rejected")
	}
	if !ledger.Begin(system) {
		t.Fatal("the system segment was discarded as a duplicate of the microphone segment")
	}

	ledger.Commit(mic)
	ledger.Commit(system)
	if ledger.Begin(system) {
		t.Fatal("a genuinely repeated segment was admitted twice")
	}
}

func TestLedgerScopesReusedControllerCountersByRecordingSession(t *testing.T) {
	ledger := NewTranscriptSessionLedger()
	firstMeeting := transcriptSegmentKey(Submission{
		RecordingSessionID: 101,
		CaptureChannel:     CaptureChannelMicrophone,
		SessionID:          1,
		SegmentID:          1,
	})
	secondMeeting := transcriptSegmentKey(Submission{
		RecordingSessionID: 102,
		CaptureChannel:     CaptureChannelMicrophone,
		SessionID:          1,
		SegmentID:          1,
	})

	if !ledger.Begin(firstMeeting) {
		t.Fatal("first meeting segment was rejected")
	}
	ledger.Commit(firstMeeting)
	if !ledger.Begin(secondMeeting) {
		t.Fatal("a fresh meeting collided with the prior meeting's controller-local counters")
	}
	ledger.Commit(secondMeeting)
	if ledger.Begin(secondMeeting) {
		t.Fatal("a duplicate within the same durable recording session was admitted")
	}
}

func TestLedgerRetiresOnlyEndedRecordingSessions(t *testing.T) {
	ledger := NewTranscriptSessionLedger()
	ended := TranscriptSegmentKey{RecordingSessionID: 201, SessionID: 1, SegmentID: 1}
	active := TranscriptSegmentKey{RecordingSessionID: 202, SessionID: 1, SegmentID: 1}
	if !ledger.Begin(ended) || !ledger.Begin(active) {
		t.Fatal("initial segments were rejected")
	}
	ledger.Commit(ended)
	ledger.Commit(active)

	ledger.EndRecordingSession(201)

	if !ledger.Begin(ended) {
		t.Fatal("ended recording-session state was retained")
	}
	if ledger.Begin(active) {
		t.Fatal("retiring an earlier meeting broke deduplication for the active meeting")
	}
}

// A meeting records other people. What it keeps is what was said, not the
// sound of them saying it, and that has to hold regardless of how the host has
// configured audio storage.
func TestMeetingCaptureNeverPersistsAudio(t *testing.T) {
	audio := []byte("pretend this is a wav")

	if got := persistableAudio(Submission{WAV: audio}); len(got) == 0 {
		t.Fatal("ordinary dictation lost its audio")
	}
	if got := persistableAudio(Submission{WAV: audio, CaptureChannel: CaptureChannelMicrophone}); got != nil {
		t.Fatal("a meeting's microphone audio would have been stored")
	}
	if got := persistableAudio(Submission{WAV: audio, CaptureChannel: CaptureChannelSystem}); got != nil {
		t.Fatal("a meeting's system audio would have been stored")
	}
}

// A meeting is a recording, not dictation. Its speech must never be typed into
// whatever window has focus: in the first live test the other side of the call
// was pasted straight into the user's own note pane, which is both surprising
// and destroys the notes the write-up anchors on.
func TestMeetingCaptureIsNeverTypedIntoTheFocusedWindow(t *testing.T) {
	if !deliverableAsOutput(Submission{}) {
		t.Fatal("ordinary dictation stopped being delivered")
	}
	if deliverableAsOutput(Submission{CaptureChannel: CaptureChannelMicrophone}) {
		t.Fatal("the meeting's own microphone would have been typed into the focused window")
	}
	if deliverableAsOutput(Submission{CaptureChannel: CaptureChannelSystem}) {
		t.Fatal("the other side of the call would have been typed into the focused window")
	}
}
