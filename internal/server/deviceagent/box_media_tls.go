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
	server         *http.Server
	listener       net.Listener
	errors         chan error
	serveDone      chan struct{}
	cancelRequests context.CancelFunc
	requests       boxMediaRequestLifecycle
	once           sync.Once
	shutdownErr    error
}

type boxMediaRequestLifecycle struct {
	mu       sync.Mutex
	draining bool
	active   sync.WaitGroup
}

func (l *boxMediaRequestLifecycle) begin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.draining {
		return false
	}
	l.active.Add(1)
	return true
}

func (l *boxMediaRequestLifecycle) end() {
	l.active.Done()
}

func (l *boxMediaRequestLifecycle) stopAccepting() {
	l.mu.Lock()
	l.draining = true
	l.mu.Unlock()
}

func (l *boxMediaRequestLifecycle) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		l.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CertificateChainDER returns a defensive snapshot of the exact certificate
// chain loaded into the dedicated TLS listener. Lifecycle policy can therefore
// bind the served leaf to the Box-pinned CA without reopening the certificate
// file and introducing a verify-then-load race.
func (s *BoxMediaTLSServer) CertificateChainDER() [][]byte {
	if s == nil || s.tls == nil || len(s.tls.Certificates) != 1 {
		return nil
	}
	chain := s.tls.Certificates[0].Certificate
	snapshot := make([][]byte, len(chain))
	for index, certificate := range chain {
		snapshot[index] = append([]byte(nil), certificate...)
	}
	return snapshot
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
	if err != nil || !boxMediaRFC1918Address(address) {
		return "", 0, errors.New("box media TLS: listen_addr must use one explicit RFC1918 IPv4 address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("box media TLS: listen_addr port must be 1..65535")
	}
	return address.String(), port, nil
}

var boxMediaRFC1918Prefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

func boxMediaRFC1918Address(address netip.Addr) bool {
	if !address.Is4() {
		return false
	}
	for _, prefix := range boxMediaRFC1918Prefixes {
		if address.BitLen() == prefix.Addr().BitLen() && prefix.Contains(address) {
			return true
		}
	}
	return false
}

func boxMediaRFC1918CIDR(raw string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() {
		return false
	}
	for _, allowed := range boxMediaRFC1918Prefixes {
		if prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Addr()) {
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
	if ctx == nil {
		return nil, errors.New("box media TLS: process context is required")
	}
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", s.config.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("box media TLS: listen: %w", err)
	}
	tlsListener := tls.NewListener(listener, s.tls.Clone())
	// #nosec G118 -- cancelRequests is stored on the runtime and invoked during
	// shutdown (see r.cancelRequests()); gosec cannot follow a cancel func that
	// outlives its function scope by design.
	requestContext, cancelRequests := context.WithCancel(ctx)
	runtime := &BoxMediaTLSRuntime{
		listener: tlsListener, errors: make(chan error, 1), serveDone: make(chan struct{}),
		cancelRequests: cancelRequests,
	}
	trackedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !runtime.requests.begin() {
			http.Error(w, "Box media listener is shutting down", http.StatusServiceUnavailable)
			return
		}
		defer runtime.requests.end()
		s.handler.ServeHTTP(w, r)
	})
	httpServer := &http.Server{
		Addr:    s.config.ListenAddr,
		Handler: trackedHandler,
		BaseContext: func(net.Listener) context.Context {
			return requestContext
		},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      boxMediaWriteTimeout,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	runtime.server = httpServer
	go func() {
		defer close(runtime.serveDone)
		defer close(runtime.errors)
		err := httpServer.Serve(tlsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtime.errors <- err
		}
	}()
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
	r.once.Do(func() {
		// This is the sole shutdown owner. Cancel every request context before
		// draining so STT, Home Assistant dispatch/readback, and TTS cannot
		// outlive the dependencies that core tears down after this returns.
		r.requests.stopAccepting()
		if r.cancelRequests != nil {
			r.cancelRequests()
		}
		r.shutdownErr = r.server.Shutdown(ctx)
		if r.shutdownErr != nil {
			// Shutdown stops accepting new requests but a context deadline may
			// leave active connections behind. Force-close them, then still wait
			// for the tracked handlers below before allowing dependency teardown.
			_ = r.server.Close()
		}
		if closeErr := r.listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) && r.shutdownErr == nil {
			r.shutdownErr = closeErr
		}
		if waitErr := r.requests.wait(ctx); waitErr != nil && r.shutdownErr == nil {
			r.shutdownErr = waitErr
		}
		if r.serveDone != nil {
			select {
			case <-r.serveDone:
			case <-ctx.Done():
				if r.shutdownErr == nil {
					r.shutdownErr = ctx.Err()
				}
			}
		}
	})
	return r.shutdownErr
}
