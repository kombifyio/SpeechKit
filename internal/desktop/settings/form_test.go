package settings

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFormValuesNormalizeCommonSettingsInputs(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/settings/update", nil)
	req.PostForm = url.Values{
		"enabled": {"1"},
		"count":   {"7"},
		"name":    {"  SpeechKit  "},
	}

	if !BoolValue(req, "enabled", false) {
		t.Fatal("BoolValue should parse 1 as true")
	}
	if got := IntValue(req, "count", 0); got != 7 {
		t.Fatalf("IntValue = %d, want 7", got)
	}
	if got := TrimmedValue(req, "name"); got != "SpeechKit" {
		t.Fatalf("TrimmedValue = %q, want SpeechKit", got)
	}
	if !Includes(req, "enabled") {
		t.Fatal("Includes should report posted key")
	}
}

func TestNonNegativeIntValueFallsBackForNegativeOrInvalidValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/settings/update", nil)
	req.PostForm = url.Values{
		"negative": {"-1"},
		"invalid":  {"abc"},
	}

	if got := NonNegativeIntValue(req, "negative", 5); got != 5 {
		t.Fatalf("negative value = %d, want fallback 5", got)
	}
	if got := NonNegativeIntValue(req, "invalid", 5); got != 5 {
		t.Fatalf("invalid value = %d, want fallback 5", got)
	}
}

func TestNormalizeMultilineTrimsAndNormalizesLineEndings(t *testing.T) {
	if got := NormalizeMultiline(" one\r\ntwo\r "); got != "one\ntwo" {
		t.Fatalf("NormalizeMultiline = %q", got)
	}
}
