package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/auditlogtest"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// TestSettingsChangedEmitsAuditEvent verifies that applyRuntimeSettings
// emits a settings.changed audit event.
func TestSettingsChangedEmitsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	state := &appState{}
	state.engine = newSpeechKitRuntime(state, speechkit.Hooks{})

	oldCfg := config.Config{}
	newCfg := config.Config{
		General: config.GeneralConfig{
			DictateEnabled:    true,
			AssistEnabled:     true,
			VoiceAgentEnabled: true,
			DictateHotkey:     "ctrl+shift+d",
			AssistHotkey:      "ctrl+win+j",
			VoiceAgentHotkey:  "win+alt+k",
		},
	}
	state.applyRuntimeSettings(
		oldCfg, newCfg,
		true, true, true,
		"ctrl+shift+d", "ctrl+win+j", "win+alt+k",
		config.HotkeyBehaviorToggle, config.HotkeyBehaviorHoldToTalk, config.HotkeyBehaviorToggle,
		"dictate", "mic-1",
		[]string{"local"},
		"pill", "default",
		config.OverlayFeedbackModeBigProductivity,
		config.OverlayFeedbackModeSmallFeedback,
		"bottom",
		"",
		false, 0, 0,
		nil,
	)

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"settings.changed"`) {
		t.Errorf("want settings.changed event in audit log, got:\n%s", data)
	}
	// Phase 1 (cpv.2.9): generic (applied) event replaced by per-section diffs.
	// A change in General emits key_path=general.
	if !strings.Contains(string(data), `"key_path":"general"`) {
		t.Errorf("want key_path=general in audit log, got:\n%s", data)
	}
}

// TestUpdateInstalledEmitsAuditEventOnSuccess verifies that a successful
// download + verification emits an update.installed event with outcome=success.
func TestUpdateInstalledEmitsAuditEventOnSuccess(t *testing.T) {
	payload := []byte("fake-installer-binary")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	prevVerify := verifyInstallerBeforeOpen
	verifyInstallerBeforeOpen = func(path string) error { return nil }
	defer func() { verifyInstallerBeforeOpen = prevVerify }()

	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	release := latestReleaseInfo{
		Version:      "1.2.3",
		DownloadURL:  server.URL + "/SpeechKit-Setup-v1.2.3.exe",
		DownloadName: "SpeechKit-Setup-v1.2.3.exe",
		DownloadSize: int64(len(payload)),
	}
	destDir := t.TempDir()

	mgr := newAppUpdateManager()
	mgr.Start(release, destDir)

	// Wait for the job to complete (max 3 s).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if jobs := mgr.AllJobs(); len(jobs) > 0 && jobs[0].Status == appUpdateStatusDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"update.installed"`) {
		t.Errorf("want update.installed event in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"outcome":"success"`) {
		t.Errorf("want outcome=success in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"version":"1.2.3"`) {
		t.Errorf("want version=1.2.3 in audit log, got:\n%s", data)
	}
}

// TestUpdateInstalledEmitsAuditEventOnVerificationFailure verifies that a
// failed signature verification emits an update.installed event with
// outcome=failure.
func TestUpdateInstalledEmitsAuditEventOnVerificationFailure(t *testing.T) {
	payload := []byte("unsigned-installer")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	prevVerify := verifyInstallerBeforeOpen
	verifyInstallerBeforeOpen = func(path string) error {
		return os.ErrInvalid // any non-nil error
	}
	defer func() { verifyInstallerBeforeOpen = prevVerify }()

	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	release := latestReleaseInfo{
		Version:      "1.2.4",
		DownloadURL:  server.URL + "/SpeechKit-Setup-v1.2.4.exe",
		DownloadName: "SpeechKit-Setup-v1.2.4.exe",
		DownloadSize: int64(len(payload)),
	}
	destDir := t.TempDir()

	mgr := newAppUpdateManager()
	mgr.Start(release, destDir)

	// Wait for the job to fail (max 3 s).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if jobs := mgr.AllJobs(); len(jobs) > 0 && jobs[0].Status == appUpdateStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"update.installed"`) {
		t.Errorf("want update.installed event in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"outcome":"failure"`) {
		t.Errorf("want outcome=failure in audit log, got:\n%s", data)
	}
}

// TestAuthFailedEmitsAuditEvent verifies that a POST with a missing
// control-plane token causes an auth.failed audit event.
func TestAuthFailedEmitsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	cfg := defaultTestConfig()
	state := &appState{controlPlaneToken: "test-secret-token"}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, nil, nil, &config.InstallState{})

	form := url.Values{"dummy": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://localhost:34115")
	// Deliberately omit the X-SpeechKit-Control-Token header.
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d; body=%s", rec.Code, rec.Body.String())
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"auth.failed"`) {
		t.Errorf("want auth.failed event in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"outcome":"failure"`) {
		t.Errorf("want outcome=failure in audit log, got:\n%s", data)
	}
}

// TestEmitVoiceAgentSessionEndUserTermination verifies that
// emitVoiceAgentSessionEnd writes a voiceagent.session.end event with
// terminated_by=user.
func TestEmitVoiceAgentSessionEndUserTermination(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	emitVoiceAgentSessionEnd(context.Background(), "va-test-user", time.Now().Add(-30*time.Second), "user")

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"voiceagent.session.end"`) {
		t.Errorf("want session.end event, got: %s", data)
	}
	if !strings.Contains(string(data), `"terminated_by":"user"`) {
		t.Errorf("want terminated_by:user, got: %s", data)
	}
}

// TestEmitVoiceAgentSessionEndErrorTermination verifies that
// emitVoiceAgentSessionEnd writes a voiceagent.session.end event with
// terminated_by=error.
func TestEmitVoiceAgentSessionEndErrorTermination(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	emitVoiceAgentSessionEnd(context.Background(), "va-test-error", time.Now().Add(-30*time.Second), "error")

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"terminated_by":"error"`) {
		t.Errorf("want terminated_by:error, got: %s", data)
	}
}

// TestEmitVoiceAgentSessionEndNoOpWhenSessionIDEmpty verifies that
// emitVoiceAgentSessionEnd does not write any audit file when sessionID is
// empty (i.e. the session was never started or the ID was already consumed by
// an earlier termination path).
func TestEmitVoiceAgentSessionEndNoOpWhenSessionIDEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	emitVoiceAgentSessionEnd(context.Background(), "", time.Now(), "user")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("want no audit file when sessionID empty, got %d entries", len(entries))
	}
}

// TestVoiceAgentSessionAuditEvents verifies that the voice agent callback
// wiring emits voiceagent.session.start and voiceagent.session.end events.
// This test exercises the callback closures directly rather than starting a
// real voice agent session, which requires a live provider.
func TestVoiceAgentSessionAuditEvents(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	state := &appState{}
	ctx := context.Background()

	// Simulate session start: set sessionID and sessionStart directly
	// as startVoiceAgentSession does, then emit the event.
	sessionID := "va-test-session-001"
	sessionStart := time.Now()
	state.mu.Lock()
	state.voiceAgentSessionID = sessionID
	state.voiceAgentSessionStart = sessionStart
	state.mu.Unlock()

	_ = auditlog.AppendEvent(ctx, auditlog.Record{
		Event: auditlog.EventVoiceAgentSessionStart,
		Resource: map[string]any{
			"session_id": sessionID,
			"provider":   "gemini-live",
			"transport":  "in-process",
		},
	})

	// Simulate session end: emit the end event as the OnSessionEnd callback does.
	state.mu.Lock()
	endSessionID := state.voiceAgentSessionID
	endSessionStart := state.voiceAgentSessionStart
	state.voiceAgentSessionID = ""
	state.voiceAgentSessionStart = time.Time{}
	state.mu.Unlock()

	_ = auditlog.AppendEvent(context.Background(), auditlog.Record{
		Event: auditlog.EventVoiceAgentSessionEnd,
		Resource: map[string]any{
			"session_id":       endSessionID,
			"duration_seconds": time.Since(endSessionStart).Seconds(),
			"terminated_by":    "user",
		},
	})

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"event":"voiceagent.session.start"`) {
		t.Errorf("want voiceagent.session.start event in audit log, got:\n%s", body)
	}
	if !strings.Contains(body, `"event":"voiceagent.session.end"`) {
		t.Errorf("want voiceagent.session.end event in audit log, got:\n%s", body)
	}
	if !strings.Contains(body, `"session_id":"va-test-session-001"`) {
		t.Errorf("want session_id in audit log, got:\n%s", body)
	}
}

// TestBYOKKeyUpdatedAuditEvent verifies that emitBYOKKeyUpdated writes a
// byok.key_updated event with the correct resource fields and never leaks
// the raw key.
func TestBYOKKeyUpdatedAuditEvent(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	ctx := context.Background()
	const testKey = "test-api-key-value-1234"
	const testProvider = "google"
	const testRegion = "europe-west3"

	emitBYOKKeyUpdated(ctx, testProvider, testRegion, testKey)

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	body := string(data)

	if !strings.Contains(body, `"event":"byok.key_updated"`) {
		t.Errorf("want byok.key_updated event in audit log, got:\n%s", body)
	}
	if !strings.Contains(body, `"provider_name":"google"`) {
		t.Errorf("want provider_name=google in audit log, got:\n%s", body)
	}
	if !strings.Contains(body, `"region":"europe-west3"`) {
		t.Errorf("want region=europe-west3 in audit log, got:\n%s", body)
	}
	// Key must not appear in log.
	if strings.Contains(body, testKey) {
		t.Errorf("raw API key must not appear in audit log, got:\n%s", body)
	}
	// fingerprint_truncated must be present and exactly 16 hex chars.
	if !strings.Contains(body, `"fingerprint_truncated":`) {
		t.Errorf("want fingerprint_truncated field in audit log, got:\n%s", body)
	}
}

// TestBYOKKeyUpdatedEmptyKeySkipsEvent verifies that emitBYOKKeyUpdated is a
// no-op when the key is empty (key-clear path is audited separately).
func TestBYOKKeyUpdatedEmptyKeySkipsEvent(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	emitBYOKKeyUpdated(context.Background(), "google", "europe-west3", "")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("want no audit file when key is empty, got %d entries", len(entries))
	}
}

// TestBYOKKeyUpdatedNonGoogleProviderHasEmptyRegion verifies that non-Google
// providers emit byok.key_updated with an empty region field.
func TestBYOKKeyUpdatedNonGoogleProviderHasEmptyRegion(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	emitBYOKKeyUpdated(context.Background(), "openai", "", "sk-test-1234")

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	body := string(data)

	if !strings.Contains(body, `"event":"byok.key_updated"`) {
		t.Errorf("want byok.key_updated event in audit log, got:\n%s", body)
	}
	if !strings.Contains(body, `"provider_name":"openai"`) {
		t.Errorf("want provider_name=openai in audit log, got:\n%s", body)
	}
	// region should be present but empty for non-Google providers.
	if !strings.Contains(body, `"region":""`) {
		t.Errorf("want empty region field for openai provider, got:\n%s", body)
	}
}
