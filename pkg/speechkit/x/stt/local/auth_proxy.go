package local

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrAuthProxyUnavailable is returned when the keyed loopback proxy in front
// of whisper-server cannot be set up, or is gone while the child still runs.
// The provider fails closed on it: whisper-server itself has no
// authentication and serves open CORS, so it is never reached directly.
var ErrAuthProxyUnavailable = errors.New("local whisper-server: authenticated loopback proxy unavailable")

// authProxy is the only door to a whisper-server child. whisper.cpp has no
// API key flag (and answers every origin with permissive CORS), so the child
// listens on a private ephemeral loopback port and this proxy listens on the
// provider's port: it requires the per-start key as a bearer token, refuses
// browser-origin and foreign-host requests, and strips CORS headers from
// responses.
type authProxy struct {
	key      string
	baseURL  string
	srv      *http.Server
	upstream *http.Transport
	once     sync.Once
}

func newProxyKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("%w: key generation: %w", ErrAuthProxyUnavailable, err)
	}
	return hex.EncodeToString(b[:]), nil
}

// startAuthProxy listens on 127.0.0.1:listenPort (0 picks a free port) and
// forwards authorised requests to the child on 127.0.0.1:upstreamPort.
func startAuthProxy(ctx context.Context, listenPort, upstreamPort int) (*authProxy, error) {
	key, err := newProxyKey()
	if err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort)))
	if err != nil {
		return nil, fmt.Errorf("%w: listen: %w", ErrAuthProxyUnavailable, err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, fmt.Errorf("%w: unexpected listener address %v", ErrAuthProxyUnavailable, ln.Addr())
	}

	upstreamAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(upstreamPort))
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, upstreamAddr)
		},
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		// A redirect from the child is never followed: the answer goes back to
		// the caller unchanged and the caller decides.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	p := &authProxy{key: key, upstream: transport, baseURL: "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port))}
	p.srv = &http.Server{
		Handler:           p.guard(forwardToChild(client, upstreamAddr)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// proxyMaxResponseBytes bounds one relayed answer. The child answers with small
// JSON documents; anything larger is a fault, not a transcript.
const proxyMaxResponseBytes = 16 << 20

var (
	proxyIdentityPath = regexp.MustCompile(`^/[0-9a-f]{32}\.txt$`)
	// The stream routes of speechkit-stt-sherpa's five-value engine.
	proxyStreamPath     = regexp.MustCompile(`^/stream/[0-9a-f]{32}$`)
	proxyStreamCallPath = regexp.MustCompile(`^/stream/[0-9a-f]{32}/(audio|finish)$`)
	proxyHopHeaders     = map[string]bool{
		"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true,
		"Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
	}
)

// proxyRouteAllowed names the only calls the provider makes to its child:
// the health probe, the per-start identity proof, one transcription, and the
// calls of a live dictation stream. The
// child's other routes (such as whisper-server's model loader) stay
// unreachable, and the child's file directory is never exposed beyond the
// identity proof.
func proxyRouteAllowed(method, path string) bool {
	switch {
	case method == http.MethodGet && path == "/health":
		return true
	case method == http.MethodGet && proxyIdentityPath.MatchString(path):
		return true
	case method == http.MethodPost && (path == "/v1/audio/transcriptions" || path == "/inference" || path == "/stream"):
		return true
	case method == http.MethodPost && proxyStreamCallPath.MatchString(path):
		return true
	case method == http.MethodDelete && proxyStreamPath.MatchString(path):
		return true
	}
	return false
}

// forwardToChild relays one allowed call to the child on loopback and returns
// its bounded answer. It buffers the answer instead of streaming it, so no
// file-sized body can pass through.
func forwardToChild(client *http.Client, upstreamAddr string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !proxyRouteAllowed(r.Method, r.URL.Path) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		target := "http://" + upstreamAddr + r.URL.Path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		out, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		if err != nil {
			http.Error(w, "whisper-server unavailable", http.StatusBadGateway)
			return
		}
		out.ContentLength = r.ContentLength
		for _, name := range []string{"Content-Type", "Accept"} {
			if value := r.Header.Get(name); value != "" {
				out.Header.Set(name, value)
			}
		}
		resp, err := client.Do(out)
		if err != nil {
			http.Error(w, "whisper-server unavailable", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		answer, err := io.ReadAll(io.LimitReader(resp.Body, proxyMaxResponseBytes+1))
		if err != nil || len(answer) > proxyMaxResponseBytes {
			http.Error(w, "whisper-server answer unusable", http.StatusBadGateway)
			return
		}
		for name, values := range resp.Header {
			canonical := http.CanonicalHeaderKey(name)
			// The child answers every origin; that never passes through.
			if proxyHopHeaders[canonical] || canonical == "Content-Length" || strings.HasPrefix(canonical, "Access-Control-") {
				continue
			}
			for _, value := range values {
				w.Header().Add(canonical, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(answer)
	})
}

// guard admits only requests that carry the per-start key and come from a
// non-browser, loopback-addressed caller.
func (p *authProxy) guard(next http.Handler) http.Handler {
	want := []byte("Bearer " + p.key)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A browser page cannot set Authorization cross-origin without a
		// preflight we never grant; refusing any Origin also closes
		// DNS-rebinding and simple cross-site POSTs.
		if r.Header.Get("Origin") != "" || !loopbackHost(r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (p *authProxy) close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		_ = p.srv.Close()
		p.upstream.CloseIdleConnections()
	})
}

// keyedTransport adds the per-start proxy key to every request the provider
// sends. While a child is owned but no proxy is published it refuses to send
// anything, so a missing proxy can never degrade to an unauthenticated direct
// call.
type keyedTransport struct {
	base http.RoundTripper
	p    *Provider
}

func (t *keyedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.p.processMu.Lock()
	proxy := t.p.proxy
	owned := t.p.cmd != nil
	t.p.processMu.Unlock()
	switch {
	case proxy != nil:
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+proxy.key)
	case owned:
		return nil, ErrAuthProxyUnavailable
	}
	return t.base.RoundTrip(req)
}

// keyed wraps a client so its requests carry the proxy key.
func (p *Provider) keyed(c *http.Client) *http.Client {
	// A client without a transport is a bare injected one: it cannot carry the
	// key, so a real child answers it 401 (fail closed).
	if c == nil || c.Transport == nil {
		return c
	}
	if kt, ok := c.Transport.(*keyedTransport); ok && kt.p == p {
		return c
	}
	c.Transport = &keyedTransport{base: c.Transport, p: p}
	return c
}
