//go:build linux

package core

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

func TestWireBoxMediaListenerDisabledIsInert(t *testing.T) {
	cfg := &config.Config{}
	app := newServerApp(cfg, RunOptions{})
	runtime, err := wireBoxMediaListener(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("wireBoxMediaListener: %v", err)
	}
	if runtime != nil || app.BoxMediaRuntime != nil {
		t.Fatal("disabled Box media returned a runtime")
	}
	_, components, _ := app.Health.Snapshot()
	entry, ok := components[boxMediaHealthComponent]
	if !ok || entry.Status != StatusDisabled || entry.Blocking {
		t.Fatalf("disabled Box media health=%#v present=%v", entry, ok)
	}
}

func TestWireBoxMediaListenerStartsDedicatedLocalRuntime(t *testing.T) {
	cfg, caPool := validBoxMediaCoreConfig(t)
	app, ledger := validBoxMediaCoreApp(t, cfg, &boxMediaCoreLocalSTT{})
	defer ledger.Close() //nolint:errcheck // test cleanup
	app.Health.SetReadyWithOptions("stt.local", StatusUnavailable, "stale startup probe", ComponentOptions{
		Blocking: true,
		Kind:     "provider",
	})

	runtime, err := wireBoxMediaListener(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("wireBoxMediaListener: %v", err)
	}
	defer runtime.Shutdown(context.Background()) //nolint:errcheck // test cleanup
	if runtime == nil || runtime.Addr() == nil || app.BoxMediaRuntime != runtime {
		t.Fatalf("Box media runtime=%#v app runtime=%#v", runtime, app.BoxMediaRuntime)
	}
	local := app.STTRouter.Local().(*boxMediaCoreLocalSTT)
	if local.starts.Load() != 1 || !local.IsReady() {
		t.Fatalf("local STT starts=%d ready=%v", local.starts.Load(), local.IsReady())
	}
	_, components, _ := app.Health.Snapshot()
	if entry := components[boxMediaHealthComponent]; entry.Status != StatusOK || !entry.Blocking {
		t.Fatalf("Box media health=%#v", entry)
	}
	if entry := components["stt.local"]; entry.Status != StatusOK || entry.Detail != "ready" {
		t.Fatalf("local STT health after successful Box probe=%#v", entry)
	}

	// The general mux remains unaware of the Box path: no fifth G0 route and
	// no general-server credential can reach the dedicated handler.
	recorder := httptest.NewRecorder()
	app.Mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/box-media/turn", http.NoBody))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("general mux Box route status=%d, want 404", recorder.Code)
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{ //nolint:gosec // test pins the generated local CA and TLS 1.3.
		RootCAs: caPool, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
	}}}
	response, err := client.Get("https://" + runtime.Addr().String() + "/v1/box-media/turn")
	if err != nil {
		t.Fatalf("GET dedicated Box listener: %v", err)
	}
	defer response.Body.Close() //nolint:errcheck // test response cleanup
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("dedicated Box listener status=%d, want 405", response.StatusCode)
	}

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown Box media runtime: %v", err)
	}
	if local.stops.Load() != 1 || local.IsReady() {
		t.Fatalf("local STT stops=%d ready=%v", local.stops.Load(), local.IsReady())
	}
}

func TestWireBoxMediaListenerPreservesPreexistingLocalRuntimeOnShutdown(t *testing.T) {
	cfg, _ := validBoxMediaCoreConfig(t)
	local := &boxMediaCoreLocalSTT{}
	local.ready.Store(true)
	app, ledger := validBoxMediaCoreApp(t, cfg, local)
	defer ledger.Close() //nolint:errcheck // test cleanup

	runtime, err := wireBoxMediaListener(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("wireBoxMediaListener: %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown Box media runtime: %v", err)
	}
	if local.starts.Load() != 0 || local.stops.Load() != 0 || !local.IsReady() {
		t.Fatalf("pre-existing local STT starts=%d stops=%d ready=%v, want 0/0/true", local.starts.Load(), local.stops.Load(), local.IsReady())
	}
}

func TestBoxMediaServerRuntimeRetainsFirstShutdownError(t *testing.T) {
	sentinel := errors.New("listener drain failed")
	listener := &boxMediaCoreListener{errors: make(chan error), shutdownErr: sentinel}
	local := &boxMediaCoreLocalSTT{}
	local.ready.Store(true)
	runtime := &boxMediaServerRuntime{listener: listener, localSTT: local, ownsLocalSTT: true}

	if err := runtime.Shutdown(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("first Shutdown error=%v, want sentinel", err)
	}
	if err := runtime.Shutdown(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("repeated Shutdown error=%v, want retained sentinel", err)
	}
	if listener.shutdowns.Load() != 1 || local.stops.Load() != 1 {
		t.Fatalf("shutdown calls listener=%d local stops=%d, want 1/1", listener.shutdowns.Load(), local.stops.Load())
	}
}

func TestWireBoxMediaListenerFailsBlockingReadinessWhenOwnedLocalRuntimeExits(t *testing.T) {
	cfg, _ := validBoxMediaCoreConfig(t)
	local := &boxMediaCoreLocalSTT{}
	app, ledger := validBoxMediaCoreApp(t, cfg, local)
	defer ledger.Close() //nolint:errcheck // test cleanup

	runtime, err := wireBoxMediaListener(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("wireBoxMediaListener: %v", err)
	}
	cfg.Server.ListenAddr = freeLoopbackHTTPAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveServer(ctx, cfg, app)
	}()
	waitForCoreListen(t, cfg.Server.ListenAddr)
	local.runtimeErr = errors.New("whisper child exited")
	close(local.runtimeDone)

	deadline := time.Now().Add(3 * time.Second)
	for {
		_, components, _ := app.Health.Snapshot()
		entry := components[boxMediaHealthComponent]
		if entry.Status == StatusUnavailable && entry.Blocking && strings.Contains(entry.Detail, "STT runtime stopped") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Box media health after local exit=%#v", entry)
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case serveErr := <-serveDone:
		t.Fatalf("HTTP server exited after Box STT death: %v", serveErr)
	default:
	}
	waitForCoreListen(t, cfg.Server.ListenAddr)
	cancel()
	select {
	case serveErr := <-serveDone:
		if serveErr != nil {
			t.Fatalf("clean shutdown after Box STT death: %v", serveErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not drain after context cancel")
	}
	if local.stops.Load() != 1 || local.IsReady() {
		t.Fatalf("owned local STT stops=%d ready=%v, want 1/false", local.stops.Load(), local.IsReady())
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated shutdown failed Box media runtime: %v", err)
	}
}

func TestBoxMediaSupervisorFailsBlockingReadinessOnLocalHealthLoss(t *testing.T) {
	sentinel := errors.New("whisper health failed")
	listener := &boxMediaCoreListener{errors: make(chan error)}
	local := &boxMediaCoreLocalSTT{healthErr: sentinel, runtimeDone: make(chan struct{})}
	local.ready.Store(true)
	runtime := &boxMediaServerRuntime{
		listener: listener, localSTT: local, localSupervisor: local,
		health: NewHealthRegistry(), healthInterval: time.Millisecond,
	}
	runtime.startSupervisor(context.Background())

	select {
	case err := <-runtime.Errors():
		if !errors.Is(err, sentinel) {
			t.Fatalf("supervisor error=%v, want health sentinel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Box media supervisor did not report local health loss")
	}
	_, components, _ := runtime.health.Snapshot()
	if entry := components[boxMediaHealthComponent]; entry.Status != StatusUnavailable || !entry.Blocking {
		t.Fatalf("Box media health after local probe failure=%#v", entry)
	}
	if entry := components["stt.local"]; entry.Status != StatusUnavailable {
		t.Fatalf("local STT health after local probe failure=%#v", entry)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// A probe failure is not evidence of a dead child. The probe runs against the
// same whisper process that may be busy on a Box turn, so a stall looks exactly
// like a corpse — and escalating on the first miss took down dictation, assist,
// voiceagent and the device-agent bridge with it. Below the threshold the
// feature reports degraded and keeps running; a later good probe restores it.
func TestBoxMediaSupervisorRidesOutTransientProbeFailures(t *testing.T) {
	listener := &boxMediaCoreListener{errors: make(chan error)}
	local := &boxMediaCoreLocalSTT{runtimeDone: make(chan struct{})}
	local.ready.Store(true)
	local.transientFailures.Store(int32(boxMediaMaxProbeFailures - 1))

	runtime := &boxMediaServerRuntime{
		listener: listener, localSTT: local, localSupervisor: local,
		health: NewHealthRegistry(), healthInterval: time.Millisecond,
	}
	runtime.startSupervisor(context.Background())

	// Give the supervisor well over the failing probes plus a recovering one.
	deadline := time.After(3 * time.Second)
	recovered := false
	for !recovered {
		select {
		case err := <-runtime.Errors():
			t.Fatalf("supervisor escalated on a transient stall: %v", err)
		case <-deadline:
			_, components, _ := runtime.health.Snapshot()
			t.Fatalf("Box media never recovered; health=%#v", components[boxMediaHealthComponent])
		case <-time.After(20 * time.Millisecond):
			_, components, _ := runtime.health.Snapshot()
			if components[boxMediaHealthComponent].Status == StatusOK {
				recovered = true
			}
		}
	}

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestWireBoxMediaListenerFailsClosedBeforeListen(t *testing.T) {
	tests := []struct {
		name   string
		local  *boxMediaCoreLocalSTT
		mutate func(*testing.T, *config.Config, *App)
		want   string
		stops  int32
		ready  bool
	}{
		{name: "local startup failure", local: &boxMediaCoreLocalSTT{startErr: errors.New("start failed")}, want: "start Box media host-local STT", stops: 1},
		{name: "local startup without ready", local: &boxMediaCoreLocalSTT{stayUnready: true}, want: "without readiness", stops: 1},
		{name: "local health failure", local: &boxMediaCoreLocalSTT{healthErr: errors.New("not healthy")}, want: "probe Box media host-local STT", stops: 1},
		{name: "foreign local identity", local: &boxMediaCoreLocalSTT{name: "cloud"}, want: "concrete host-local STT", stops: 0},
		{name: "no ready local TTS", local: &boxMediaCoreLocalSTT{}, mutate: func(_ *testing.T, _ *config.Config, app *App) {
			app.TTSRouter = tts.NewRouter(tts.StrategyLocalOnly, boxMediaCoreTTSProvider{healthErr: errors.New("tts down")})
		}, want: "ready local TTS", stops: 1},
		{name: "already-ready local then untrusted certificate CA", local: &boxMediaCoreLocalSTT{}, mutate: func(t *testing.T, cfg *config.Config, _ *App) {
			other := writeBoxMediaCoreCertificates(t, freeBoxMediaCoreAddr(t))
			cfg.Server.DeviceAgent.BoxMedia.CertificateFile = other.certificateFile
			cfg.Server.DeviceAgent.BoxMedia.PrivateKeyFile = other.privateKeyFile
		}, want: "against pinned CA", stops: 0, ready: true},
		{name: "TLS key load failure", local: &boxMediaCoreLocalSTT{}, mutate: func(_ *testing.T, cfg *config.Config, _ *App) {
			cfg.Server.DeviceAgent.BoxMedia.PrivateKeyFile += ".missing"
		}, want: "load key pair", stops: 1},
		{name: "TLS bind failure", local: &boxMediaCoreLocalSTT{}, mutate: func(t *testing.T, cfg *config.Config, _ *App) {
			listener, err := net.Listen("tcp", cfg.Server.DeviceAgent.BoxMedia.ListenAddr)
			if err != nil {
				t.Fatalf("occupy Box media listener: %v", err)
			}
			t.Cleanup(func() { _ = listener.Close() })
		}, want: "box media TLS: listen", stops: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := validBoxMediaCoreConfig(t)
			app, ledger := validBoxMediaCoreApp(t, cfg, tc.local)
			defer ledger.Close() //nolint:errcheck // test cleanup
			tc.local.ready.Store(tc.ready)
			if tc.mutate != nil {
				tc.mutate(t, cfg, app)
			}
			runtime, err := wireBoxMediaListener(context.Background(), cfg, app)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wireBoxMediaListener runtime=%#v error=%v, want %q", runtime, err, tc.want)
			}
			if runtime != nil || app.BoxMediaRuntime != nil {
				t.Fatal("failed Box media wiring exposed a runtime")
			}
			if got := tc.local.stops.Load(); got != tc.stops {
				t.Fatalf("local STT stops=%d, want %d", got, tc.stops)
			}
			if tc.ready && tc.local.starts.Load() != 0 {
				t.Fatalf("already-ready local STT starts=%d, want 0", tc.local.starts.Load())
			}
			_, components, _ := app.Health.Snapshot()
			if entry, exists := components[boxMediaHealthComponent]; exists && entry.Status == StatusOK {
				t.Fatalf("failed Box media wiring published ready health: %#v", entry)
			}
		})
	}
}

func TestServeServerBoxMediaListenerFailureKeepsHTTPServing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.ListenAddr = freeLoopbackHTTPAddr(t)
	cfg.Server.AuthMode = "none"
	app := newServerApp(cfg, RunOptions{})
	errCh := make(chan error, 1)
	errCh <- errors.New("listener failed")
	listener := &boxMediaCoreListener{errors: errCh}
	app.BoxMediaRuntime = &boxMediaServerRuntime{listener: listener}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveServer(ctx, cfg, app) }()
	waitForCoreListen(t, cfg.Server.ListenAddr)

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, components, _ := app.Health.Snapshot()
		if components[boxMediaHealthComponent].Status == StatusUnavailable {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("failed Box media health=%#v", components[boxMediaHealthComponent])
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("HTTP server exited after Box listener failure: %v", err)
	default:
	}
	waitForCoreListen(t, cfg.Server.ListenAddr)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("clean shutdown after Box listener failure: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not drain after context cancel")
	}
}

func TestServeServerMainListenFailureStillShutsDownBoxRuntime(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve main listener: %v", err)
	}
	defer occupied.Close() //nolint:errcheck // test cleanup

	cfg := &config.Config{}
	cfg.Server.ListenAddr = occupied.Addr().String()
	cfg.Server.AuthMode = "none"
	app := newServerApp(cfg, RunOptions{})
	listener := &boxMediaCoreListener{errors: make(chan error)}
	local := &boxMediaCoreLocalSTT{}
	local.ready.Store(true)
	app.BoxMediaRuntime = &boxMediaServerRuntime{
		listener: listener, localSTT: local, ownsLocalSTT: true,
	}

	err = serveServer(context.Background(), cfg, app)
	if err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("serveServer error=%v, want occupied main-listener failure", err)
	}
	if listener.shutdowns.Load() != 1 || local.stops.Load() != 1 {
		t.Fatalf("early failure cleanup listener=%d local=%d, want 1/1", listener.shutdowns.Load(), local.stops.Load())
	}
}

func TestServeServerContextShutdownTreatsClosedBoxErrorChannelAsClean(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.ListenAddr = freeBoxMediaCoreAddr(t)
	cfg.Server.AuthMode = "none"
	app := newServerApp(cfg, RunOptions{})
	listener := &boxMediaCoreListener{errors: make(chan error), closeErrorsOnShutdown: true}
	app.BoxMediaRuntime = &boxMediaServerRuntime{listener: listener}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveServer(ctx, cfg, app) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", cfg.Server.ListenAddr, 25*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("main listener did not become reachable before clean shutdown: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("clean context shutdown reported a listener failure: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("clean context shutdown did not complete")
	}
	if listener.shutdowns.Load() != 1 {
		t.Fatalf("Box media listener shutdowns=%d, want 1", listener.shutdowns.Load())
	}
}
