//go:build linux

package core

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// newTestStoreForWiring spins up a SQLite store (pure-Go modernc.org)
// so the wiring test can exercise the real type-assertion path
// without standing up Postgres.
func newTestStoreForWiring(t *testing.T) store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wiring.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: path})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestWireWakewordTraining_Disabled_Returns503AndIsNonBlocking(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.TrainingData.AcceptUploads = false

	app := &App{Cfg: cfg, Mux: http.NewServeMux(), Health: NewHealthRegistry()}
	wireWakewordTraining(cfg, app)

	// Endpoint is mounted and returns 503.
	srv := httptest.NewServer(app.Mux)
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/v1/wakeword/activations")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}

	// Health is disabled (not unavailable) and non-blocking.
	_, components, _ := app.Health.Snapshot()
	entry, ok := components["api.wakeword_training"]
	if !ok {
		t.Fatal("component api.wakeword_training not registered")
	}
	if entry.Status != StatusDisabled {
		t.Errorf("status = %q, want disabled", entry.Status)
	}
	if entry.Blocking {
		t.Error("disabled feature must be non-blocking on /readyz")
	}

	// Readiness should still be green because the only registered
	// component is non-blocking and disabled doesn't block.
	overall, _, _ := app.Health.ReadinessSnapshot()
	if overall != StatusStarting && overall != StatusOK {
		t.Errorf("readiness = %q, want starting or ok (component non-blocking)", overall)
	}

	// Strict readiness must also be green — a deliberately disabled
	// feature is not a health problem (StatusDisabled has rank 0).
	strictOverall, _, _ := app.Health.Snapshot()
	if statusRank(strictOverall) > statusRank(StatusOK) {
		t.Errorf("strict readiness = %q (rank %d), want ok-level (disabled must not degrade strict)", strictOverall, statusRank(strictOverall))
	}
}

func TestWireWakewordTraining_Enabled_MountsAndMarksReady(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.TrainingData.AcceptUploads = true
	cfg.Server.TrainingData.AudioDir = t.TempDir()
	cfg.Server.MaxUploadMB = 5

	app := &App{
		Cfg:    cfg,
		Mux:    http.NewServeMux(),
		Health: NewHealthRegistry(),
		Store:  newTestStoreForWiring(t),
	}
	wireWakewordTraining(cfg, app)

	// Health is OK.
	_, components, _ := app.Health.Snapshot()
	entry := components["api.wakeword_training"]
	if entry.Status != StatusOK {
		t.Errorf("status = %q, want ok", entry.Status)
	}

	// Endpoint is mounted and unauthenticated calls reach the handler
	// (return 401 because there's no Identity on the request ctx).
	srv := httptest.NewServer(app.Mux)
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/v1/wakeword/activations")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (handler reached, no identity)", resp.StatusCode)
	}
}

func TestWireWakewordTraining_EnabledButStoreLacksInterface(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.TrainingData.AcceptUploads = true
	cfg.Server.TrainingData.AudioDir = t.TempDir()

	app := &App{
		Cfg:    cfg,
		Mux:    http.NewServeMux(),
		Health: NewHealthRegistry(),
		Store:  nil, // no store at all
	}
	wireWakewordTraining(cfg, app)

	_, components, _ := app.Health.Snapshot()
	entry := components["api.wakeword_training"]
	if entry.Status != StatusUnavailable {
		t.Errorf("status = %q, want unavailable when store nil", entry.Status)
	}
	if entry.Blocking {
		t.Error("missing-store should not block /readyz overall")
	}
}

func TestResolveAudioDir_DefaultsToModelDir(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.ModelDir = "/tmp/sk"
	got := resolveAudioDir(cfg)
	want := filepath.Join("/tmp/sk", "wakeword-activations")
	if got != want {
		t.Errorf("resolveAudioDir = %q, want %q", got, want)
	}
}

func TestResolveAudioDir_RespectsExplicitOverride(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.ModelDir = "/tmp/sk"
	cfg.Server.TrainingData.AudioDir = "/var/spool/wakeword"
	got := resolveAudioDir(cfg)
	if got != "/var/spool/wakeword" {
		t.Errorf("explicit override ignored: got %q", got)
	}
}

func TestResolveAudioDir_FallbackWhenModelDirEmpty(t *testing.T) {
	cfg := &config.Config{}
	got := resolveAudioDir(cfg)
	if !strings.HasPrefix(got, "/var/lib/speechkit") {
		t.Errorf("expected /var/lib/speechkit fallback, got %q", got)
	}
}
