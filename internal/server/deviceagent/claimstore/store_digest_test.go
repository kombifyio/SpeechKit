package claimstore

import (
	"bytes"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestHMACDigestCanonicalAndDomainSeparated(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	requestID := testUUIDV7(now, 1)
	key := bytes.Repeat([]byte{0x5a}, minimumDigestKeyLen)
	req := CanonicalRequest{
		PairedDeviceID: "pair-kitchen-1",
		RequestID:      requestID,
		RuleID:         "kitchen-light-off",
		Locale:         "de-DE",
		Text:           "Schalte das Licht aus",
		EntityID:       "light.kitchen",
		ExpectedState:  "off",
	}

	digest, err := HMACDigest(key, req)
	if err != nil {
		t.Fatalf("HMACDigest: %v", err)
	}
	trimmed, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: " pair-kitchen-1 ",
		RequestID:      strings.ToUpper(requestID),
		RuleID:         " kitchen-light-off ",
		Locale:         " de-DE ",
		Text:           "  Schalte das Licht aus  ",
		EntityID:       " light.kitchen ",
		ExpectedState:  " off ",
	})
	if err != nil {
		t.Fatalf("HMACDigest normalized: %v", err)
	}
	if digest != trimmed {
		t.Fatal("equivalent normalized requests produced different digests")
	}

	changed, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: req.PairedDeviceID,
		RequestID:      req.RequestID,
		RuleID:         req.RuleID,
		Locale:         req.Locale,
		Text:           "Schalte das Licht an",
		EntityID:       req.EntityID,
		ExpectedState:  req.ExpectedState,
	})
	if err != nil {
		t.Fatalf("HMACDigest changed: %v", err)
	}
	if changed == digest {
		t.Fatal("different command text produced the same digest")
	}
	boundToAudio, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: req.PairedDeviceID,
		RequestID:      req.RequestID,
		RuleID:         req.RuleID,
		Locale:         req.Locale,
		Text:           req.Text,
		EntityID:       req.EntityID,
		ExpectedState:  req.ExpectedState,
		InputSHA256:    strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("HMACDigest audio-bound: %v", err)
	}
	if boundToAudio == digest {
		t.Fatal("audio-bound media request reused the legacy v1 digest")
	}
	otherAudio := req
	otherAudio.InputSHA256 = strings.Repeat("b", 64)
	otherAudioDigest, err := HMACDigest(key, otherAudio)
	if err != nil {
		t.Fatalf("HMACDigest other audio: %v", err)
	}
	if otherAudioDigest == boundToAudio {
		t.Fatal("different exact audio digests produced the same claim digest")
	}
	invalidAudio := req
	invalidAudio.InputSHA256 = strings.Repeat("A", 64)
	if _, err := HMACDigest(key, invalidAudio); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("uppercase audio digest error = %v, want ErrInvalidCanonicalRequest", err)
	}
	if _, err := HMACDigest([]byte("short"), req); !errors.Is(err, ErrDigestKeyTooShort) {
		t.Fatalf("short key error = %v, want ErrDigestKeyTooShort", err)
	}
	tooLong := req
	tooLong.Text = strings.Repeat("x", maxCommandTextBytes+1)
	if _, err := HMACDigest(key, tooLong); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("oversized request error = %v, want ErrInvalidCanonicalRequest", err)
	}
	for _, mutate := range []func(*CanonicalRequest){
		func(request *CanonicalRequest) { request.RuleID = "" },
		func(request *CanonicalRequest) { request.EntityID = "switch.front_door" },
		func(request *CanonicalRequest) { request.ExpectedState = "unlocked" },
	} {
		invalid := req
		mutate(&invalid)
		if _, err := HMACDigest(key, invalid); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("unsafe canonical request error = %v, want ErrInvalidCanonicalRequest", err)
		}
	}

	// Length-prefixing must distinguish field-boundary changes.
	boundaryA, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: "pair-a",
		RequestID:      requestID,
		RuleID:         "boundary-rule",
		Locale:         "en",
		Text:           "ab:c",
		EntityID:       "light.boundary",
		ExpectedState:  "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	boundaryB, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: "pair-a",
		RequestID:      requestID,
		RuleID:         "boundary-rule",
		Locale:         "en-a",
		Text:           "b:c",
		EntityID:       "light.boundary",
		ExpectedState:  "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	if boundaryA == boundaryB {
		t.Fatal("length-delimited canonical fields collided")
	}
}

func TestValidateRequestID(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	valid := testUUIDV7(now.Add(-time.Minute), 2)
	issuedAt, err := ValidateRequestID(valid, now, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("ValidateRequestID(valid): %v", err)
	}
	if issuedAt.UnixMilli() != now.Add(-time.Minute).UnixMilli() {
		t.Fatalf("issuedAt = %s, want %s", issuedAt, now.Add(-time.Minute))
	}

	tests := []struct {
		name string
		id   string
		want error
	}{
		{name: "v4", id: uuid.NewString(), want: ErrInvalidRequestID},
		{name: "uppercase", id: strings.ToUpper(valid), want: ErrInvalidRequestID},
		{name: "stale", id: testUUIDV7(now.Add(-5*time.Minute-time.Millisecond), 3), want: ErrStaleRequestID},
		{name: "future", id: testUUIDV7(now.Add(30*time.Second+time.Millisecond), 4), want: ErrFutureRequestID},
		{name: "garbage", id: "not-a-uuid", want: ErrInvalidRequestID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ValidateRequestID(test.id, now, 5*time.Minute, 30*time.Second); !errors.Is(err, test.want) {
				t.Fatalf("ValidateRequestID error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResultDigestStable(t *testing.T) {
	t.Parallel()
	result := CompletedResult{
		Outcome:        OutcomeSuccess,
		ConversationID: "ha-conversation-digest-1",
		SpeechText:     "done",
		Language:       "en-US",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	first := digestCompletedResult(result)
	second := digestCompletedResult(result)
	if first != second {
		t.Fatal("result digest is not stable")
	}
	result.Retryable = true
	if first == digestCompletedResult(result) {
		t.Fatal("result digest did not bind retryability")
	}
	result.Retryable = false
	result.ConversationID = "ha-conversation-digest-2"
	if first == digestCompletedResult(result) {
		t.Fatal("result digest did not bind conversation id")
	}
}

func TestCompletedResultValidation(t *testing.T) {
	t.Parallel()
	valid := CompletedResult{
		Outcome:        OutcomeRejected,
		Language:       "zh-Hans",
		ResponseType:   "error",
		ErrorCode:      "ha.request_rejected",
		ReasonCode:     "ha.auth_failed",
		ActionExecuted: ActionExecutedNo,
	}
	if _, err := normalizeCompletedResult(valid); err != nil {
		t.Fatalf("valid rejected result: %v", err)
	}
	tests := []CompletedResult{
		{},
		{Outcome: OutcomeSuccess, ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeRejected, ErrorCode: "ha.error", ActionExecuted: ActionExecutedNo},
		{Outcome: OutcomeRejected, ErrorCode: "HA ERROR", ReasonCode: "ha.failed", ActionExecuted: ActionExecutedNo},
		{Outcome: OutcomeSuccess, ConversationID: strings.Repeat("x", maxConversationIDBytes+1), SpeechText: "ok", ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeSuccess, SpeechText: strings.Repeat("x", maxSpeechTextBytes+1), ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeSuccess, SpeechText: "ok", Language: "de_DE", ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeSuccess, SpeechText: "ok", ActionExecuted: ActionExecutedUnknown},
	}
	for index, result := range tests {
		if _, err := normalizeCompletedResult(result); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("invalid result %d error = %v, want ErrInvalidResult", index, err)
		}
	}
}
