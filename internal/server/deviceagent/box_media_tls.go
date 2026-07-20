package deviceagent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BoxMediaTLSConfig is the explicit bootstrap contract for the dedicated Box
// listener. Certificate provisioning is intentionally out of scope: the host
// supplies an existing absolute-path key pair, and the Box must separately pin
// the issuing local CA. There is no plaintext listener or HTTP redirect mode.
type BoxMediaTLSConfig struct {
	ListenAddr      string
	CertificateFile string
	PrivateKeyFile  string
}

// BoxMediaTLSServer owns only the exact Box media handler. It never mounts the
// general SpeechKit mux, device-agent v1 routes, MCP, Gateway, or proxy paths.
type BoxMediaTLSServer struct {
	config  BoxMediaTLSConfig
	handler http.Handler
	tls     *tls.Config
}

type BoxMediaTLSRuntime struct {
	server   *http.Server
	listener net.Listener
	errors   chan error
	once     sync.Once
}

func NewBoxMediaTLSServer(config BoxMediaTLSConfig, handler http.Handler) (*BoxMediaTLSServer, error) {
	if handler == nil {
		return nil, errors.New("box media TLS: handler is required")
	}
	host, port, err := validateBoxMediaListenAddr(config.ListenAddr)
	if err != nil {
		return nil, err
	}
	_ = port
	certPath := strings.TrimSpace(config.CertificateFile)
	keyPath := strings.TrimSpace(config.PrivateKeyFile)
	if !filepath.IsAbs(certPath) || !filepath.IsAbs(keyPath) || filepath.Clean(certPath) == filepath.Clean(keyPath) {
		return nil, errors.New("box media TLS: distinct absolute certificate and private-key paths are required")
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("box media TLS: load key pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("box media TLS: certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("box media TLS: parse leaf certificate: %w", err)
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, errors.New("box media TLS: leaf certificate is not currently valid")
	}
	if err := leaf.VerifyHostname(host); err != nil {
		return nil, fmt.Errorf("box media TLS: certificate does not identify listen address %q: %w", host, err)
	}
	pair.Leaf = leaf
	config.ListenAddr = net.JoinHostPort(host, strconv.Itoa(port))
	config.CertificateFile = certPath
	config.PrivateKeyFile = keyPath
	return &BoxMediaTLSServer{
		config:  config,
		handler: handler,
		tls: &tls.Config{ //nolint:gosec // TLS 1.3 is the minimum and only policy choice here.
			MinVersion:   tls.VersionTLS13,
			MaxVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{pair},
			NextProtos:   []string{"http/1.1"},
		},
	}, nil
}

func validateBoxMediaListenAddr(raw string) (string, int, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(raw))
	if err != nil {
		return "", 0, fmt.Errorf("box media TLS: listen_addr must be an explicit IP and port: %w", err)
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil || address.Is4In6() || address.IsUnspecified() || address.IsMulticast() || !boxMediaLocalAddress(address) {
		return "", 0, errors.New("box media TLS: listen_addr must use one explicit local unicast IP")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("box media TLS: listen_addr port must be 1..65535")
	}
	return address.String(), port, nil
}

func boxMediaLocalAddress(address netip.Addr) bool {
	for _, prefix := range bridgeLocalPrefixes {
		if address.BitLen() == prefix.Addr().BitLen() && prefix.Contains(address) {
			return true
		}
	}
	return false
}

// Start binds synchronously so configuration, certificate, and port failures
// are returned before any caller can claim the endpoint is available.
func (s *BoxMediaTLSServer) Start(ctx context.Context) (*BoxMediaTLSRuntime, error) {
	if s == nil || s.handler == nil || s.tls == nil {
		return nil, errors.New("box media TLS: server is not initialized")
	}
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", s.config.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("box media TLS: listen: %w", err)
	}
	tlsListener := tls.NewListener(listener, s.tls.Clone())
	httpServer := &http.Server{
		Addr:              s.config.ListenAddr,
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	runtime := &BoxMediaTLSRuntime{
		server: httpServer, listener: tlsListener, errors: make(chan error, 1),
	}
	go func() {
		err := httpServer.Serve(tlsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtime.errors <- err
		}
		close(runtime.errors)
	}()
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = runtime.Shutdown(shutdownCtx)
		}()
	}
	return runtime, nil
}

func (r *BoxMediaTLSRuntime) Errors() <-chan error {
	if r == nil {
		return nil
	}
	return r.errors
}

func (r *BoxMediaTLSRuntime) Addr() net.Addr {
	if r == nil || r.listener == nil {
		return nil
	}
	return r.listener.Addr()
}

func (r *BoxMediaTLSRuntime) Shutdown(ctx context.Context) error {
	if r == nil || r.server == nil {
		return nil
	}
	var shutdownErr error
	r.once.Do(func() {
		shutdownErr = r.server.Shutdown(ctx)
		if closeErr := r.listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) && shutdownErr == nil {
			shutdownErr = closeErr
		}
	})
	return shutdownErr
}
