package deviceagent

import (
	"bytes"
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
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestBoxMediaTLSServerStartsOnlyTLS13WithoutRedirects(t *testing.T) {
	addr := freeBoxMediaAddr(t)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("parse Box media test address: %v", err)
	}
	certPath, keyPath, certPEM := writeBoxMediaTestCertificate(t, net.ParseIP(host), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != BoxMediaTurnPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server, err := NewBoxMediaTLSServer(BoxMediaTLSConfig{
		ListenAddr: addr, CertificateFile: certPath, PrivateKeyFile: keyPath,
	}, handler)
	if err != nil {
		t.Fatalf("NewBoxMediaTLSServer: %v", err)
	}
	chain := server.CertificateChainDER()
	if len(chain) == 0 || len(chain[0]) == 0 {
		t.Fatal("loaded TLS certificate chain is empty")
	}
	chain[0][0] ^= 0xff
	if refreshed := server.CertificateChainDER(); len(refreshed) == 0 || refreshed[0][0] == chain[0][0] {
		t.Fatal("CertificateChainDER did not return a defensive snapshot")
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime, err := server.Start(ctx)
	if err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = runtime.Shutdown(shutdownCtx)
	})

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("append test root")
	}
	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{TLSClientConfig: &tls.Config{ //nolint:gosec // test trusts only its ephemeral CA.
			MinVersion: tls.VersionTLS13,
			RootCAs:    roots,
		}},
	}
	response, err := client.Get("https://" + runtime.Addr().String() + BoxMediaTurnPath)
	if err != nil {
		t.Fatalf("TLS request: %v", err)
	}
	defer response.Body.Close() //nolint:errcheck // test response is drained below
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if response.TLS == nil || response.TLS.Version != tls.VersionTLS13 {
		t.Fatalf("negotiated TLS=%#v", response.TLS)
	}
	legacyConfig := &tls.Config{ //nolint:gosec // intentionally proves TLS 1.2 is rejected.
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	legacyConnection, legacyErr := tls.Dial("tcp", runtime.Addr().String(), legacyConfig)
	if legacyErr == nil {
		_ = legacyConnection.Close()
		t.Fatal("TLS 1.2 connection unexpectedly succeeded")
	}

	wrongPath, err := client.Get("https://" + runtime.Addr().String() + BoxMediaTurnPath + "/")
	if err != nil {
		t.Fatalf("wrong-path request: %v", err)
	}
	defer wrongPath.Body.Close() //nolint:errcheck // test response is drained below
	_, _ = io.Copy(io.Discard, wrongPath.Body)
	if wrongPath.StatusCode != http.StatusNotFound || wrongPath.StatusCode == http.StatusMovedPermanently {
		t.Fatalf("wrong path status=%d, want exact 404 without redirect", wrongPath.StatusCode)
	}
}

func TestBoxMediaTLSServerShutdownCancelsAndDrainsActiveRequest(t *testing.T) {
	addr := freeBoxMediaAddr(t)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("parse Box media test address: %v", err)
	}
	certPath, keyPath, certPEM := writeBoxMediaTestCertificate(t, net.ParseIP(host), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	started := make(chan struct{})
	finished := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		defer close(finished)
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	server, err := NewBoxMediaTLSServer(BoxMediaTLSConfig{
		ListenAddr: addr, CertificateFile: certPath, PrivateKeyFile: keyPath,
	}, handler)
	if err != nil {
		t.Fatalf("NewBoxMediaTLSServer: %v", err)
	}
	runtime, err := server.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("append test root")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{ //nolint:gosec // test trusts only its ephemeral CA.
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}}}
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, requestErr := client.Get("https://" + runtime.Addr().String() + BoxMediaTurnPath)
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("active Box media request did not reach the handler")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Box media handler outlived runtime Shutdown")
	}
	select {
	case <-requestDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Box media client request did not finish after runtime Shutdown")
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated Shutdown did not retain the first nil result: %v", err)
	}
}

func TestBoxMediaTLSServerForceCloseRetainsFirstShutdownErrorAndDrains(t *testing.T) {
	addr := freeBoxMediaAddr(t)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("parse Box media test address: %v", err)
	}
	certPath, keyPath, certPEM := writeBoxMediaTestCertificate(t, net.ParseIP(host), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	started := make(chan struct{})
	finished := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		defer close(finished)
		<-release
	})
	server, err := NewBoxMediaTLSServer(BoxMediaTLSConfig{
		ListenAddr: addr, CertificateFile: certPath, PrivateKeyFile: keyPath,
	}, handler)
	if err != nil {
		t.Fatalf("NewBoxMediaTLSServer: %v", err)
	}
	runtime, err := server.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		releaseHandler()
		_ = runtime.Shutdown(context.Background())
	})

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("append test root")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{ //nolint:gosec // test trusts only its ephemeral CA.
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}}}
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, requestErr := client.Get("https://" + runtime.Addr().String() + BoxMediaTurnPath)
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("active Box media request did not reach the force-close handler")
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- runtime.Shutdown(shutdownCtx) }()
	select {
	case err := <-shutdownDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Shutdown error=%v, want canceled drain budget", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled Shutdown stayed blocked on the in-flight handler")
	}
	releaseHandler()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("force-closed handler did not finish after release")
	}
	select {
	case <-requestDone:
	case <-time.After(3 * time.Second):
		t.Fatal("force-closed client request did not finish")
	}
	if err := runtime.Shutdown(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("repeated Shutdown error=%v, want retained context cancellation", err)
	}
}

func TestBoxMediaRequestLifecycleWaitHonorsDeadline(t *testing.T) {
	var lifecycle boxMediaRequestLifecycle
	if !lifecycle.begin() {
		t.Fatal("initial request admission was rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := lifecycle.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error=%v, want canceled", err)
	}
	lifecycle.end()
}

func TestBoxMediaRequestLifecycleRejectsAdmissionAfterDrainStarts(t *testing.T) {
	var lifecycle boxMediaRequestLifecycle
	if !lifecycle.begin() {
		t.Fatal("initial request admission was rejected")
	}
	lifecycle.stopAccepting()
	if lifecycle.begin() {
		t.Fatal("request was admitted after drain started")
	}
	waitDone := make(chan struct{})
	go func() {
		_ = lifecycle.wait(context.Background())
		close(waitDone)
	}()
	select {
	case <-waitDone:
		t.Fatal("request lifecycle drain returned before the active request exited")
	case <-time.After(25 * time.Millisecond):
	}
	lifecycle.end()
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("request lifecycle drain did not finish after the active request exited")
	}
}

func TestBoxMediaTLSFiniteTurnE2E(t *testing.T) {
	addr := freeBoxMediaAddr(t)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("parse Box media test address: %v", err)
	}
	certPath, keyPath, certPEM := writeBoxMediaTestCertificate(t, net.ParseIP(host), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	fixture := newBoxMediaFixture(t)
	_, sourceCIDR, err := net.ParseCIDR(host + "/32")
	if err != nil {
		t.Fatalf("parse Box media test source CIDR: %v", err)
	}
	binding := fixture.handler.bridge.bindings["speaker-kitchen-001"]
	binding.allowed = []*net.IPNet{sourceCIDR}
	fixture.handler.bridge.bindings["speaker-kitchen-001"] = binding
	server, err := NewBoxMediaTLSServer(BoxMediaTLSConfig{
		ListenAddr: addr, CertificateFile: certPath, PrivateKeyFile: keyPath,
	}, fixture.handler)
	if err != nil {
		t.Fatalf("NewBoxMediaTLSServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime, err := server.Start(ctx)
	if err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = runtime.Shutdown(shutdownCtx)
	})

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("append test root")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{ //nolint:gosec // test trusts only its ephemeral CA.
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}}}
	input := boxL16(8000)
	digest := sha256.Sum256(input)
	requestID := mustBoxUUIDv7(t)
	request, err := http.NewRequest(http.MethodPost, "https://"+runtime.Addr().String()+BoxMediaTurnPath, bytes.NewReader(input))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "audio/L16; rate=16000; channels=1")
	request.Header.Set("Authorization", "Bearer "+testBoxMediaPairingToken)
	request.Header.Set(BoxMediaDeviceIDHeader, "speaker-kitchen-001")
	request.Header.Set(BoxMediaPairingIDHeader, "pairing-kitchen-v1")
	request.Header.Set(BoxMediaRequestIDHeader, requestID)
	request.Header.Set(BoxMediaInputSHA256Header, hex.EncodeToString(digest[:]))
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("finite turn: %v", err)
	}
	defer response.Body.Close() //nolint:errcheck // assertions consume the bounded response.
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read finite turn: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.TLS == nil || response.TLS.Version != tls.VersionTLS13 {
		t.Fatalf("finite turn status=%d TLS=%#v body=%s", response.StatusCode, response.TLS, string(body))
	}
	if response.Header.Get(BoxMediaDeviceIDHeader) != "speaker-kitchen-001" || response.Header.Get(BoxMediaRequestIDHeader) != requestID || response.Header.Get(BoxMediaReplayHeader) != "false" {
		t.Fatalf("finite turn identity/replay headers=%v", response.Header)
	}
	if len(body) != 4800*2 || fixture.ha.converseCalls != 1 || fixture.ha.verifyCalls != 1 || fixture.stt.calls != 1 {
		t.Fatalf("finite turn bytes=%d STT=%d HA converse=%d verify=%d", len(body), fixture.stt.calls, fixture.ha.converseCalls, fixture.ha.verifyCalls)
	}
}

func TestNewBoxMediaTLSServerFailsClosedOnUnsafeBootstrap(t *testing.T) {
	validAddr := "192.168.10.10:8444"
	validCert, validKey, _ := writeBoxMediaTestCertificate(t, net.ParseIP("192.168.10.10"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	wrongCert, wrongKey, _ := writeBoxMediaTestCertificate(t, net.ParseIP("192.168.10.11"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	expiredCert, expiredKey, _ := writeBoxMediaTestCertificate(t, net.ParseIP("192.168.10.10"), time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name   string
		config BoxMediaTLSConfig
	}{
		{name: "wildcard host", config: BoxMediaTLSConfig{ListenAddr: ":8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "unspecified host", config: BoxMediaTLSConfig{ListenAddr: "0.0.0.0:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "loopback host", config: BoxMediaTLSConfig{ListenAddr: "127.0.0.1:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "link-local host", config: BoxMediaTLSConfig{ListenAddr: "169.254.10.20:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "CGNAT host", config: BoxMediaTLSConfig{ListenAddr: "100.64.10.20:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "IPv6 ULA host", config: BoxMediaTLSConfig{ListenAddr: "[fd00::10]:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "IPv6 loopback host", config: BoxMediaTLSConfig{ListenAddr: "[::1]:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "public host", config: BoxMediaTLSConfig{ListenAddr: "203.0.113.9:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "DNS host", config: BoxMediaTLSConfig{ListenAddr: "speechkit.local:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "zero port", config: BoxMediaTLSConfig{ListenAddr: "192.168.10.10:0", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "relative certificate", config: BoxMediaTLSConfig{ListenAddr: validAddr, CertificateFile: "server.crt", PrivateKeyFile: validKey}},
		{name: "same cert and key", config: BoxMediaTLSConfig{ListenAddr: validAddr, CertificateFile: validCert, PrivateKeyFile: validCert}},
		{name: "missing key", config: BoxMediaTLSConfig{ListenAddr: validAddr, CertificateFile: validCert, PrivateKeyFile: filepath.Join(t.TempDir(), "missing.key")}},
		{name: "wrong IP SAN", config: BoxMediaTLSConfig{ListenAddr: validAddr, CertificateFile: wrongCert, PrivateKeyFile: wrongKey}},
		{name: "expired certificate", config: BoxMediaTLSConfig{ListenAddr: validAddr, CertificateFile: expiredCert, PrivateKeyFile: expiredKey}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewBoxMediaTLSServer(test.config, handler); err == nil {
				t.Fatal("unsafe Box media TLS bootstrap was accepted")
			}
		})
	}
	if _, err := NewBoxMediaTLSServer(BoxMediaTLSConfig{ListenAddr: validAddr, CertificateFile: validCert, PrivateKeyFile: validKey}, nil); err == nil {
		t.Fatal("nil handler was accepted")
	}
}

func TestValidateBoxMediaListenAddrAcceptsOnlyRFC1918Ranges(t *testing.T) {
	tests := []string{
		"10.1.2.3:8444",
		"172.16.0.1:8444",
		"172.31.255.254:8444",
		"192.168.10.20:8444",
	}
	for _, listenAddr := range tests {
		t.Run(listenAddr, func(t *testing.T) {
			if _, _, err := validateBoxMediaListenAddr(listenAddr); err != nil {
				t.Fatalf("validateBoxMediaListenAddr(%q): %v", listenAddr, err)
			}
		})
	}
}

func freeBoxMediaAddr(t *testing.T) string {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("enumerate local addresses: %v", err)
	}
	for _, raw := range addresses {
		prefix, parseErr := netip.ParsePrefix(raw.String())
		if parseErr != nil {
			continue
		}
		address := prefix.Addr().Unmap()
		if !boxMediaRFC1918Address(address) {
			continue
		}
		listener, listenErr := net.Listen("tcp4", net.JoinHostPort(address.String(), "0"))
		if listenErr != nil {
			continue
		}
		addr := listener.Addr().String()
		if closeErr := listener.Close(); closeErr != nil {
			t.Fatalf("release RFC1918 address: %v", closeErr)
		}
		return addr
	}
	t.Fatal("no bindable RFC1918 IPv4 address is available for the Box media TLS test")
	return ""
}

func writeBoxMediaTestCertificate(t *testing.T, ip net.IP, notBefore, notAfter time.Time) (string, string, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "SpeechKit Box media test"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{ip},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath, certPEM
}
