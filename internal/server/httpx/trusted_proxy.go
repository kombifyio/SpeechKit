//go:build linux

package httpx

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// TrustedProxies decides whether request Forwarded/X-Forwarded-* headers may
// influence security-sensitive behavior.
type TrustedProxies struct {
	cidrs []*net.IPNet
}

func NewTrustedProxies(raw []string) (TrustedProxies, error) {
	out := TrustedProxies{}
	for _, value := range raw {
		cidr := strings.TrimSpace(value)
		if cidr == "" {
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return TrustedProxies{}, fmt.Errorf("trusted proxy CIDR %q: %w", cidr, err)
		}
		out.cidrs = append(out.cidrs, network)
	}
	return out, nil
}

// RequestIsHTTPS reports whether the request is HTTPS directly or through a
// trusted proxy that supplied X-Forwarded-Proto=https.
func (t TrustedProxies) RequestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return false
	}
	return t.TrustsRequest(r)
}

func (t TrustedProxies) TrustsRequest(r *http.Request) bool {
	ip := remoteIP(r)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, cidr := range t.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if host == "" {
		return nil
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.Trim(host, "[]")
	return net.ParseIP(host)
}
