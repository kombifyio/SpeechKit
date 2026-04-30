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
// Scenarios use programmatically generated audio fixtures (synth sine
// wave wrapped in a WAV header) so the tool has no external file
// dependencies. WebM/Opus and OGG/Opus paths are exercised by the
// server-side ffmpeg-test pair, not here.
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
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	var (
		server      = flag.String("server", "http://localhost:8080", "speechkit-server base URL")
		token       = flag.String("token", "", "bearer token (also reads $SPEECHKIT_SERVER_TOKEN)")
		scenarios   = flag.String("scenarios", "all", "comma-separated scenarios: health,dictation,assist,voiceagent,all")
		timeout     = flag.Duration("timeout", 30*time.Second, "per-request timeout")
		strictReady = flag.Bool("strict-ready", false, "fail when /readyz returns 503 (default: allow degraded for missing-key deployments)")
		verbose     = flag.Bool("v", false, "verbose: print every request body and response")
	)
	flag.Parse()

	bearer := strings.TrimSpace(*token)
	if bearer == "" {
		bearer = strings.TrimSpace(os.Getenv("SPEECHKIT_SERVER_TOKEN"))
	}
	c := &client{
		base:    strings.TrimRight(*server, "/"),
		token:   bearer,
		timeout: *timeout,
		verbose: *verbose,
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
		err := fn(c, &scenarioOpts{strictReady: *strictReady})
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
	strictReady bool
}

var allScenarios = map[string]scenarioFn{
	"health":     scenarioHealth,
	"dictation":  scenarioDictation,
	"assist":     scenarioAssist,
	"voiceagent": scenarioVoiceAgentCreate,
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
		return []string{"health", "dictation", "assist", "voiceagent"}
	}
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
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

	// /readyz can be 200 (everything ready) or 503 (some component
	// degraded). We tolerate degraded states unless --strict-ready is set,
	// because dev deployments commonly run without optional provider keys.
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
	if c.verbose {
		fmt.Printf("    /readyz status=%d body=%s\n", resp.StatusCode, string(body))
	}
	return nil
}

func scenarioDictation(c *client, opts *scenarioOpts) error {
	// 250ms 440 Hz sine — enough for the ingress path to validate but
	// short enough that any STT provider will return quickly. The actual
	// transcript content depends on the deployment's provider mix; we
	// only assert the response shape, not the words.
	pcm := synthSine(16000, 440.0, 250)
	wav := wrapWAV(pcm, 16000, 1)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Type", "audio/wav")
	hdr.Set("Content-Disposition", `form-data; name="audio"; filename="e2e.wav"`)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return err
	}
	if _, err := part.Write(wav); err != nil {
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
		if c.verbose {
			fmt.Printf("    dictation provider=%s text=%q latency_ms=%d\n", dr.Provider, dr.Text, dr.LatencyMs)
		}
		return nil
	case http.StatusServiceUnavailable:
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

func scenarioAssist(c *client, opts *scenarioOpts) error {
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
			if c.verbose {
				fmt.Printf("    assist %s action=%s text=%q latency_ms=%d\n", check.name, ar.Action, ar.Text, ar.LatencyMs)
			}
		case http.StatusServiceUnavailable:
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
	if err := c.verifyVoiceAgentWebSocket(ctx, sr.WSURL); err != nil {
		return fmt.Errorf("voiceagent websocket: %w", err)
	}
	if c.verbose {
		fmt.Printf("    voiceagent websocket connected and closed cleanly\n")
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

// ── HTTP client ─────────────────────────────────────────────────────────────

type client struct {
	base    string
	token   string
	timeout time.Duration
	verbose bool
}

func (c *client) verifyVoiceAgentWebSocket(ctx context.Context, rawURL string) error {
	wsURL := strings.TrimSpace(rawURL)
	if wsURL == "" {
		return errors.New("ws_url is empty")
	}
	opts := &websocket.DialOptions{}
	if c.token != "" {
		opts.HTTPHeader = http.Header{}
		opts.HTTPHeader.Set("Authorization", "Bearer "+c.token)
	}
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
