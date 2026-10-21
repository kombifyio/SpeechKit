package local

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = &url.URL{Scheme: "http", Host: upstreamAddr, Path: pr.In.URL.Path, RawPath: pr.In.URL.RawPath, RawQuery: pr.In.URL.RawQuery}
			pr.Out.Host = upstreamAddr
			// The key authenticates the caller to this proxy only; the child
			// never sees it.
			pr.Out.Header.Del("Authorization")
		},
		Transport: transport,
		ModifyResponse: func(resp *http.Response) error {
			for name := range resp.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
					resp.Header.Del(name)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "whisper-server unavailable", http.StatusBadGateway)
		},
	}

	p := &authProxy{key: key, upstream: transport, baseURL: "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port))}
	p.srv = &http.Server{
		Handler:           p.guard(rp),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
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
