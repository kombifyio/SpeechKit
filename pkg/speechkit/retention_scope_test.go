package speechkit

import (
	"errors"
	"testing"
)

func TestParseRetentionScopeAcceptsKnownValuesAndRejectsTypos(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want RetentionScope
	}{
		{"", RetentionScopeRetain},
		{"retain", RetentionScopeRetain},
		{"  Retain  ", RetentionScopeRetain},
		{"ephemeral", RetentionScopeEphemeral},
		{"EPHEMERAL", RetentionScopeEphemeral},
	} {
		got, err := ParseRetentionScope(tc.raw)
		if err != nil {
			t.Fatalf("ParseRetentionScope(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("ParseRetentionScope(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
	if _, err := ParseRetentionScope("zero"); !errors.Is(err, ErrUnknownRetentionScope) {
		t.Fatalf("ParseRetentionScope(\"zero\") error = %v, want ErrUnknownRetentionScope", err)
	}
}

// A value that slipped past config validation must not quietly start keeping
// recordings, so normalization collapses to the strictest scope.
func TestNormalizeRetentionScopeFailsClosed(t *testing.T) {
	if got := NormalizeRetentionScope("nonsense"); got != RetentionScopeEphemeral {
		t.Fatalf("NormalizeRetentionScope(\"nonsense\") = %q, want %q", got, RetentionScopeEphemeral)
	}
	if got := NormalizeRetentionScope(""); got != RetentionScopeRetain {
		t.Fatalf("an unset scope must stay backwards compatible, got %q", got)
	}
}

// Under the ephemeral scope the recording goes and the result stays. Settings
// are not a record of anything that was said, so they stay too.
func TestDurableKeepsTheResultAndDropsTheRecording(t *testing.T) {
	for _, class := range []ArtifactClass{ArtifactClassRecording, ArtifactClassScreenCapture, ArtifactClassNote} {
		durable, reason := RetentionScopeEphemeral.Durable(class)
		if durable {
			t.Errorf("%s is durable under the ephemeral scope", class)
		}
		if reason != RetentionScopeReasonEphemeral {
			t.Errorf("%s reason = %q, want %q", class, reason, RetentionScopeReasonEphemeral)
		}
	}
	for _, class := range []ArtifactClass{ArtifactClassReview, ArtifactClassSettings} {
		if durable, reason := RetentionScopeEphemeral.Durable(class); !durable || reason != "" {
			t.Errorf("%s should survive the ephemeral scope, got durable=%v reason=%q", class, durable, reason)
		}
	}
	for _, class := range []ArtifactClass{ArtifactClassRecording, ArtifactClassScreenCapture, ArtifactClassNote, ArtifactClassReview, ArtifactClassSettings} {
		if durable, _ := RetentionScopeRetain.Durable(class); !durable {
			t.Errorf("%s must stay durable under the retain scope", class)
		}
	}
}

// An unknown scope value reaching the predicate behaves like the strictest one.
func TestDurableFailsClosedForAnUnknownScope(t *testing.T) {
	if durable, _ := RetentionScope("made-up").Durable(ArtifactClassRecording); durable {
		t.Fatal("an unknown retention scope kept a recording durable")
	}
}

// Keeping nothing on the device while the vendor keeps the audio would make
// the setting a false promise, so a provider that cannot assert no-store is
// refused rather than trusted.
func TestAllowsProviderRetentionBlocksWhatCannotAssertNoStore(t *testing.T) {
	if allowed, _ := RetentionScopeEphemeral.AllowsProviderRetention(ProviderRetentionNoStore); !allowed {
		t.Error("a provider that asserts no-store must be allowed under the ephemeral scope")
	}
	allowed, reason := RetentionScopeEphemeral.AllowsProviderRetention(ProviderRetentionUnknown)
	if allowed {
		t.Error("a provider that cannot assert no-store must be blocked under the ephemeral scope")
	}
	if reason != RetentionScopeReasonProviderRetains {
		t.Errorf("reason = %q, want %q", reason, RetentionScopeReasonProviderRetains)
	}
	for _, retention := range []ProviderRetention{ProviderRetentionNoStore, ProviderRetentionUnknown} {
		if allowed, _ := RetentionScopeRetain.AllowsProviderRetention(retention); !allowed {
			t.Errorf("the retain scope must not block %q", retention)
		}
	}
}

// The two axes are orthogonal: neither answers the other's question.
func TestRetentionScopeIsIndependentOfNetworkScope(t *testing.T) {
	if durable, _ := RetentionScopeRetain.Durable(ArtifactClassRecording); !durable {
		t.Fatal("retain must keep recordings regardless of any network scope")
	}
	if !NetworkScopeDeviceOnly.Restricted() {
		t.Fatal("device_only should still be a restricted network scope")
	}
	if RetentionScopeRetain.Restricted() {
		t.Fatal("retain must not report itself as a restricted retention scope")
	}
}
