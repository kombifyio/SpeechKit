//go:build linux

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

const (
	deviceAgentCoreTestBearerEnv = "SPEECHKIT_CORE_TEST_SERVER_BEARER"
	deviceAgentCoreTestHAEnv     = "SPEECHKIT_CORE_TEST_HA_TOKEN"
	deviceAgentCoreTestTokenEnv  = "SPEECHKIT_CORE_TEST_DEVICE_TOKEN"
	deviceAgentCoreTestHAToken   = "home-assistant-token-with-more-than-32-bytes"
	deviceAgentCoreTestToken     = "device-pairing-token-with-more-than-32-bytes"
)

func TestDeviceAgentAuthRoutesAreExactPostAndRuntimeConditional(t *testing.T) {
	expectedPaths := map[string]bool{
		"/v1/device-agent/register": false,
		"/v1/device-agent/events":   false,
		"/v1/device-agent/assist":   false,
		"/v1/device-agent/tts":      false,
	}
	routes := deviceAgentAuthRoutes()
	if len(routes) != len(expectedPaths) {
		t.Fatalf("deviceAgentAuthRoutes() returned %d routes, want %d: %#v", len(routes), len(expectedPaths), routes)
	}
	for _, route := range routes {
		if _, ok := expectedPaths[route.Path]; !ok {
			t.Fatalf("unexpected device-agent public route: %#v", route)
		}
		if route.PathPrefix != "" || route.PathSuffix != "" {
			t.Fatalf("device-agent auth route must be exact, got %#v", route)
		}
		if len(route.Methods) != 1 || route.Methods[0] != http.MethodPost {
			t.Fatalf("device-agent auth route must permit only POST, got %#v", route)
		}
		expectedPaths[route.Path] = true
	}
	for path, found := range expectedPaths {
		if !found {
			t.Fatalf("missing device-agent auth route %q", path)
		}
	}

	t.Setenv(deviceAgentCoreTestBearerEnv, "general-server-bearer")
	for _, mounted := range []bool{false, true} {
		mounted := mounted
		t.Run(map[bool]string{false: "bridge_not_mounted", true: "bridge_mounted"}[mounted], func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.ListenAddr = "127.0.0.1:0"
			cfg.Server.AuthMode = string(middleware.AuthModeBearer)
			cfg.Server.BearerTokenEnv = deviceAgentCoreTestBearerEnv
			app := newServerApp(cfg, RunOptions{})
			app.DeviceAgentBridgeMounted = mounted
			app.Mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})

			chain, err := serverMiddlewareChain(context.Background(), cfg, app)
			if err != nil {
				t.Fatalf("serverMiddlewareChain: %v", err)
			}
			handler := chain(app.Mux)
			for path := range expectedPaths {
				req := httptest.NewRequest(http.MethodPost, path, http.NoBody)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				want := http.StatusUnauthorized
				if mounted {
					want = http.StatusNoContent
				}
				if rec.Code != want {
					t.Errorf("mounted=%v POST %s status=%d, want %d", mounted, path, rec.Code, want)
				}
			}

			for _, request := range []struct {
				method string
				path   string
			}{
				{method: http.MethodGet, path: "/v1/device-agent/register"},
				{method: http.MethodPost, path: "/v1/device-agent/register/"},
				{method: http.MethodPost, path: "/v1/device-agent/register/extra"},
				{method: http.MethodPost, path: "/v1/device-agent/unknown"},
				{method: http.MethodPost, path: "/api/v1/device-agent/register"},
				{method: http.MethodPost, path: "/api/v1/device-agent/assist"},
			} {
				req := httptest.NewRequest(request.method, request.path, http.NoBody)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("mounted=%v %s %s status=%d, want 401", mounted, request.method, request.path, rec.Code)
				}
			}
		})
	}
}

func TestRegisterAPIAliasNeverProjectsDeviceAgent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/device-agent/register", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/device-agent/assist", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	registerAPIAlias(mux)

	direct := httptest.NewRecorder()
	mux.ServeHTTP(direct, httptest.NewRequest(http.MethodPost, "/v1/device-agent/register", http.NoBody))
	if direct.Code != http.StatusNoContent {
		t.Fatalf("direct device-agent route status=%d, want 204", direct.Code)
	}

	for _, path := range []string{
		"/api/v1/device-agent",
		"/api/v1/device-agent/",
		"/api/v1/device-agent/register",
		"/api/v1/device-agent/assist",
		"/api/v1/device-agent/register/extra",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, http.NoBody))
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s status=%d, want 404", path, rec.Code)
		}
	}
}

func TestWireDeviceAgentBridgeDisabledIsInert(t *testing.T) {
	cfg := &config.Config{}
	app := newServerApp(cfg, RunOptions{})

	ledger, err := wireDeviceAgentBridge(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("wireDeviceAgentBridge disabled: %v", err)
	}
	if ledger != nil {
		t.Fatal("disabled device-agent bridge returned a claim ledger")
	}
	if app.DeviceAgentBridgeMounted {
		t.Fatal("disabled device-agent bridge set DeviceAgentBridgeMounted")
	}

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/device-agent/register", http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled device-agent route status=%d, want 404", rec.Code)
	}

	_, components, _ := app.Health.Snapshot()
	entry, ok := components["api.device_agent"]
	if !ok || entry.Status != StatusDisabled || entry.Blocking {
		t.Fatalf("disabled device-agent health=%#v, present=%v", entry, ok)
	}
}

func TestWireDeviceAgentBridgeMissingTTSFailsClosed(t *testing.T) {
	cfg := validDeviceAgentCoreConfig(t, "http://127.0.0.1:8123")
	app := newServerApp(cfg, RunOptions{})

	ledger, err := wireDeviceAgentBridge(context.Background(), cfg, app)
	if !errors.Is(err, errDeviceAgentTTSUnavailable) {
		t.Fatalf("wireDeviceAgentBridge error=%v, want %v", err, errDeviceAgentTTSUnavailable)
	}
	if ledger != nil {
		t.Fatal("failed device-agent bridge returned a claim ledger")
	}
	if app.DeviceAgentBridgeMounted {
		t.Fatal("failed device-agent bridge set DeviceAgentBridgeMounted")
	}

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/device-agent/register", http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("failed device-agent route status=%d, want 404", rec.Code)
	}
}

func TestWireDeviceAgentBridgeMountsCompleteLocalDependencies(t *testing.T) {
	var probes atomic.Int32
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+deviceAgentCoreTestHAToken {
			t.Errorf("Home Assistant probe Authorization=%q", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		probes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"API running."}`))
	}))
	defer ha.Close()

	t.Setenv(deviceAgentCoreTestHAEnv, deviceAgentCoreTestHAToken)
	t.Setenv(deviceAgentCoreTestTokenEnv, deviceAgentCoreTestToken)
	cfg := validDeviceAgentCoreConfig(t, ha.URL)
	app := newServerApp(cfg, RunOptions{})
	app.TTSRouter = tts.NewRouter(tts.StrategyLocalOnly, deviceAgentCoreTestTTSProvider{})
	app.TTSEnabled = true

	ledger, err := wireDeviceAgentBridge(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("wireDeviceAgentBridge: %v", err)
	}
	if ledger == nil {
		t.Fatal("wireDeviceAgentBridge returned nil ledger")
	}
	defer func() {
		if err := ledger.Close(); err != nil {
			t.Errorf("close claim ledger: %v", err)
		}
	}()
	if !app.DeviceAgentBridgeMounted {
		t.Fatal("complete device-agent bridge did not set DeviceAgentBridgeMounted")
	}
	_, components, _ := app.Health.Snapshot()
	if entry := components["api.device_agent"]; entry.Status != StatusOK || entry.Detail != "local HA bridge listening" {
		t.Fatalf("device-agent health=%#v", entry)
	}

	registration := wire.Registration{
		Version:      wire.CurrentProtocolVersion,
		RegisteredAt: time.Now().UTC(),
		Device: wire.DeviceDescriptor{
			AgentID:  "agent-kitchen",
			DeviceID: "device-kitchen",
			RoomID:   "room-kitchen",
		},
		Capabilities: wire.Capabilities{Assist: true, TTS: true},
		Health:       wire.Health{Status: "ready", CaptureReady: true, OutputReady: true},
	}
	raw, err := json.Marshal(registration)
	if err != nil {
		t.Fatalf("marshal registration: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/device-agent/register", bytes.NewReader(raw))
	req.RemoteAddr = "127.0.0.1:43123"
	req.Header.Set("Authorization", "Bearer "+deviceAgentCoreTestToken)
	req.Header.Set("X-SpeechKit-Device-ID", "device-kitchen")
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(wire.ServerInstanceHeader); got != "speechkit-local-test-instance" {
		t.Fatalf("server instance header=%q", got)
	}
	var ack wire.RegistrationAck
	if err := json.NewDecoder(rec.Body).Decode(&ack); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if ack.Status != "paired" || ack.PairingID != "pairing-kitchen-v1" || ack.ServerInstanceID != "speechkit-local-test-instance" {
		t.Fatalf("registration ack=%#v", ack)
	}
	if ack.Capabilities.HomeAssistant.Status != wire.CapabilityReady || ack.Capabilities.TTS.Status != wire.CapabilityReady {
		t.Fatalf("registration capabilities=%#v", ack.Capabilities)
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("Home Assistant probes=%d, want 1", got)
	}

	deniedReq := httptest.NewRequest(http.MethodPost, "/v1/device-agent/register", bytes.NewReader(raw))
	deniedReq.RemoteAddr = "192.0.2.44:43123"
	deniedReq.Header.Set("Authorization", "Bearer "+deviceAgentCoreTestToken)
	deniedReq.Header.Set("X-SpeechKit-Device-ID", "device-kitchen")
	deniedReq.Header.Set("Content-Type", "application/json")
	denied := httptest.NewRecorder()
	app.Mux.ServeHTTP(denied, deniedReq)
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "source_cidr_not_allowed") {
		t.Fatalf("non-LAN registration status=%d body=%s", denied.Code, denied.Body.String())
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("denied source reached Home Assistant; probes=%d", got)
	}
}

func validDeviceAgentCoreConfig(t *testing.T, homeAssistantURL string) *config.Config {
	t.Helper()
	now := time.Now().UTC()
	t.Setenv(deviceAgentCoreTestHAEnv, deviceAgentCoreTestHAToken)
	t.Setenv(deviceAgentCoreTestTokenEnv, deviceAgentCoreTestToken)
	return &config.Config{
		TTS: config.TTSConfig{Enabled: true, Strategy: "local-only"},
		Assist: config.AssistConfig{HomeAssistant: config.AssistHomeAssistantConfig{
			URL:      homeAssistantURL,
			TokenEnv: deviceAgentCoreTestHAEnv,
			Language: "de-DE",
		}},
		Server: config.ServerConfig{
			ListenAddr: "127.0.0.1:0",
			DeviceAgent: config.ServerDeviceAgentConfig{
				Enabled:          true,
				ServerInstanceID: "speechkit-local-test-instance",
				ClaimStorePath:   filepath.Join(t.TempDir(), "device-agent-claims.sqlite"),
				Devices: []config.ServerDeviceAgentDeviceConfig{{
					DeviceID:           "device-kitchen",
					PairingID:          "pairing-kitchen-v1",
					RoomID:             "room-kitchen",
					TokenEnv:           deviceAgentCoreTestTokenEnv,
					AllowedClientCIDRs: []string{"127.0.0.0/8"},
					LocalRules: []config.ServerDeviceAgentLocalRuleConfig{{
						RuleID: "kitchen-light-off", TriggerText: "Küchenlicht aus", Locale: "de-DE",
						Action: "turn_off", EntityID: "light.kitchen",
						NotBefore: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
					}},
				}},
			},
		},
	}
}

type deviceAgentCoreTestTTSProvider struct{}

func (deviceAgentCoreTestTTSProvider) Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error) {
	return &tts.Result{
		Audio:      []byte("test-audio"),
		Format:     "wav",
		SampleRate: 16_000,
		Provider:   "core-test-tts",
	}, nil
}

func (deviceAgentCoreTestTTSProvider) Name() string           { return "core-test-tts" }
func (deviceAgentCoreTestTTSProvider) Kind() tts.ProviderKind { return tts.ProviderKindLocalBuiltIn }
func (deviceAgentCoreTestTTSProvider) Health(context.Context) error {
	return nil
}
