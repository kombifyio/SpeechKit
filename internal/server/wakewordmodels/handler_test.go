//go:build linux

package wakewordmodels

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/wakewordcatalog"
)

func TestFileRouteServesFromLocalDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hey_kombify.onnx"), []byte("model-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(Options{Enabled: true, LocalDir: dir}).Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Known catalog file present on disk → served.
	resp, err := http.Get(srv.URL + "/v1/wakeword/files/hey_kombify.onnx") //nolint:noctx // test client
	if err != nil {
		t.Fatalf("GET file: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "model-bytes" {
		t.Fatalf("served file = %d %q, want 200 model-bytes", resp.StatusCode, body)
	}

	// Known catalog file that is not on disk → 404 model_pending.
	if status, b := getJSON(t, srv.URL+"/v1/wakeword/files/hey_quby.onnx"); status != http.StatusNotFound {
		t.Errorf("absent file status = %d, want 404 (%v)", status, b)
	}

	// A name that is not a catalog file → 404 not_found (no serving).
	if status, _ := getJSON(t, srv.URL+"/v1/wakeword/files/secret.onnx"); status != http.StatusNotFound {
		t.Errorf("unknown file status = %d, want 404", status)
	}
}

func TestKnownModelFileRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"", "..", "../etc/passwd", "sub/dir.onnx", "a\\b", "secret.onnx"} {
		if knownModelFile(bad) {
			t.Errorf("knownModelFile(%q) = true, want false", bad)
		}
	}
	if !knownModelFile("hey_kombify.onnx") {
		t.Error("knownModelFile(hey_kombify.onnx) = false, want true")
	}
}

func newTestServer(enabled bool) *httptest.Server {
	mux := http.NewServeMux()
	New(Options{Enabled: enabled, Author: "SpeechKit", Website: "https://speechkit.example"}).Mount(mux)
	return httptest.NewServer(mux)
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // test client
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if resp.Header.Get("Content-Type") == "application/json" {
		_ = json.NewDecoder(resp.Body).Decode(&body)
	}
	return resp.StatusCode, body
}

func TestListEnumeratesPhrasesWithOpenWakeWord(t *testing.T) {
	srv := newTestServer(true)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/wakeword/models")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200", status)
	}
	models, _ := body["models"].([]any)
	if len(models) == 0 {
		t.Fatal("list returned no models")
	}
	var found bool
	for _, raw := range models {
		m, _ := raw.(map[string]any)
		if m["id"] != "hey_kombify" {
			continue
		}
		found = true
		formats, _ := m["formats"].([]any)
		if !containsStr(formats, "openwakeword") {
			t.Errorf("hey_kombify formats = %v, want to include openwakeword", formats)
		}
		// microWakeWord is pending → must NOT advertise a manifest URL.
		if _, ok := m["manifest_url"]; ok {
			t.Error("hey_kombify should not advertise a manifest_url until microWakeWord is published")
		}
		if m["openwakeword"] == nil {
			t.Error("hey_kombify should carry an openwakeword listing")
		}
	}
	if !found {
		t.Fatal("hey_kombify missing from list")
	}
}

func TestManifestPendingReturns404(t *testing.T) {
	srv := newTestServer(true)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/wakeword/models/hey_kombify/manifest.json")
	if status != http.StatusNotFound {
		t.Fatalf("manifest status = %d, want 404 (pending)", status)
	}
	if errObj, _ := body["error"].(map[string]any); errObj["code"] != "microwakeword_pending" {
		t.Errorf("error code = %v, want microwakeword_pending", errObj["code"])
	}
}

func TestSingleWordPhrasesServedAndPending(t *testing.T) {
	srv := newTestServer(true)
	defer srv.Close()

	_, body := getJSON(t, srv.URL+"/v1/wakeword/models")
	models, _ := body["models"].([]any)
	find := func(id string) map[string]any {
		for _, raw := range models {
			if m, _ := raw.(map[string]any); m["id"] == id {
				return m
			}
		}
		return nil
	}

	// jarvis: no model of either family published yet → empty formats, no
	// listing, no manifest URL.
	jarvis := find("jarvis")
	if jarvis == nil {
		t.Fatal("jarvis missing from model list")
	}
	if formats, _ := jarvis["formats"].([]any); len(formats) != 0 {
		t.Errorf("jarvis formats = %v, want empty until a model is published", formats)
	}
	if _, ok := jarvis["manifest_url"]; ok {
		t.Error("jarvis must not advertise a manifest_url")
	}
	if jarvis["openwakeword"] != nil {
		t.Error("jarvis must not carry an openwakeword listing")
	}

	// kombify: single-word openWakeWord published, microWakeWord still pending →
	// openwakeword format + listing, but no manifest_url yet.
	kombify := find("kombify")
	if kombify == nil {
		t.Fatal("kombify missing from model list")
	}
	if formats, _ := kombify["formats"].([]any); !containsStr(formats, "openwakeword") {
		t.Errorf("kombify formats = %v, want to include openwakeword", formats)
	}
	if kombify["openwakeword"] == nil {
		t.Error("kombify should carry an openwakeword listing")
	}
	if _, ok := kombify["manifest_url"]; ok {
		t.Error("kombify must not advertise a manifest_url while microWakeWord is pending")
	}

	// Both manifest routes report pending until a .tflite is published.
	for _, id := range []string{"jarvis", "kombify"} {
		status, mBody := getJSON(t, srv.URL+"/v1/wakeword/models/"+id+"/manifest.json")
		if status != http.StatusNotFound {
			t.Errorf("%q manifest status = %d, want 404 pending", id, status)
		}
		if errObj, _ := mBody["error"].(map[string]any); errObj["code"] != "microwakeword_pending" {
			t.Errorf("%q manifest error code = %v, want microwakeword_pending", id, errObj["code"])
		}
	}

	// kombify's openWakeWord triplet is actually served.
	status, owwBody := getJSON(t, srv.URL+"/v1/wakeword/models/kombify/openwakeword")
	if status != http.StatusOK {
		t.Fatalf("kombify openwakeword status = %d, want 200", status)
	}
	if phrase, _ := owwBody["phrase"].(map[string]any); phrase["sha256"] != "1cf8e8d80f2c9515fbcfbd36e99d537eb8fb87132657c03b6a4e65f606db2769" {
		t.Errorf("kombify openwakeword phrase sha256 = %v, unexpected", phrase["sha256"])
	}
}

func TestOpenWakeWordTripletServed(t *testing.T) {
	srv := newTestServer(true)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/wakeword/models/hey_kombify/openwakeword")
	if status != http.StatusOK {
		t.Fatalf("openwakeword status = %d, want 200", status)
	}
	phrase, _ := body["phrase"].(map[string]any)
	if phrase["sha256"] != "24c6d2d1c235892362ebf12b0055801d2f8461f856e15d704c3d8262304f4c9f" {
		t.Errorf("phrase sha256 = %v, unexpected", phrase["sha256"])
	}
	if body["melspec"] == nil || body["embedding"] == nil {
		t.Error("openwakeword response must include melspec + embedding")
	}
}

func TestUnknownModelReturns404(t *testing.T) {
	srv := newTestServer(true)
	defer srv.Close()

	status, _ := getJSON(t, srv.URL+"/v1/wakeword/models/does_not_exist/manifest.json")
	if status != http.StatusNotFound {
		t.Fatalf("unknown model status = %d, want 404", status)
	}
}

func TestDisabledReturns503(t *testing.T) {
	srv := newTestServer(false)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/wakeword/models")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("disabled status = %d, want 503", status)
	}
	if errObj, _ := body["error"].(map[string]any); errObj["code"] != "wakeword_models_disabled" {
		t.Errorf("error code = %v, want wakeword_models_disabled", errObj["code"])
	}
}

func TestNonGetReturns405(t *testing.T) {
	srv := newTestServer(true)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/wakeword/models", "application/json", nil) //nolint:noctx // test client
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", resp.StatusCode)
	}
}

// TestMicroManifestSchemaMatchesESPHomeV2 asserts the emitted manifest has
// exactly the ESPHome micro_wake_word v2 key set (cf. okay_nabu.json), so an
// ESPHome `micro_wake_word:` block can reference the URL unchanged. It uses a
// synthetic "available" model because no real microWakeWord model is published
// yet.
func TestMicroManifestSchemaMatchesESPHomeV2(t *testing.T) {
	m := wakewordcatalog.Model{
		ID:               "hey_kombify",
		WakeWord:         "Hey Kombify",
		TrainedLanguages: []string{"en"},
		MicroWakeWord: wakewordcatalog.MicroWakeWordArtifact{
			Available: true,
			File:      wakewordcatalog.FileArtifact{URL: "https://cdn.example/hey_kombify.tflite"},
			Params: wakewordcatalog.MicroWakeWordParams{
				ProbabilityCutoff: 0.97,
				SlidingWindowSize: 5,
				TensorArenaSize:   26080,
			},
		},
	}
	raw, err := json.Marshal(microManifestFor(m, "SpeechKit", "https://speechkit.example"))
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	wantTop := []string{"author", "micro", "model", "trained_languages", "type", "version", "wake_word", "website"}
	assertKeySet(t, "manifest", got, wantTop)

	if got["type"] != "micro" {
		t.Errorf("type = %v, want micro", got["type"])
	}
	if got["version"].(float64) != 2 {
		t.Errorf("version = %v, want 2", got["version"])
	}
	// Relative model filename is load-bearing for ESPHome urljoin resolution.
	if got["model"] != "model.tflite" {
		t.Errorf("model = %v, want relative \"model.tflite\"", got["model"])
	}

	micro, _ := got["micro"].(map[string]any)
	wantMicro := []string{"feature_step_size", "minimum_esphome_version", "probability_cutoff", "sliding_window_size", "tensor_arena_size"}
	assertKeySet(t, "micro", micro, wantMicro)
	if micro["feature_step_size"].(float64) != 10 {
		t.Errorf("feature_step_size = %v, want 10 (fixed default)", micro["feature_step_size"])
	}
	if micro["minimum_esphome_version"] != wakewordcatalog.DefaultMinimumESPHomeVersion {
		t.Errorf("minimum_esphome_version = %v, want default %q", micro["minimum_esphome_version"], wakewordcatalog.DefaultMinimumESPHomeVersion)
	}
}

func assertKeySet(t *testing.T, label string, obj map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("%s key set = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s key set = %v, want %v", label, got, want)
		}
	}
}

func containsStr(list []any, want string) bool {
	for _, v := range list {
		if s, _ := v.(string); s == want {
			return true
		}
	}
	return false
}
