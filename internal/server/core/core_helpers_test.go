//go:build linux

package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

func serveServerSettingsWithBearer(app *App, req *http.Request) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	middleware.Auth(middleware.AuthOptions{
		Mode:                "bearer",
		BearerTokenProvider: func() string { return "test-token" },
	})(app.Mux).ServeHTTP(rec, req)
	return rec
}

func serveServerSettingsWithBearerRole(app *App, req *http.Request, role string) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	middleware.Auth(middleware.AuthOptions{
		Mode:                "bearer",
		BearerTokenProvider: func() string { return "test-token" },
		BearerRole:          role,
	})(app.Mux).ServeHTTP(rec, req)
	return rec
}

const boxMediaCoreTokenEnv = "SPEECHKIT_CORE_TEST_BOX_MEDIA_TOKEN"

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
	app.STTRouter = &stt.Router{}
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

func freeLoopbackHTTPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback HTTP address: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback HTTP address: %v", err)
	}
	return addr
}

func waitForCoreListen(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", addr, 25*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("main listener did not become reachable: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
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
