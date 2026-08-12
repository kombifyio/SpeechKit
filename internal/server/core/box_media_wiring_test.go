//go:build linux

package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

const boxMediaCoreTokenEnv = "SPEECHKIT_CORE_TEST_BOX_MEDIA_TOKEN"

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
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveServer(context.Background(), cfg, app)
	}()
	local.runtimeErr = errors.New("whisper child exited")
	close(local.runtimeDone)

	select {
	case serveErr := <-serveDone:
		if serveErr == nil || !strings.Contains(serveErr.Error(), "Box media runtime: whisper child exited") {
			t.Fatalf("serveServer error=%v", serveErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not exit after owned local STT death")
	}
	_, components, _ := app.Health.Snapshot()
	entry := components[boxMediaHealthComponent]
	if entry.Status != StatusUnavailable || !entry.Blocking || !strings.Contains(entry.Detail, "STT runtime stopped") {
		t.Fatalf("Box media health after local exit=%#v", entry)
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

func TestServeServerPropagatesBoxMediaListenerFailure(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.ListenAddr = "127.0.0.1:0"
	cfg.Server.AuthMode = "none"
	app := newServerApp(cfg, RunOptions{})
	errCh := make(chan error, 1)
	errCh <- errors.New("listener failed")
	listener := &boxMediaCoreListener{errors: errCh}
	app.BoxMediaRuntime = &boxMediaServerRuntime{listener: listener}

	err := serveServer(context.Background(), cfg, app)
	if err == nil || !strings.Contains(err.Error(), "Box media runtime: listener failed") {
		t.Fatalf("serveServer error=%v", err)
	}
	if listener.shutdowns.Load() != 1 {
		t.Fatalf("Box media listener shutdowns=%d, want 1", listener.shutdowns.Load())
	}
	_, components, _ := app.Health.Snapshot()
	if entry := components[boxMediaHealthComponent]; entry.Status != StatusUnavailable {
		t.Fatalf("failed Box media health=%#v", entry)
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

type boxMediaCoreLocalSTT struct {
	name        string
	startErr    error
	healthErr   error
	runtimeErr  error
	stayUnready bool
	runtimeDone chan struct{}
	ready       atomic.Bool
	starts      atomic.Int32
	stops       atomic.Int32
	// transientFailures is how many upcoming Health calls fail before the
	// stub starts answering normally again.
	transientFailures atomic.Int32
}

func (p *boxMediaCoreLocalSTT) Name() string {
	if strings.TrimSpace(p.name) == "" {
		return "local"
	}
	return p.name
}

func (p *boxMediaCoreLocalSTT) StartServer(context.Context) error {
	p.starts.Add(1)
	if p.startErr != nil {
		return p.startErr
	}
	if !p.stayUnready {
		p.ready.Store(true)
	}
	return nil
}

func (p *boxMediaCoreLocalSTT) StopServer() {
	p.stops.Add(1)
	p.ready.Store(false)
}

func (p *boxMediaCoreLocalSTT) RuntimeDone() <-chan struct{} {
	if p.runtimeDone == nil {
		p.runtimeDone = make(chan struct{})
	}
	return p.runtimeDone
}

func (p *boxMediaCoreLocalSTT) RuntimeError() error { return p.runtimeErr }

func (p *boxMediaCoreLocalSTT) IsReady() bool { return p.ready.Load() }

func (p *boxMediaCoreLocalSTT) Health(context.Context) error {
	// transientFailures models a child that stalls for a few probes and then
	// answers again — the case the supervisor must ride out rather than treat
	// as death. Zero (the default) keeps the previous fixed behaviour.
	if n := p.transientFailures.Load(); n > 0 {
		p.transientFailures.Store(n - 1)
		return errors.New("transient probe stall")
	}
	return p.healthErr
}

func (p *boxMediaCoreLocalSTT) Transcribe(context.Context, []byte, stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Text: "Küchenlicht aus", Provider: p.Name()}, nil
}

type boxMediaCoreTTSProvider struct{ healthErr error }

func (p boxMediaCoreTTSProvider) Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error) {
	return &tts.Result{Audio: []byte("test"), Format: "wav", SampleRate: 48_000, Provider: p.Name()}, nil
}

func (boxMediaCoreTTSProvider) Name() string           { return "core-box-local-tts" }
func (boxMediaCoreTTSProvider) Kind() tts.ProviderKind { return tts.ProviderKindLocalBuiltIn }
func (p boxMediaCoreTTSProvider) Health(context.Context) error {
	return p.healthErr
}

type boxMediaCoreListener struct {
	errors                chan error
	closeErrorsOnShutdown bool
	shutdownErr           error
	closeOnce             sync.Once
	shutdowns             atomic.Int32
}

func (l *boxMediaCoreListener) Errors() <-chan error { return l.errors }
func (*boxMediaCoreListener) Addr() net.Addr         { return nil }
func (l *boxMediaCoreListener) Shutdown(context.Context) error {
	l.shutdowns.Add(1)
	if l.closeErrorsOnShutdown {
		l.closeOnce.Do(func() { close(l.errors) })
	}
	return l.shutdownErr
}

type boxMediaCoreCertificates struct {
	certificateFile string
	privateKeyFile  string
	caFile          string
	caSHA256        string
	caPool          *x509.CertPool
}

func validBoxMediaCoreConfig(t *testing.T) (*config.Config, *x509.CertPool) {
	t.Helper()
	listenAddr := freeBoxMediaCoreAddr(t)
	certificates := writeBoxMediaCoreCertificates(t, listenAddr)
	t.Setenv(boxMediaCoreTokenEnv, "box-media-core-token-0123456789abcdef")
	cfg := validDeviceAgentCoreConfig(t, newBoxMediaCoreHA(t).URL)
	cfg.Local = config.LocalConfig{Enabled: true, ModelPath: filepath.Join(t.TempDir(), "ggml-small.bin"), Port: 9000, GPU: "cpu"}
	cfg.Server.DeviceAgent.BoxMedia = config.ServerDeviceAgentBoxMediaConfig{
		Enabled: true, ListenAddr: listenAddr,
		CertificateFile: certificates.certificateFile,
		PrivateKeyFile:  certificates.privateKeyFile,
		PinnedCAFile:    certificates.caFile,
		PinnedCASHA256:  certificates.caSHA256,
		TokenEnv:        boxMediaCoreTokenEnv,
		DeviceID:        "device-kitchen",
		PairingID:       "pairing-kitchen-v1",
		RoomID:          "room-kitchen",
		Transcript:      "Küchenlicht aus",
		CommandID:       "kitchen-light-off",
		Locale:          "de-DE",
	}
	return cfg, certificates.caPool
}

func validBoxMediaCoreApp(t *testing.T, cfg *config.Config, local *boxMediaCoreLocalSTT) (*App, interface{ Close() error }) {
	t.Helper()
	app := newServerApp(cfg, RunOptions{})
	app.TTSRouter = tts.NewRouter(tts.StrategyLocalOnly, boxMediaCoreTTSProvider{})
	app.TTSEnabled = true
	app.STTRouter = &router.Router{}
	app.STTRouter.SetLocal(local)
	ledger, err := wireDeviceAgentBridge(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("wireDeviceAgentBridge: %v", err)
	}
	return app, ledger
}

func newBoxMediaCoreHA(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message":"API running."}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func freeBoxMediaCoreAddr(t *testing.T) string {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("list interfaces for Box media address: %v", err)
	}
	for _, raw := range addresses {
		prefix, parseErr := netip.ParsePrefix(raw.String())
		if parseErr != nil {
			continue
		}
		address := prefix.Addr().Unmap()
		if !address.Is4() || !address.IsPrivate() {
			continue
		}
		listener, listenErr := net.Listen("tcp4", net.JoinHostPort(address.String(), "0"))
		if listenErr != nil {
			continue
		}
		reserved := listener.Addr().String()
		if closeErr := listener.Close(); closeErr != nil {
			t.Fatalf("release Box media address: %v", closeErr)
		}
		return reserved
	}
	t.Fatal("no bindable RFC1918 IPv4 address available for Box media test")
	return ""
}

func writeBoxMediaCoreCertificates(t *testing.T, listenAddr string) boxMediaCoreCertificates {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "SpeechKit Box Test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true, IsCA: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		t.Fatalf("parse Box media address: %v", err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "SpeechKit Box Test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP(host)},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatalf("marshal server key: %v", err)
	}
	root := t.TempDir()
	certificateFile := filepath.Join(root, "box-media.crt")
	privateKeyFile := filepath.Join(root, "box-media.key")
	caFile := filepath.Join(root, "box-media-ca.crt")
	if err := os.WriteFile(certificateFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o600); err != nil {
		t.Fatalf("write server certificate: %v", err)
	}
	if err := os.WriteFile(privateKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write server key: %v", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatalf("write CA certificate: %v", err)
	}
	digest := sha256.Sum256(caDER)
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		t.Fatal("append generated CA")
	}
	return boxMediaCoreCertificates{
		certificateFile: certificateFile, privateKeyFile: privateKeyFile, caFile: caFile,
		caSHA256: hex.EncodeToString(digest[:]), caPool: caPool,
	}
}
