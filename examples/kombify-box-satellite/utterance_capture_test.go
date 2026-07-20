package main

import "testing"

func TestUtteranceCaptureAuthorityRequiresLosslessSilenceBoundary(t *testing.T) {
	base := utteranceCaptureResult{Bytes: 32000, BoundaryVerified: true, Spoke: true, End: utteranceEndSilence}
	if !base.Authoritative() {
		t.Fatal("lossless silence-terminated capture was not authoritative")
	}

	tests := []utteranceCaptureResult{
		{Bytes: 0, BoundaryVerified: true, Spoke: true, End: utteranceEndSilence},
		{Bytes: 32000, Spoke: true, End: utteranceEndSilence},
		{Bytes: 32000, BoundaryVerified: true, Spoke: false, End: utteranceEndSilence},
		{Bytes: 32000, BoundaryVerified: true, Spoke: true, End: utteranceEndMaxDuration},
		{Bytes: 32000, BoundaryVerified: true, Spoke: true, End: utteranceEndSourceClosed},
		{Bytes: 32000, BoundaryVerified: true, Spoke: true, End: utteranceEndSilence, DroppedFrames: 1},
		{Bytes: 32000, BoundaryVerified: true, Spoke: true, End: utteranceEndSilence, PlaybackContaminated: true},
	}
	for _, capture := range tests {
		if capture.Authoritative() {
			t.Fatalf("non-authoritative capture accepted: %#v", capture)
		}
	}
}

func TestUtteranceBoundaryConsumesQueuedCorrectionBeforeSilence(t *testing.T) {
	boundary := newUtteranceBoundary(12, 20) // 640 silent PCM bytes
	if end := boundary.observe(320, true); end != "" {
		t.Fatalf("voice ended capture: %q", end)
	}
	if end := boundary.observe(320, false); end != "" {
		t.Fatalf("partial silence ended capture: %q", end)
	}
	// This correction may already be queued behind the first silent frame. A
	// wall-clock detector could miss it; ordered sample accounting must reset.
	if end := boundary.observe(320, true); end != "" {
		t.Fatalf("queued correction ended capture: %q", end)
	}
	if end := boundary.observe(320, false); end != "" {
		t.Fatalf("first post-correction silence ended capture: %q", end)
	}
	if end := boundary.observe(320, false); end != utteranceEndSilence {
		t.Fatalf("complete post-correction silence ended with %q", end)
	}
	if got := boundary.result(utteranceEndSilence); got.Bytes != 1600 || !got.Spoke {
		t.Fatalf("boundary result = %#v", got)
	}
}

func TestUtteranceBoundaryHardSampleCapPrecedesAuthority(t *testing.T) {
	boundary := newUtteranceBoundary(1, 1400)
	if boundary.wouldExceed(32001) != true {
		t.Fatal("frame beyond hard PCM cap was accepted")
	}
	if end := boundary.observe(32000, true); end != utteranceEndMaxDuration {
		t.Fatalf("hard sample cap ended with %q", end)
	}
	if boundary.result(utteranceEndMaxDuration).Authoritative() {
		t.Fatal("maximum-duration capture became authoritative")
	}
}

func TestAuthorityBoundaryRejectsSpeechUntilCleanPostWakeSilence(t *testing.T) {
	boundary := newAuthorityUtteranceBoundary(12, 20)
	if end := boundary.observe(3200, true); end != "" {
		t.Fatalf("wake-tail speech ended capture: %q", end)
	}
	if boundary.result("").BoundaryVerified {
		t.Fatal("wake-tail speech established a clean authority boundary")
	}
	if end := boundary.observe(4000, false); end != "" {
		t.Fatalf("partial lead silence ended capture: %q", end)
	}
	if end := boundary.observe(4000, false); end != utteranceEndUncleanStart {
		t.Fatalf("speech-contaminated lead ended with %q", end)
	}
	if boundary.result(utteranceEndUncleanStart).Authoritative() {
		t.Fatal("speech before the clean boundary became authoritative")
	}

	boundary = newAuthorityUtteranceBoundary(12, 20)
	if end := boundary.observe(8000, false); end != "" {
		t.Fatalf("clean lead silence ended capture: %q", end)
	}
	if got := boundary.result(""); !got.BoundaryVerified || got.AuthorityStartBytes != 8000 || got.Spoke {
		t.Fatalf("verified boundary = %#v", got)
	}
	if end := boundary.observe(320, true); end != "" {
		t.Fatalf("post-boundary user speech ended capture: %q", end)
	}
	if end := boundary.observe(640, false); end != utteranceEndSilence {
		t.Fatalf("post-user silence ended with %q", end)
	}
	if !boundary.result(utteranceEndSilence).Authoritative() {
		t.Fatal("clean post-wake utterance was not authoritative")
	}
}

func TestExtractAuthorityPCMExcludesAndDetachesPreBoundaryAudio(t *testing.T) {
	captured := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	result := utteranceCaptureResult{
		Bytes:               len(captured),
		AuthorityStartBytes: 4,
		BoundaryVerified:    true,
		Spoke:               true,
		End:                 utteranceEndSilence,
	}
	authorityPCM, ok := extractAuthorityPCM(result, captured)
	if !ok || string(authorityPCM) != string([]byte{5, 6, 7, 8}) {
		t.Fatalf("authority PCM = %v ok=%v", authorityPCM, ok)
	}
	captured[4] = 99
	if authorityPCM[0] != 5 {
		t.Fatal("authority PCM aliases the mutable capture buffer")
	}

	result.Bytes++
	if _, ok := extractAuthorityPCM(result, captured); ok {
		t.Fatal("capture length mismatch produced authority PCM")
	}
}
