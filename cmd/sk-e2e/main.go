// Command sk-e2e is a thin end-to-end smoke client for a running
// speechkit-server instance. It walks every public v1 endpoint with
// realistic payloads and exits non-zero when any contract assertion
// fails. Designed to be run from CI, from `scripts/test-e2e-local.ps1`,
// or by hand against a staging URL.
//
// Usage:
//
//	sk-e2e --server http://localhost:8080
//	sk-e2e --server $URL --token $TOKEN --scenarios health,dictation
//
// Exit codes:
//
//	0 — all scenarios passed
//	1 — at least one scenario failed
//	2 — invalid CLI usage / setup error
//
// By default scenarios use programmatically generated audio fixtures
// (synth sine wave wrapped in a WAV header) so the tool has no external
// file dependencies. When --require-functional is set, Dictation must be
// given a spoken --audio-fixture and mode checks require provider-backed
// non-empty outputs.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	var (
		server      = flag.String("server", "http://localhost:8080", "speechkit-server base URL")
		token       = flag.String("token", "", "bearer token (also reads $SPEECHKIT_SERVER_TOKEN)")
		scenarios   = flag.String("scenarios", "all", "comma-separated scenarios: health,deployment,dictation,assist,voiceagent,all")
		timeout     = flag.Duration("timeout", 30*time.Second, "per-request timeout")
		strictReady = flag.Bool("strict-ready", false, "fail when /readyz or /readyz/strict returns 503")
		functional  = flag.Bool("require-functional", false, "fail on provider-unavailable responses and require real mode outputs")
		audioFile   = flag.String("audio-fixture", "", "spoken audio fixture for functional dictation checks")
		expectText  = flag.String("expect-dictation-text", "", "case-insensitive text fragment expected in functional dictation output")
		vaText      = flag.String("voiceagent-text", "Say exactly: SpeechKit voice agent live check.", "text turn for functional Voice Agent checks")
		verbose     = flag.Bool("v", false, "verbose: print every request body and response")
		localOnly   = flag.Bool("local-only", false, "inject X-SpeechKit-Local-Only:1 on every request and assert /v1/deployment/status reports providers.cloud_keys_present=false")
	)
	flag.Parse()

	bearer := strings.TrimSpace(*token)
	if bearer == "" {
		bearer = strings.TrimSpace(os.Getenv("SPEECHKIT_SERVER_TOKEN"))
	}
	c := &client{
		base:      strings.TrimRight(*server, "/"),
		token:     bearer,
		timeout:   *timeout,
		verbose:   *verbose,
		localOnly: *localOnly,
	}

	wanted := selectedScenarios(*scenarios)
	results := make([]scenarioResult, 0, len(wanted))

	for _, name := range wanted {
		fn, ok := allScenarios[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "sk-e2e: unknown scenario %q\n", name)
			os.Exit(2)
		}
		fmt.Printf("=== RUN  %s\n", name)
		start := time.Now()
		err := fn(c, &scenarioOpts{
			strictReady:         *strictReady,
			requireFunctional:   *functional,
			audioFixturePath:    *audioFile,
			expectDictationText: *expectText,
			voiceAgentText:      *vaText,
		})
		dur := time.Since(start)
		if err != nil {
			fmt.Printf("--- FAIL %s (%s)\n   %v\n", name, dur, err)
			results = append(results, scenarioResult{name: name, ok: false, dur: dur, err: err})
			continue
		}
		fmt.Printf("--- PASS %s (%s)\n", name, dur)
		results = append(results, scenarioResult{name: name, ok: true, dur: dur})
	}

	failed := 0
	for _, r := range results {
		if !r.ok {
			failed++
		}
	}
	fmt.Println()
	if failed == 0 {
		fmt.Printf("OK  %d/%d scenarios\n", len(results), len(results))
		return
	}
	fmt.Printf("FAIL %d/%d scenarios failed\n", failed, len(results))
	os.Exit(1)
}

// ── scenarios ───────────────────────────────────────────────────────────────

type scenarioFn func(c *client, opts *scenarioOpts) error

type scenarioOpts struct {
	strictReady         bool
	requireFunctional   bool
	audioFixturePath    string
	expectDictationText string
	voiceAgentText      string
}

var allScenarios = map[string]scenarioFn{
	"health":     scenarioHealth,
	"deployment": scenarioDeploymentStatus,
	"dictation":  scenarioDictation,
	"assist":     scenarioAssist,
	"voiceagent": scenarioVoiceAgentCreate,
	"wakeword":   scenarioWakewordActivations,
}

type scenarioResult struct {
	name string
	ok   bool
	dur  time.Duration
	err  error
}

func selectedScenarios(s string) []string {
	if strings.TrimSpace(s) == "" || s == "all" {
		// Stable order: health first (cheapest), then mode-specific.
		return []string{"health", "deployment", "dictation", "assist", "voiceagent", "wakeword"}
	}
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func scenarioDeploymentStatus(c *client, opts *scenarioOpts) error {
	if strings.TrimSpace(c.token) == "" {
		return errors.New("deployment status requires --token or SPEECHKIT_SERVER_TOKEN")
	}
	resp, body, err := c.do(http.MethodGet, "/api/v1/deployment/status", "", nil, true)
	if err != nil {
		return fmt.Errorf("deployment status request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deployment status = %d; body=%s", resp.StatusCode, body)
	}
	if bytes.Contains(body, []byte(c.token)) {
		return fmt.Errorf("deployment status leaked bearer token; body=%s", body)
	}
	var status struct {
		Version string `json:"version"`
		Auth    struct {
			Mode           string `json:"mode"`
			BearerTokenEnv string `json:"bearer_token_env"`
			BearerTokenSet bool   `json:"bearer_token_set"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("deployment status parse: %w body=%s", err, body)
	}
	if status.Version == "" {
		return fmt.Errorf("deployment status missing version; body=%s", body)
	}
	if status.Auth.Mode == "" {
		return fmt.Errorf("deployment status missing auth.mode; body=%s", body)
	}
	if status.Auth.BearerTokenEnv == "" || !status.Auth.BearerTokenSet {
		return fmt.Errorf("deployment status bearer token not configured; body=%s", body)
	}
	if c.verbose {
		fmt.Printf("    deployment auth_mode=%s bearer_env=%s\n", status.Auth.Mode, status.Auth.BearerTokenEnv)
	}
	if c.localOnly {
		var providers struct {
			Providers struct {
				CloudKeysPresent bool `json:"cloud_keys_present"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(body, &providers); err != nil {
			return fmt.Errorf("deployment status parse providers: %w body=%s", err, body)
		}
		if providers.Providers.CloudKeysPresent {
			return fmt.Errorf("local-only assertion failed: deployment status reports providers.cloud_keys_present=true; body=%s", body)
		}
		if c.verbose {
			fmt.Println("    local-only: providers.cloud_keys_present=false")
		}
	}
	return nil
}

func scenarioHealth(c *client, opts *scenarioOpts) error {
	// /healthz must always return 200 quickly.
	resp, body, err := c.do(http.MethodGet, "/healthz", "", nil, false /* no auth */)
	if err != nil {
		return fmt.Errorf("/healthz: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}
	var hz struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &hz); err != nil {
		return fmt.Errorf("/healthz body parse: %w body=%s", err, body)
	}
	if hz.Status != "ok" {
		return fmt.Errorf("/healthz status = %q, want ok", hz.Status)
	}

	// /readyz is the orchestrator probe. It can still be 503 when a blocking
	// dependency for an enabled mode is unavailable; providerless dev stacks use
	// the response shape as the contract unless --strict-ready is set.
	resp, body, err = c.do(http.MethodGet, "/readyz", "", nil, false)
	if err != nil {
		return fmt.Errorf("/readyz: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return fmt.Errorf("/readyz status = %d, want 200 or 503", resp.StatusCode)
	}
	if opts.strictReady && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/readyz returned %d under --strict-ready; body=%s", resp.StatusCode, string(body))
	}
	if err := assertReadinessBody("/readyz", body, false); err != nil {
		return err
	}
	if c.verbose {
		fmt.Printf("    /readyz status=%d body=%s\n", resp.StatusCode, string(body))
	}

	resp, body, err = c.do(http.MethodGet, "/readyz/strict", "", nil, false)
	if err != nil {
		return fmt.Errorf("/readyz/strict: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return fmt.Errorf("/readyz/strict status = %d, want 200 or 503", resp.StatusCode)
	}
	if opts.strictReady && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/readyz/strict returned %d under --strict-ready; body=%s", resp.StatusCode, string(body))
	}
	if err := assertReadinessBody("/readyz/strict", body, true); err != nil {
		return err
	}
	if c.verbose {
		fmt.Printf("    /readyz/strict status=%d body=%s\n", resp.StatusCode, string(body))
	}
	return nil
}

func scenarioDictation(c *client, opts *scenarioOpts) error {
	var (
		audio       []byte
		contentType = "audio/wav"
		filename    = "e2e.wav"
		err         error
	)
	if strings.TrimSpace(opts.audioFixturePath) != "" {
		audio, err = os.ReadFile(opts.audioFixturePath)
		if err != nil {
			return fmt.Errorf("dictation audio fixture: %w", err)
		}
		filename = filepath.Base(opts.audioFixturePath)
		contentType = audioContentType(filename)
	} else {
		if opts.requireFunctional {
			return errors.New("functional dictation requires --audio-fixture with real spoken audio")
		}
		// 250ms 440 Hz sine is only an ingress smoke fixture. It is not a
		// functional transcription test.
		pcm := synthSine(16000, 440.0, 250)
		audio = wrapWAV(pcm, 16000, 1)
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Type", contentType)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="audio"; filename=%q`, filename))
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return err
	}
	if _, err := part.Write(audio); err != nil {
		return err
	}
	_ = mw.WriteField("language", "en")
	if err := mw.Close(); err != nil {
		return err
	}

	resp, respBody, err := c.do(http.MethodPost, "/api/v1/dictation/transcribe", mw.FormDataContentType(), body.Bytes(), true)
	if err != nil {
		return fmt.Errorf("dictation request: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		var dr struct {
			Text       string `json:"text"`
			DurationMs int64  `json:"duration_ms"`
			LatencyMs  int64  `json:"latency_ms"`
			Provider   string `json:"provider"`
		}
		if err := json.Unmarshal(respBody, &dr); err != nil {
			return fmt.Errorf("dictation response parse: %w body=%s", err, respBody)
		}
		if dr.DurationMs <= 0 {
			return fmt.Errorf("dictation: duration_ms must be > 0; got %d body=%s", dr.DurationMs, respBody)
		}
		if opts.requireFunctional {
			if strings.TrimSpace(dr.Text) == "" {
				return fmt.Errorf("dictation: functional check produced empty transcript; body=%s", respBody)
			}
			if expected := strings.TrimSpace(opts.expectDictationText); expected != "" &&
				!strings.Contains(strings.ToLower(dr.Text), strings.ToLower(expected)) {
				return fmt.Errorf("dictation: transcript %q does not contain expected text fragment %q", dr.Text, expected)
			}
		}
		if c.verbose {
			fmt.Printf("    dictation provider=%s text=%q latency_ms=%d\n", dr.Provider, dr.Text, dr.LatencyMs)
		}
		return nil
	case http.StatusServiceUnavailable:
		if opts.requireFunctional {
			return fmt.Errorf("dictation unavailable during functional check; body=%s", respBody)
		}
		// Acceptable when the deployment has no STT provider keys; we
		// still want the contract envelope.
		if !hasErrorEnvelope(respBody) {
			return fmt.Errorf("dictation 503 missing error envelope; body=%s", respBody)
		}
		fmt.Printf("    dictation: server has no STT provider configured (503 with envelope) — treating as smoke-pass\n")
		return nil
	default:
		return fmt.Errorf("dictation status = %d; body=%s", resp.StatusCode, respBody)
	}
}

func audioContentType(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".wav":
		return "audio/wav"
	case ".webm":
		return "audio/webm"
	case ".ogg":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}

func scenarioAssist(c *client, opts *scenarioOpts) error {
	if err := checkAssistSelfTest(c, opts); err != nil {
		return err
	}

	checks := []struct {
		name         string
		payload      map[string]any
		wantAction   string
		allowDegrade bool
	}{
		{
			name: "direct",
			payload: map[string]any{
				"text":   "what time is it",
				"locale": "en",
				"tts":    false, // skip TTS — we don't want to depend on the deployment having TTS keys
			},
			allowDegrade: true,
		},
		{
			name:       "copy_last",
			wantAction: "execute",
			payload: map[string]any{
				"text":   "copy last",
				"locale": "en",
				"tts":    false,
			},
		},
		{
			name:       "insert_last",
			wantAction: "execute",
			payload: map[string]any{
				"text":   "insert last",
				"locale": "en",
				"tts":    false,
			},
		},
		{
			name:       "summarize",
			wantAction: "execute",
			payload: map[string]any{
				"text":      "summarize this",
				"locale":    "en",
				"selection": "Deploy smoke source text. It exercises the Assist summarize codeword after release.",
				"tts":       false,
			},
		},
	}

	for _, check := range checks {
		bodyBytes, _ := json.Marshal(check.payload)
		resp, respBody, err := c.do(http.MethodPost, "/api/v1/assist/process", "application/json", bodyBytes, true)
		if err != nil {
			return fmt.Errorf("assist %s request: %w", check.name, err)
		}
		switch resp.StatusCode {
		case http.StatusOK:
			var ar struct {
				Text      string `json:"text"`
				Action    string `json:"action"`
				LatencyMs int64  `json:"latency_ms"`
			}
			if err := json.Unmarshal(respBody, &ar); err != nil {
				return fmt.Errorf("assist %s response parse: %w body=%s", check.name, err, respBody)
			}
			if ar.Action == "" {
				return fmt.Errorf("assist %s: empty action field; body=%s", check.name, respBody)
			}
			if check.wantAction != "" && ar.Action != check.wantAction {
				return fmt.Errorf("assist %s: action = %q, want %q; body=%s", check.name, ar.Action, check.wantAction, respBody)
			}
			if opts.requireFunctional && strings.TrimSpace(ar.Text) == "" {
				return fmt.Errorf("assist %s: functional check produced empty text; body=%s", check.name, respBody)
			}
			if c.verbose {
				fmt.Printf("    assist %s action=%s text=%q latency_ms=%d\n", check.name, ar.Action, ar.Text, ar.LatencyMs)
			}
		case http.StatusServiceUnavailable:
			if opts.requireFunctional {
				return fmt.Errorf("assist %s unavailable during functional check; body=%s", check.name, respBody)
			}
			if !check.allowDegrade {
				return fmt.Errorf("assist %s unexpectedly unavailable; body=%s", check.name, respBody)
			}
			if !hasErrorEnvelope(respBody) {
				return fmt.Errorf("assist %s 503 missing error envelope; body=%s", check.name, respBody)
			}
			fmt.Printf("    assist %s: deployment lacks required provider (503 with envelope) — treating as smoke-pass\n", check.name)
		default:
			return fmt.Errorf("assist %s status = %d; body=%s", check.name, resp.StatusCode, respBody)
		}
	}
	return nil
}

func checkAssistSelfTest(c *client, opts *scenarioOpts) error {
	resp, respBody, err := c.do(http.MethodPost, "/api/v1/assist/self-test", "application/json", nil, true)
	if err != nil {
		return fmt.Errorf("assist self-test request: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		var sr struct {
			Status    string `json:"status"`
			Text      string `json:"text"`
			LatencyMs int64  `json:"latency_ms"`
		}
		if err := json.Unmarshal(respBody, &sr); err != nil {
			return fmt.Errorf("assist self-test response parse: %w body=%s", err, respBody)
		}
		if sr.Status != "ok" || strings.TrimSpace(sr.Text) == "" {
			return fmt.Errorf("assist self-test invalid success body=%s", respBody)
		}
		if c.verbose {
			fmt.Printf("    assist self-test text=%q latency_ms=%d\n", sr.Text, sr.LatencyMs)
		}
		return nil
	case http.StatusServiceUnavailable:
		if opts.requireFunctional {
			return fmt.Errorf("assist self-test unavailable during functional check; body=%s", respBody)
		}
		if !hasErrorEnvelope(respBody) {
			return fmt.Errorf("assist self-test 503 missing error envelope; body=%s", respBody)
		}
		details, _ := errorEnvelopeDetails(respBody)
		if len(details) == 0 {
			return fmt.Errorf("assist self-test 503 missing structured details; body=%s", respBody)
		}
		fmt.Printf("    assist self-test: unavailable with structured diagnostics — treating as smoke-pass\n")
		return nil
	default:
		return fmt.Errorf("assist self-test status = %d; body=%s", resp.StatusCode, respBody)
	}
}

func scenarioVoiceAgentCreate(c *client, opts *scenarioOpts) error {
	// Validate the session ticket envelope and exercise the public WebSocket
	// upgrade path without sending audio frames.
	resp, respBody, err := c.do(http.MethodPost, "/api/v1/voiceagent/sessions", "application/json", []byte(`{}`), true)
	if err != nil {
		return fmt.Errorf("voiceagent create: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("voiceagent create status = %d; body=%s", resp.StatusCode, respBody)
	}
	var sr struct {
		SessionID string `json:"session_id"`
		WSURL     string `json:"ws_url"`
		Ticket    string `json:"ticket"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(respBody, &sr); err != nil {
		return fmt.Errorf("voiceagent create body parse: %w body=%s", err, respBody)
	}
	if sr.SessionID == "" || sr.Ticket == "" || sr.WSURL == "" {
		return fmt.Errorf("voiceagent create: missing required fields; body=%s", respBody)
	}
	if !strings.Contains(sr.WSURL, "ticket=") {
		return fmt.Errorf("voiceagent create: ws_url missing ticket query param; got %q", sr.WSURL)
	}
	if c.verbose {
		fmt.Printf("    voiceagent session_id=%s expires_at=%s\n", sr.SessionID, sr.ExpiresAt)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	if opts.requireFunctional {
		if err := c.verifyVoiceAgentFunctional(ctx, sr.WSURL, opts.voiceAgentText); err != nil {
			return fmt.Errorf("voiceagent functional websocket: %w", err)
		}
	} else if err := c.verifyVoiceAgentWebSocket(ctx, sr.WSURL); err != nil {
		return fmt.Errorf("voiceagent websocket: %w", err)
	}
	if c.verbose {
		fmt.Printf("    voiceagent websocket verified\n")
	}

	// Clean up the session — leaving it dangling would slowly use up the
	// server's per-identity quota across repeated runs. A successful WS
	// close may already remove the session server-side, so 404 is also OK.
	delResp, delBody, delErr := c.do(http.MethodDelete, "/api/v1/voiceagent/sessions/"+sr.SessionID, "", nil, true)
	if delErr != nil {
		return fmt.Errorf("voiceagent delete: %w", delErr)
	}
	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("voiceagent delete status = %d; body=%s", delResp.StatusCode, delBody)
	}
	return nil
}

// scenarioWakewordActivations exercises the v0.37.5 wake-word
// training-data REST endpoints against the deployed server. Posts a
// synthetic activation clip, verifies the metadata round-trips on
// GET, applies a label via PATCH, then DELETEs it so subsequent runs
// stay idempotent.
//
// When the server has `[server.training_data].accept_uploads=false`
// (the default), every endpoint returns 503 with a
// `training_data_disabled` envelope. We treat that as a smoke-pass
// rather than a failure — the contract is "503 with a structured
// payload" and a server that opts out should still respond cleanly.
func scenarioWakewordActivations(c *client, opts *scenarioOpts) error {
	// Probe first: a single GET tells us whether uploads are accepted
	// on this deployment without provisioning a clip we'd then have
	// to garbage-collect.
	probeResp, probeBody, err := c.do(http.MethodGet, "/api/v1/wakeword/activations", "", nil, true)
	if err != nil {
		return fmt.Errorf("wakeword probe: %w", err)
	}
	if probeResp.StatusCode == http.StatusServiceUnavailable {
		if opts.requireFunctional {
			return fmt.Errorf("wakeword activations unavailable during functional check; body=%s", probeBody)
		}
		if !hasErrorEnvelope(probeBody) {
			return fmt.Errorf("wakeword 503 missing error envelope; body=%s", probeBody)
		}
		fmt.Printf("    wakeword: deployment has [server.training_data].accept_uploads=false (503 with envelope) — treating as smoke-pass\n")
		return nil
	}
	if probeResp.StatusCode != http.StatusOK {
		return fmt.Errorf("wakeword probe status = %d; body=%s", probeResp.StatusCode, probeBody)
	}

	// Build a tiny synthetic WAV + metadata pair.
	pcm := synthSine(16000, 440.0, 250)
	wav := wrapWAV(pcm, 16000, 1)
	id := fmt.Sprintf("sk-e2e-%d", time.Now().UnixNano())
	meta := map[string]any{
		"id":           id,
		"phrase_id":    "hey_quby",
		"phrase":       "Hey Quby",
		"backend":      "sk-e2e",
		"score":        0.99,
		"captured_at":  time.Now().UTC().Format(time.RFC3339),
		"pre_roll_ms":  1500,
		"post_roll_ms": 500,
		"sample_rate":  16000,
	}
	metaJSON, _ := json.Marshal(meta)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("metadata", string(metaJSON))
	part, err := mw.CreateFormFile("audio", "sk-e2e.wav")
	if err != nil {
		return fmt.Errorf("create audio part: %w", err)
	}
	if _, err := part.Write(wav); err != nil {
		return fmt.Errorf("write audio part: %w", err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("multipart close: %w", err)
	}

	createResp, createBody, err := c.do(http.MethodPost, "/api/v1/wakeword/activations", mw.FormDataContentType(), body.Bytes(), true)
	if err != nil {
		return fmt.Errorf("wakeword upload: %w", err)
	}
	if createResp.StatusCode != http.StatusCreated {
		return fmt.Errorf("wakeword upload status = %d; body=%s", createResp.StatusCode, createBody)
	}

	// GET the metadata back and assert the round-trip.
	getResp, getBody, err := c.do(http.MethodGet, "/api/v1/wakeword/activations/"+id, "", nil, true)
	if err != nil {
		return fmt.Errorf("wakeword get: %w", err)
	}
	if getResp.StatusCode != http.StatusOK {
		return fmt.Errorf("wakeword get status = %d; body=%s", getResp.StatusCode, getBody)
	}
	var got struct {
		ID       string `json:"id"`
		PhraseID string `json:"phrase_id"`
		Label    string `json:"label"`
	}
	if err := json.Unmarshal(getBody, &got); err != nil {
		return fmt.Errorf("wakeword get parse: %w body=%s", err, getBody)
	}
	if got.ID != id || got.PhraseID != "hey_quby" {
		return fmt.Errorf("wakeword get round-trip mismatch: %+v", got)
	}

	// Apply a label via PATCH.
	patchBody, _ := json.Marshal(map[string]string{"label": "correct"})
	patchResp, patchRespBody, err := c.do(http.MethodPatch, "/api/v1/wakeword/activations/"+id, "application/json", patchBody, true)
	if err != nil {
		return fmt.Errorf("wakeword patch: %w", err)
	}
	if patchResp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("wakeword patch status = %d; body=%s", patchResp.StatusCode, patchRespBody)
	}

	// Clean up so re-runs stay idempotent.
	delResp, delBody, err := c.do(http.MethodDelete, "/api/v1/wakeword/activations/"+id, "", nil, true)
	if err != nil {
		return fmt.Errorf("wakeword delete: %w", err)
	}
	if delResp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("wakeword delete status = %d; body=%s", delResp.StatusCode, delBody)
	}
	if c.verbose {
		fmt.Printf("    wakeword: round-trip id=%s succeeded\n", id)
	}
	return nil
}

// ── HTTP client ─────────────────────────────────────────────────────────────

type client struct {
	base      string
	token     string
	timeout   time.Duration
	verbose   bool
	localOnly bool
}

func (c *client) wsDialOptions() *websocket.DialOptions {
	opts := &websocket.DialOptions{HTTPHeader: http.Header{}}
	if c.token != "" {
		opts.HTTPHeader.Set("Authorization", "Bearer "+c.token)
	}
	opts.HTTPHeader.Set("Origin", strings.TrimRight(c.base, "/"))
	return opts
}

func (c *client) verifyVoiceAgentWebSocket(ctx context.Context, rawURL string) error {
	wsURL := strings.TrimSpace(rawURL)
	if wsURL == "" {
		return errors.New("ws_url is empty")
	}
	opts := c.wsDialOptions()
	conn, resp, err := websocket.Dial(ctx, wsURL, opts)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()
	return conn.Close(websocket.StatusNormalClosure, "speechkit e2e smoke complete")
}

func (c *client) verifyVoiceAgentFunctional(ctx context.Context, rawURL, textTurn string) error {
	wsURL := strings.TrimSpace(rawURL)
	if wsURL == "" {
		return errors.New("ws_url is empty")
	}
	opts := c.wsDialOptions()
	conn, resp, err := websocket.Dial(ctx, wsURL, opts)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	start := map[string]any{
		"type":            "start",
		"media_transport": "websocket",
		"locale":          "en",
		"system_prompt_override": strings.Join([]string{
			"You are running a SpeechKit release verification.",
			"Reply briefly to the user's text turn.",
			"Include the words 'SpeechKit live check' in your answer.",
		}, " "),
	}
	if err := writeWSJSON(ctx, conn, start); err != nil {
		return fmt.Errorf("send start: %w", err)
	}

	turn := strings.TrimSpace(textTurn)
	if turn == "" {
		turn = "Say exactly: SpeechKit live check."
	}
	textSent := false
	var outputTranscript strings.Builder
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read frame before output transcript: %w", err)
		}
		if typ == websocket.MessageBinary {
			continue
		}
		if typ != websocket.MessageText {
			continue
		}
		var frame struct {
			Type    string `json:"type"`
			State   string `json:"state"`
			Text    string `json:"text"`
			Done    bool   `json:"done"`
			Code    string `json:"code"`
			Message string `json:"message"`
			Reason  string `json:"reason"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			return fmt.Errorf("decode websocket frame %s: %w", data, err)
		}
		switch frame.Type {
		case "error":
			return fmt.Errorf("%s: %s", firstNonEmpty(frame.Code, "voiceagent_error"), frame.Message)
		case "state":
			if frame.State == "listening" && !textSent {
				if err := writeWSJSON(ctx, conn, map[string]any{"type": "text", "text": turn}); err != nil {
					return fmt.Errorf("send text turn: %w", err)
				}
				textSent = true
			}
		case "output_transcript":
			if !textSent {
				return errors.New("received output_transcript before sending text turn")
			}
			outputTranscript.WriteString(frame.Text)
			if strings.TrimSpace(outputTranscript.String()) != "" {
				_ = writeWSJSON(ctx, conn, map[string]any{"type": "stop"})
				return nil
			}
		case "session_end":
			return fmt.Errorf("session ended before output transcript: %s", frame.Reason)
		}
	}
}

func writeWSJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, body)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// doResp captures the small amount of HTTP response metadata the
// scenarios actually need. Returning a struct (instead of the full
// *http.Response) lets us close the body inside do() without leaving
// dangling cleanup at every call site — which also keeps golangci's
// bodyclose checker happy.
type doResp struct {
	StatusCode int
	Header     http.Header
}

func (c *client) do(method, path, contentType string, body []byte, auth bool) (*doResp, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if auth && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.localOnly {
		req.Header.Set("X-SpeechKit-Local-Only", "1")
	}
	if c.verbose && body != nil && contentType == "application/json" {
		fmt.Printf("    → %s %s\n      %s\n", method, path, string(body))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &doResp{StatusCode: resp.StatusCode, Header: resp.Header}, nil, err
	}
	return &doResp{StatusCode: resp.StatusCode, Header: resp.Header}, respBody, nil
}

func hasErrorEnvelope(body []byte) bool {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	return env.Error.Code != ""
}

func errorEnvelopeDetails(body []byte) (map[string]any, bool) {
	var env struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil || len(env.Error.Details) == 0 {
		return nil, false
	}
	return env.Error.Details, true
}

func assertReadinessBody(endpoint string, body []byte, strictEndpoint bool) error {
	var rz struct {
		Status       string `json:"status"`
		StrictStatus string `json:"strict_status"`
		Components   map[string]struct {
			Status   string `json:"status"`
			Blocking bool   `json:"blocking"`
		} `json:"components"`
	}
	if err := json.Unmarshal(body, &rz); err != nil {
		return fmt.Errorf("%s body parse: %w body=%s", endpoint, err, body)
	}
	if rz.Status == "" {
		return fmt.Errorf("%s missing status; body=%s", endpoint, body)
	}
	if rz.StrictStatus == "" {
		return fmt.Errorf("%s missing strict_status; body=%s", endpoint, body)
	}
	if rz.Components == nil {
		return fmt.Errorf("%s missing components map; body=%s", endpoint, body)
	}
	if strictEndpoint && rz.Status != rz.StrictStatus {
		return fmt.Errorf("%s status=%q strict_status=%q, want identical", endpoint, rz.Status, rz.StrictStatus)
	}
	return nil
}

// ── audio fixtures ──────────────────────────────────────────────────────────

func synthSine(rate int, hz float64, ms int) []byte {
	frames := rate * ms / 1000
	out := make([]byte, frames*2)
	for f := 0; f < frames; f++ {
		t := float64(f) / float64(rate)
		v := int16(math.Sin(2*math.Pi*hz*t) * 10000)
		// Reinterpret the signed sample as the equivalent unsigned bit
		// pattern. Same width, no information lost — gosec's G115 is
		// over-eager here.
		binary.LittleEndian.PutUint16(out[f*2:], uint16(v)) // #nosec G115 -- signed-to-unsigned reinterpret of identical-width int16 sample.
	}
	return out
}

func wrapWAV(pcm []byte, rate, channels int) []byte {
	// pcm slices are bounded by a few seconds of test audio; the
	// individual fields are header values that fit in their target
	// widths by definition (channels=1, rate=16000). gosec G115 is
	// flagging the implicit widening conversion which can't overflow
	// in practice — silence the noise with explicit nolint comments.
	dataSize := uint32(len(pcm)) // #nosec G115 -- bounded by the test fixture size.
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], 36+dataSize)
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], uint16(channels))        // #nosec G115 -- header value, channels=1 in this harness.
	binary.LittleEndian.PutUint32(buf[24:], uint32(rate))            // #nosec G115 -- header value, rate=16000 in this harness.
	binary.LittleEndian.PutUint32(buf[28:], uint32(rate*channels*2)) // #nosec G115 -- computed from constants.
	binary.LittleEndian.PutUint16(buf[32:], uint16(channels*2))      // #nosec G115 -- computed from constants.
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], dataSize)
	copy(buf[44:], pcm)
	return buf
}

// silence unused-import warnings when we drop scenarios; errors needed for
// linter tolerance.
var _ = errors.New
