package toolbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// fixture is the shared speechkit.toolbridge.v1 wire fixture. The same JSON
// file is copy-tested in kombify-Agents (workbench-orchestrator
// test/__fixtures__/speechkit-toolbridge.v1.json); any drift between the two
// repos fails one side's contract gate.
type fixture struct {
	Version            string          `json:"version"`
	Manifest           json.RawMessage `json:"manifest"`
	CallRequest        json.RawMessage `json:"call_request"`
	CallResponseOK     json.RawMessage `json:"call_response_ok"`
	CallResponseDenied json.RawMessage `json:"call_response_denied"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "speechkit-toolbridge.v1.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if f.Version != WireVersion {
		t.Fatalf("fixture version = %q, want %q", f.Version, WireVersion)
	}
	return f
}

func fixtureSession(credential string) Session {
	return Session{
		ID:         "vs_fixture_001",
		Locale:     "en-US",
		PersonaID:  "default",
		Credential: credential,
	}
}

func decodeJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func newBridge(t *testing.T, server *httptest.Server, mutate func(*Options)) *Bridge {
	t.Helper()
	opts := Options{
		ManifestURL: server.URL + "/manifest",
		InvokeURL:   server.URL + "/call",
		HTTPClient:  server.Client(),
	}
	if mutate != nil {
		mutate(&opts)
	}
	bridge, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return bridge
}

// TestCallRequestMatchesFixture is the contract drift gate for the request
// side: the exact JSON body the bridge POSTs to /call must equal the
// fixture's call_request document.
func TestCallRequestMatchesFixture(t *testing.T) {
	f := loadFixture(t)
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/call" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-credential" {
			t.Errorf("Authorization = %q, want session credential bearer", got)
		}
		body, err := readAll(r)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(f.CallResponseOK)
	}))
	defer server.Close()

	bridge := newBridge(t, server, nil)
	_, ok := bridge.Execute(context.Background(), fixtureSession("fixture-credential"),
		"kombify_knowledge_search", map[string]any{"query": "What is kombify TechStack?"})
	if !ok {
		t.Fatal("Execute ok = false, want true")
	}
	got := decodeJSON(t, captured)
	want := decodeJSON(t, f.CallRequest)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("call request drifted from fixture.\n got: %#v\nwant: %#v", got, want)
	}
}

// TestCallResponseOKMatchesFixture asserts the success envelope parses and
// maps to the model-facing {result} payload.
func TestCallResponseOKMatchesFixture(t *testing.T) {
	f := loadFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(f.CallResponseOK)
	}))
	defer server.Close()

	bridge := newBridge(t, server, nil)
	payload, ok := bridge.Execute(context.Background(), fixtureSession("cred"), "kombify_knowledge_search", map[string]any{"query": "q"})
	if !ok {
		t.Fatal("Execute ok = false, want true")
	}
	wire := decodeJSON(t, f.CallResponseOK)
	if payload["result"] != wire["result_text"] {
		t.Fatalf("result = %v, want fixture result_text %v", payload["result"], wire["result_text"])
	}
	if _, hasError := payload["error"]; hasError {
		t.Fatalf("success payload must not carry error: %#v", payload)
	}
}

// TestCallResponseDeniedMatchesFixture is the drift gate for the denial
// envelope: error_code + user_guidance must survive into the structured
// error tool response so the model verbalizes the denial.
func TestCallResponseDeniedMatchesFixture(t *testing.T) {
	f := loadFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(f.CallResponseDenied)
	}))
	defer server.Close()

	bridge := newBridge(t, server, nil)
	payload, ok := bridge.Execute(context.Background(), fixtureSession("cred"), "kombify_knowledge_search", map[string]any{"query": "q"})
	if !ok {
		t.Fatal("Execute ok = false, want structured denial payload")
	}
	wire := decodeJSON(t, f.CallResponseDenied)
	errObj, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("payload.error missing: %#v", payload)
	}
	if errObj["code"] != wire["error_code"] {
		t.Fatalf("error.code = %v, want fixture error_code %v", errObj["code"], wire["error_code"])
	}
	if errObj["message"] != wire["message"] {
		t.Fatalf("error.message = %v, want fixture message %v", errObj["message"], wire["message"])
	}
	if !reflect.DeepEqual(payload["user_guidance"], wire["user_guidance"]) {
		t.Fatalf("user_guidance drifted.\n got: %#v\nwant: %#v", payload["user_guidance"], wire["user_guidance"])
	}
}

// TestManifestMatchesFixture asserts the manifest envelope decodes with the
// fixture's tool names, JSON-schema parameters, and timeout hints intact.
func TestManifestMatchesFixture(t *testing.T) {
	f := loadFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manifest" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-credential" {
			t.Errorf("Authorization = %q, want session credential bearer", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(f.Manifest)
	}))
	defer server.Close()

	bridge := newBridge(t, server, nil)
	tools, err := bridge.Definitions(context.Background(), fixtureSession("fixture-credential"))
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}
	var wire struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(f.Manifest, &wire); err != nil {
		t.Fatalf("decode fixture manifest: %v", err)
	}
	if len(tools) != len(wire.Tools) {
		t.Fatalf("tool count = %d, want %d", len(tools), len(wire.Tools))
	}
	for i, tool := range tools {
		if tool.Name != wire.Tools[i]["name"] {
			t.Fatalf("tool[%d].Name = %q, want %q", i, tool.Name, wire.Tools[i]["name"])
		}
		wantParams := wire.Tools[i]["parameters"]
		gotParams := roundTrip(t, tool.Parameters)
		if !reflect.DeepEqual(gotParams, wantParams) {
			t.Fatalf("tool[%d] parameters drifted.\n got: %#v\nwant: %#v", i, gotParams, wantParams)
		}
		if float64(tool.TimeoutMs) != wire.Tools[i]["timeout_ms"] {
			t.Fatalf("tool[%d].TimeoutMs = %d, want %v", i, tool.TimeoutMs, wire.Tools[i]["timeout_ms"])
		}
	}
}

func TestDefinitionsWithoutCredentialFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("bridge must not be called without a credential")
	}))
	defer server.Close()

	bridge := newBridge(t, server, nil)
	tools, err := bridge.Definitions(context.Background(), Session{ID: "s1"})
	if err != nil || tools != nil {
		t.Fatalf("Definitions = (%v, %v), want (nil, nil)", tools, err)
	}
}

func TestDefinitionsRejectsUnknownVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"speechkit.toolbridge.v2","tools":[{"name":"x"}]}`))
	}))
	defer server.Close()

	bridge := newBridge(t, server, nil)
	tools, err := bridge.Definitions(context.Background(), fixtureSession("cred"))
	if err == nil || tools != nil {
		t.Fatalf("Definitions = (%v, %v), want version error and no tools", tools, err)
	}
}

func TestManifestCachedPerCredential(t *testing.T) {
	f := loadFixture(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(f.Manifest)
	}))
	defer server.Close()

	now := time.Now()
	bridge := newBridge(t, server, func(o *Options) {
		o.Clock = func() time.Time { return now }
	})
	ctx := context.Background()
	if _, err := bridge.Definitions(ctx, fixtureSession("cred-a")); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if _, err := bridge.Definitions(ctx, fixtureSession("cred-a")); err != nil {
		t.Fatalf("cached fetch: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("manifest hits = %d, want 1 (cache)", got)
	}
	if _, err := bridge.Definitions(ctx, fixtureSession("cred-b")); err != nil {
		t.Fatalf("second credential fetch: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("manifest hits = %d, want 2 (per-credential cache)", got)
	}
	// TTL expiry refetches.
	now = now.Add(manifestCacheTTL + time.Second)
	if _, err := bridge.Definitions(ctx, fixtureSession("cred-a")); err != nil {
		t.Fatalf("post-TTL fetch: %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("manifest hits = %d, want 3 (TTL expiry)", got)
	}
}

func TestExecuteEnforcesPerSessionCallCap(t *testing.T) {
	f := loadFixture(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(f.CallResponseOK)
	}))
	defer server.Close()

	bridge := newBridge(t, server, func(o *Options) { o.MaxCallsPerSession = 2 })
	ctx := context.Background()
	session := fixtureSession("cred")
	for i := 0; i < 2; i++ {
		payload, ok := bridge.Execute(ctx, session, "kombify_knowledge_search", nil)
		if !ok {
			t.Fatalf("call %d: ok = false", i)
		}
		if _, hasError := payload["error"]; hasError {
			t.Fatalf("call %d unexpectedly errored: %#v", i, payload)
		}
	}
	payload, ok := bridge.Execute(ctx, session, "kombify_knowledge_search", nil)
	if !ok {
		t.Fatal("capped call: ok = false, want structured error payload")
	}
	errObj, isMap := payload["error"].(map[string]any)
	if !isMap || errObj["code"] != "tool_call_limit_exceeded" {
		t.Fatalf("capped call payload = %#v, want tool_call_limit_exceeded", payload)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream hits = %d, want 2 (cap enforced before HTTP)", got)
	}
	// Another session is unaffected.
	other := fixtureSession("cred")
	other.ID = "vs_other"
	if _, ok := bridge.Execute(ctx, other, "kombify_knowledge_search", nil); !ok {
		t.Fatal("other session: ok = false")
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("upstream hits = %d, want 3", got)
	}
}

func TestExecuteUnreachableEndpointReturnsStructuredError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // immediately unreachable

	bridge, err := New(Options{
		ManifestURL: server.URL + "/manifest",
		InvokeURL:   server.URL + "/call",
		Timeout:     500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	payload, ok := bridge.Execute(context.Background(), fixtureSession("cred"), "tool", nil)
	if !ok {
		t.Fatal("ok = false, want structured error payload")
	}
	errObj, isMap := payload["error"].(map[string]any)
	if !isMap || errObj["code"] != "tool_bridge_unreachable" {
		t.Fatalf("payload = %#v, want tool_bridge_unreachable", payload)
	}
}

func roundTrip(t *testing.T, value map[string]any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func readAll(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	return io.ReadAll(r.Body)
}
