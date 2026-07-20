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
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoxMediaTLSServerStartsOnlyTLS13WithoutRedirects(t *testing.T) {
	addr := freeBoxMediaAddr(t)
	certPath, keyPath, certPEM := writeBoxMediaTestCertificate(t, net.ParseIP("127.0.0.1"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
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

func TestBoxMediaTLSFiniteTurnE2E(t *testing.T) {
	addr := freeBoxMediaAddr(t)
	certPath, keyPath, certPEM := writeBoxMediaTestCertificate(t, net.ParseIP("127.0.0.1"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	fixture := newBoxMediaFixture(t)
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
	validCert, validKey, _ := writeBoxMediaTestCertificate(t, net.ParseIP("127.0.0.1"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	wrongCert, wrongKey, _ := writeBoxMediaTestCertificate(t, net.ParseIP("127.0.0.2"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	expiredCert, expiredKey, _ := writeBoxMediaTestCertificate(t, net.ParseIP("127.0.0.1"), time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name   string
		config BoxMediaTLSConfig
	}{
		{name: "wildcard host", config: BoxMediaTLSConfig{ListenAddr: ":8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "unspecified host", config: BoxMediaTLSConfig{ListenAddr: "0.0.0.0:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "public host", config: BoxMediaTLSConfig{ListenAddr: "203.0.113.9:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "DNS host", config: BoxMediaTLSConfig{ListenAddr: "speechkit.local:8444", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "zero port", config: BoxMediaTLSConfig{ListenAddr: "127.0.0.1:0", CertificateFile: validCert, PrivateKeyFile: validKey}},
		{name: "relative certificate", config: BoxMediaTLSConfig{ListenAddr: "127.0.0.1:8444", CertificateFile: "server.crt", PrivateKeyFile: validKey}},
		{name: "same cert and key", config: BoxMediaTLSConfig{ListenAddr: "127.0.0.1:8444", CertificateFile: validCert, PrivateKeyFile: validCert}},
		{name: "missing key", config: BoxMediaTLSConfig{ListenAddr: "127.0.0.1:8444", CertificateFile: validCert, PrivateKeyFile: filepath.Join(t.TempDir(), "missing.key")}},
		{name: "wrong IP SAN", config: BoxMediaTLSConfig{ListenAddr: "127.0.0.1:8444", CertificateFile: wrongCert, PrivateKeyFile: wrongKey}},
		{name: "expired certificate", config: BoxMediaTLSConfig{ListenAddr: "127.0.0.1:8444", CertificateFile: expiredCert, PrivateKeyFile: expiredKey}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewBoxMediaTLSServer(test.config, handler); err == nil {
				t.Fatal("unsafe Box media TLS bootstrap was accepted")
			}
		})
	}
	if _, err := NewBoxMediaTLSServer(BoxMediaTLSConfig{ListenAddr: "127.0.0.1:8444", CertificateFile: validCert, PrivateKeyFile: validKey}, nil); err == nil {
		t.Fatal("nil handler was accepted")
	}
}

func freeBoxMediaAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback address: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback address: %v", err)
	}
	return addr
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
