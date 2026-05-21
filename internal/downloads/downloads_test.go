package downloads

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/internal/testutil"
)

func init() {
	// Tests use httptest loopback servers; relax download URL validation.
	DownloadURLValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}
}

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("expected non-nil Manager")
	}
	jobs := m.AllJobs()
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs, got %d", len(jobs))
	}
}

func waitForJobStatus(t *testing.T, m *Manager, timeout time.Duration, statuses ...Status) JobView {
	t.Helper()
	allowed := make(map[Status]bool, len(statuses))
	for _, status := range statuses {
		allowed[status] = true
	}
	var jobs []JobView
	testutil.Eventually(t, timeout, 50*time.Millisecond, func() bool {
		jobs = m.AllJobs()
		return len(jobs) == 1 && allowed[jobs[0].Status]
	})
	return jobs[0]
}

func TestHTTPDownload(t *testing.T) {
	content := []byte("fake-model-binary-data-for-test")
	h := sha256.Sum256(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager()

	var done bool
	var mu sync.Mutex
	snap := m.Start(Item{
		ID:        "test-model",
		ProfileID: "test-profile",
		Name:      "Test Model",
		SizeBytes: int64(len(content)),
		Kind:      KindHTTP,
		URL:       srv.URL + "/model.bin",
		SHA256:    hex.EncodeToString(h[:]),
	}, dir, func(item Item) {
		mu.Lock()
		done = true
		mu.Unlock()
	})

	if snap.ID == "" {
		t.Fatal("expected non-empty job ID")
	}
	if snap.Status != StatusPending {
		t.Fatalf("expected pending status, got %s", snap.Status)
	}

	waitForJobStatus(t, m, 5*time.Second, StatusDone)

	jobs := m.AllJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Status != StatusDone {
		t.Fatalf("expected done, got %s (error: %s)", jobs[0].Status, jobs[0].Error)
	}

	// Verify file was written.
	got, err := os.ReadFile(filepath.Join(dir, "model.bin"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(content))
	}

	// Verify onDone callback fired.
	mu.Lock()
	if !done {
		t.Error("expected onDone callback to have fired")
	}
	mu.Unlock()
}

func TestHTTPDownloadServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := NewManager()
	m.Start(Item{
		ID:     "fail-model",
		Kind:   KindHTTP,
		URL:    srv.URL + "/model.bin",
		SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	}, t.TempDir(), nil)

	waitForJobStatus(t, m, 5*time.Second, StatusDone, StatusFailed)

	jobs := m.AllJobs()
	if len(jobs) != 1 || jobs[0].Status != StatusFailed {
		t.Fatalf("expected failed status, got %v", jobs)
	}
}

func TestCancelJob(t *testing.T) {
	// Slow server that blocks until cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
		// Write a tiny bit then block.
		w.Write([]byte("x"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	m := NewManager()
	snap := m.Start(Item{
		ID:        "cancel-model",
		Kind:      KindHTTP,
		URL:       srv.URL + "/model.bin",
		SizeBytes: 999999999,
		SHA256:    "0000000000000000000000000000000000000000000000000000000000000000",
	}, t.TempDir(), nil)

	waitForJobStatus(t, m, 5*time.Second, StatusRunning)

	if !m.CancelJob(snap.ID) {
		t.Fatal("CancelJob returned false")
	}

	waitForJobStatus(t, m, 5*time.Second, StatusCancelled)

	jobs := m.AllJobs()
	if len(jobs) != 1 || jobs[0].Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %v", jobs)
	}
}

func TestCancelNonExistentJob(t *testing.T) {
	m := NewManager()
	if m.CancelJob("nonexistent") {
		t.Error("expected false for nonexistent job")
	}
}

func TestCatalogReturnsItems(t *testing.T) {
	cfg := &config.Config{}
	items := Catalog(t.Context(), cfg)
	if len(items) == 0 {
		t.Fatal("expected non-empty catalog")
	}
	// All items should have required fields.
	for _, item := range items {
		if item.ID == "" {
			t.Error("item has empty ID")
		}
		if item.ProfileID == "" {
			t.Errorf("item %s has empty ProfileID", item.ID)
		}
		if item.Kind != KindHTTP && item.Kind != KindOllama {
			t.Errorf("item %s has unknown kind %q", item.ID, item.Kind)
		}
		if item.Kind == KindHTTP && item.URL == "" {
			t.Errorf("HTTP item %s has empty URL", item.ID)
		}
		if item.Kind == KindHTTP && item.SHA256 == "" {
			t.Errorf("HTTP item %s has empty SHA256", item.ID)
		}
		if item.Kind == KindOllama && item.OllamaModel == "" {
			t.Errorf("Ollama item %s has empty OllamaModel", item.ID)
		}
	}
}

func TestCatalogExposesWhisperCppTurboAsRecommendedChoice(t *testing.T) {
	cfg := &config.Config{}
	items := Catalog(t.Context(), cfg)

	var whisperItems []Item
	for _, item := range items {
		if item.ProfileID == "stt.local.whispercpp" {
			whisperItems = append(whisperItems, item)
		}
	}

	if len(whisperItems) != 3 {
		t.Fatalf("whisper.cpp download choices = %d, want 3", len(whisperItems))
	}
	if whisperItems[0].ID != "whisper.ggml-small" {
		t.Fatalf("first whisper choice = %q, want %q", whisperItems[0].ID, "whisper.ggml-small")
	}
	if whisperItems[1].ID != "whisper.ggml-large-v3-turbo" {
		t.Fatalf("second whisper choice = %q, want %q", whisperItems[1].ID, "whisper.ggml-large-v3-turbo")
	}
	if whisperItems[2].ID != "whisper.ggml-large-v3" {
		t.Fatalf("third whisper choice = %q, want %q", whisperItems[2].ID, "whisper.ggml-large-v3")
	}
	if whisperItems[0].Recommended {
		t.Fatal("expected whisper small to no longer be recommended")
	}
	if !whisperItems[1].Recommended {
		t.Fatal("expected whisper turbo to be recommended")
	}
	if whisperItems[2].Recommended {
		t.Fatal("expected whisper large v3 to stay non-recommended")
	}
}

func TestCatalogMarksPendingLocalInstallStarterModelAsSelected(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyLocalInstallDefaults(cfg, &config.InstallState{Mode: config.InstallModeLocal})

	items := CatalogWithStatus(t.Context(), cfg, StatusOptions{})

	var starterSelected, turboSelected bool
	for _, item := range items {
		switch item.ID {
		case "whisper.ggml-small":
			starterSelected = item.Selected
		case "whisper.ggml-large-v3-turbo":
			turboSelected = item.Selected
		}
	}

	if !starterSelected {
		t.Fatal("whisper small should be selected for a pending local install")
	}
	if turboSelected {
		t.Fatal("whisper turbo should not be selected for a pending local install")
	}
}

func TestCatalogMarksBundledStarterWhisperModelAvailable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	modelPath := filepath.Join(filepath.Dir(exe), "models", config.DefaultLocalSTTModel)
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatalf("mkdir bundled models dir: %v", err)
	}
	if err := os.WriteFile(modelPath, []byte("bundled starter model"), 0o644); err != nil {
		t.Fatalf("write bundled starter model: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(modelPath)
	})

	cfg := &config.Config{}
	config.ApplyLocalInstallDefaults(cfg, &config.InstallState{Mode: config.InstallModeLocal})

	items := CatalogWithStatus(t.Context(), cfg, StatusOptions{})
	for _, item := range items {
		if item.ProfileID == "stt.local.whispercpp" && filepath.Base(item.URL) == config.DefaultLocalSTTModel {
			if !item.Available {
				t.Fatal("bundled starter model should be reported available")
			}
			if !item.Selected {
				t.Fatal("bundled starter model should be selected for local install defaults")
			}
			return
		}
	}
	t.Fatalf("catalog missing default local STT model %q", config.DefaultLocalSTTModel)
}

func TestCatalogExposesOllamaItemsForAllUserModes(t *testing.T) {
	cfg := &config.Config{}
	items := Catalog(t.Context(), cfg)

	wantByProfile := map[string]string{
		"stt.ollama.gemma4-e4b-transcribe":    "ollama.gemma4-e4b-dictate",
		"assist.ollama.gemma4-e4b":            "ollama.gemma4-e4b-assist",
		"realtime.ollama.gemma4-e4b-pipeline": "ollama.gemma4-e4b-voice",
	}

	seen := map[string]string{}
	for _, item := range items {
		if item.Kind == KindOllama {
			seen[item.ProfileID] = item.ID
		}
	}

	for profileID, itemID := range wantByProfile {
		if got := seen[profileID]; got != itemID {
			t.Fatalf("ollama download item for %s = %q, want %q", profileID, got, itemID)
		}
	}
}

func TestCatalogExposesLlamaCppAssistDownloadChoices(t *testing.T) {
	cfg := &config.Config{}
	items := Catalog(t.Context(), cfg)

	var assistItems []Item
	for _, item := range items {
		if item.ProfileID == "assist.builtin.gemma4-e4b" && item.Kind == KindHTTP {
			assistItems = append(assistItems, item)
		}
	}

	if len(assistItems) < 2 {
		t.Fatalf("llama.cpp assist download choices = %d, want at least 2", len(assistItems))
	}
	if assistItems[0].ID != "llamacpp.gemma-4-e4b-it-q4-k-m" {
		t.Fatalf("first llama.cpp assist choice = %q, want %q", assistItems[0].ID, "llamacpp.gemma-4-e4b-it-q4-k-m")
	}
	if !assistItems[0].Recommended {
		t.Fatal("expected Gemma 4 E4B Q4_K_M llama.cpp assist model to be recommended")
	}
	if assistItems[1].ID != "llamacpp.gemma-4-e2b-it-q8-0" {
		t.Fatalf("second llama.cpp assist choice = %q, want %q", assistItems[1].ID, "llamacpp.gemma-4-e2b-it-q8-0")
	}
}

func TestCatalogMarksWhisperCppTurboSelectedFromConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Local.Model = "ggml-large-v3-turbo.bin"

	items := Catalog(t.Context(), cfg)
	for _, item := range items {
		if item.ID == "whisper.ggml-large-v3-turbo" {
			if !item.Selected {
				t.Fatal("expected whisper turbo to be selected from config")
			}
			return
		}
	}

	t.Fatal("expected whisper turbo item in catalog")
}

func TestCatalogMarksLlamaCppAssistModelSelectedFromConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.LocalLLM.ModelPath = filepath.Join("C:", "SpeechKit", "models", "gemma-4-E4B-it-Q4_K_M.gguf")

	items := Catalog(t.Context(), cfg)
	for _, item := range items {
		if item.ID == "llamacpp.gemma-4-e4b-it-q4-k-m" {
			if !item.Selected {
				t.Fatal("expected llama.cpp assist Q4_K_M model to be selected from config")
			}
			return
		}
	}

	t.Fatal("expected llama.cpp assist Q4_K_M item in catalog")
}

func TestCatalogMarksLocalLLMRuntimeRequiredWhenBundledServerMissing(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("SPEECHKIT_ALLOW_LLAMA_PATH", "0")

	cfg := &config.Config{}
	cfg.LocalLLM.BaseURL = "http://127.0.0.1:1/v1"

	items := CatalogWithStatus(t.Context(), cfg, StatusOptions{ProbeRuntimes: true})
	for _, item := range items {
		if item.ID == "llamacpp.gemma-4-e4b-it-q4-k-m" {
			if item.RuntimeReady {
				t.Fatal("expected local LLM runtime to be unavailable")
			}
			if item.RuntimeProblem == "" {
				t.Fatal("expected local LLM runtime problem")
			}
			return
		}
	}

	t.Fatal("expected llama.cpp assist Q4_K_M item in catalog")
}

func TestCatalogMarksLocalLLMRuntimeReadyWhenBundledServerPresent(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("SPEECHKIT_ALLOW_LLAMA_PATH", "0")
	managedDir := filepath.Join(localAppData, "SpeechKit", "llama")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "llama-server.exe"), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}

	items := CatalogWithStatus(t.Context(), cfg, StatusOptions{ProbeRuntimes: true})
	for _, item := range items {
		if item.ID == "llamacpp.gemma-4-e4b-it-q4-k-m" {
			if !item.RuntimeReady {
				t.Fatalf("expected local LLM runtime to be ready: %s", item.RuntimeProblem)
			}
			if item.RuntimeProblem != "" {
				t.Fatalf("expected no runtime problem, got %q", item.RuntimeProblem)
			}
			return
		}
	}

	t.Fatal("expected llama.cpp assist Q4_K_M item in catalog")
}

func TestCatalogMarksLocalLLMArtifactsSelectedPerProfile(t *testing.T) {
	cfg := &config.Config{}
	cfg.LocalLLM.ModelPath = filepath.Join("C:", "SpeechKit", "models", "gemma-4-E4B-it-Q4_K_M.gguf")
	cfg.LocalLLM.AssistModel = "gemma-4-E4B-it-Q4_K_M.gguf"
	cfg.LocalLLM.AgentModel = "gemma-4-E2B-it-Q8_0.gguf"

	items := CatalogWithStatus(t.Context(), cfg, StatusOptions{})
	selected := map[string]bool{}
	for _, item := range items {
		selected[item.ID] = item.Selected
	}

	if !selected["llamacpp.gemma-4-e4b-it-q4-k-m"] {
		t.Fatal("expected assist Q4_K_M artifact to be selected")
	}
	if selected["llamacpp.gemma-4-e4b-it-q4-k-m-voice"] {
		t.Fatal("did not expect voice Q4_K_M artifact to be selected from assist model")
	}
	if !selected["llamacpp.gemma-4-e2b-it-q8-0-voice"] {
		t.Fatal("expected voice E2B Q8_0 artifact to be selected")
	}
}

func TestCatalogMarksWakewordArtifactsInWakewordModelDir(t *testing.T) {
	root := t.TempDir()
	wakeDir := filepath.Join(root, "wakeword")
	if err := os.MkdirAll(wakeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"melspectrogram.onnx", "embedding_model.onnx", "hey_quby.onnx"} {
		if err := os.WriteFile(filepath.Join(wakeDir, filename), []byte("wakeword"), 0o600); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
	}

	cfg := &config.Config{}
	cfg.General.ModelDownloadDir = root
	cfg.Wakeword.PhraseID = "hey_quby"

	items := CatalogWithStatus(t.Context(), cfg, StatusOptions{})
	seen := map[string]Item{}
	for _, item := range items {
		if strings.HasPrefix(item.ID, "wakeword.") {
			seen[item.ID] = item
		}
	}

	for _, id := range []string{"wakeword.shared.melspec", "wakeword.shared.embedding", "wakeword.phrase.hey_quby"} {
		item, ok := seen[id]
		if !ok {
			t.Fatalf("missing wake-word catalog item %s", id)
		}
		if !item.Available {
			t.Fatalf("%s should be available from %s", id, wakeDir)
		}
	}
	if !seen["wakeword.phrase.hey_quby"].Selected {
		t.Fatal("selected wake phrase should be marked selected")
	}
}

func TestReadinessStatusOptionsSkipOllamaProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "gemma4:e4b"}},
		})
	}))
	defer srv.Close()

	old := OllamaBaseURL
	OllamaBaseURL = srv.URL
	defer func() { OllamaBaseURL = old }()

	cfg := &config.Config{}
	items := CatalogWithStatus(t.Context(), cfg, ReadinessStatusOptions)
	for _, item := range items {
		if item.Kind == KindOllama && item.ID == "ollama.gemma4-e4b-assist" {
			if item.Available {
				t.Fatal("readiness catalog should not probe Ollama availability")
			}
			return
		}
	}

	t.Fatal("expected Ollama assist item in catalog")
}

func TestResolveWhisperModelsDir(t *testing.T) {
	t.Run("from default model download dir", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.General.ModelDownloadDir = filepath.Join("D:", "SpeechKit", "Models")
		cfg.Local.ModelPath = filepath.Join("C:", "legacy", "ggml-small.bin")
		got := ResolveWhisperModelsDir(cfg)
		want := filepath.Join("D:", "SpeechKit", "Models")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("from config", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Local.ModelPath = filepath.Join("C:", "models", "ggml-small.bin")
		got := ResolveWhisperModelsDir(cfg)
		want := filepath.Join("C:", "models")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("nil config", func(t *testing.T) {
		got := ResolveWhisperModelsDir(nil)
		if got == "" {
			t.Error("expected non-empty dir")
		}
	})
}

func TestFileIsPresent(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "exists.bin")
	os.WriteFile(present, []byte("x"), 0o644)

	if !FileIsPresent(present) {
		t.Error("expected present for existing file")
	}
	if FileIsPresent(filepath.Join(dir, "nope.bin")) {
		t.Error("expected not present for missing file")
	}
	if FileIsPresent(dir) {
		t.Error("expected not present for directory")
	}
}

func TestOllamaModelPresentWhenOffline(t *testing.T) {
	// When Ollama isn't running, should return false without error.
	// Point OllamaBaseURL at an unreachable address so we don't hit real Ollama.
	old := OllamaBaseURL
	OllamaBaseURL = "http://127.0.0.1:1" // guaranteed-unreachable port
	defer func() { OllamaBaseURL = old }()

	result := OllamaModelPresent(t.Context(), "nonexistent:latest")
	if result {
		t.Error("expected false when Ollama is unreachable")
	}
}

func TestOllamaModelPresentWithMockServer(t *testing.T) {
	models := []struct {
		Name string `json:"name"`
	}{
		{Name: "gemma4:e4b"},
		{Name: "llama3:latest"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"models": models})
	}))
	defer srv.Close()

	old := OllamaBaseURL
	OllamaBaseURL = srv.URL
	defer func() { OllamaBaseURL = old }()

	if !OllamaModelPresent(t.Context(), "gemma4:e4b") {
		t.Error("expected true for exact match gemma4:e4b")
	}
	if !OllamaModelPresent(t.Context(), "gemma4:other") {
		t.Error("expected true for prefix match gemma4:other")
	}
	if OllamaModelPresent(t.Context(), "nonexistent:latest") {
		t.Error("expected false for nonexistent model")
	}
}

func TestHTTPDownloadSHA256Pass(t *testing.T) {
	content := []byte("valid-model-data-for-sha256-test")
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager()

	m.Start(Item{
		ID:        "sha-pass",
		Kind:      KindHTTP,
		URL:       srv.URL + "/model.bin",
		SizeBytes: int64(len(content)),
		SHA256:    hash,
	}, dir, nil)

	waitForJobStatus(t, m, 5*time.Second, StatusDone, StatusFailed)

	jobs := m.AllJobs()
	if len(jobs) != 1 || jobs[0].Status != StatusDone {
		t.Fatalf("expected done, got %s (error: %s)", jobs[0].Status, jobs[0].Error)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "model.bin"))
	if !bytes.Equal(got, content) {
		t.Error("content mismatch")
	}
}

func TestHTTPDownloadRequiresSHA256(t *testing.T) {
	content := []byte("model-data-without-hash")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager()

	m.Start(Item{
		ID:        "sha-required",
		Kind:      KindHTTP,
		URL:       srv.URL + "/model.bin",
		SizeBytes: int64(len(content)),
	}, dir, nil)

	waitForJobStatus(t, m, 5*time.Second, StatusDone, StatusFailed)

	jobs := m.AllJobs()
	if len(jobs) != 1 || jobs[0].Status != StatusFailed {
		t.Fatalf("expected failed, got %v", jobs)
	}
	if jobs[0].Error == "" {
		t.Fatal("expected error message")
	}
	if _, err := os.Stat(filepath.Join(dir, "model.bin")); err == nil {
		t.Error("expected unhashed file not to be written")
	}
}

func TestHTTPDownloadSHA256Mismatch(t *testing.T) {
	content := []byte("this-data-will-not-match-the-hash")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager()

	m.Start(Item{
		ID:        "sha-fail",
		Kind:      KindHTTP,
		URL:       srv.URL + "/model.bin",
		SizeBytes: int64(len(content)),
		SHA256:    "0000000000000000000000000000000000000000000000000000000000000000",
	}, dir, nil)

	waitForJobStatus(t, m, 5*time.Second, StatusDone, StatusFailed)

	jobs := m.AllJobs()
	if len(jobs) != 1 || jobs[0].Status != StatusFailed {
		t.Fatalf("expected failed, got %s", jobs[0].Status)
	}
	if jobs[0].Error == "" {
		t.Fatal("expected error message")
	}

	// Corrupt file should have been removed.
	if _, err := os.Stat(filepath.Join(dir, "model.bin")); err == nil {
		t.Error("expected file to be removed after SHA256 mismatch")
	}
	if _, err := os.Stat(filepath.Join(dir, "model.bin.download")); err == nil {
		t.Error("expected temp file to be removed after SHA256 mismatch")
	}
}
