package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"math/big"
	"mime"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Device pairing request limits (speechkit.pairing.v2). A request lives
// RequestTTL; the operator approves it by the code the device displays.
const (
	RequestTTL           = 10 * time.Minute
	maxPendingRequests   = 16
	maxPendingPerSource  = 3
	maxCreatesPerSource  = 6
	createWindow         = 10 * time.Minute
	codeDigits           = 6
	maxDeviceNameRunes   = 64
	maxDeviceKindLength  = 32
	maxHardwareIDLength  = 64
	defaultDeviceKind    = "device"
	requestIDRandomBytes = 12
	pollSecretBytes      = 32
)

// RequestState is the lifecycle of one pairing request.
type RequestState string

const (
	RequestPending  RequestState = "pending"
	RequestApproved RequestState = "approved"
	RequestDenied   RequestState = "denied"
)

var (
	ErrRequestNotFound  = errors.New("pairing: request not found or expired")
	ErrTooManyRequests  = errors.New("pairing: too many pairing requests")
	ErrInvalidDevice    = errors.New("pairing: invalid device description")
	ErrRequestNotIssued = errors.New("pairing: request is not approved")
)

// DeviceRequest is one pending pairing. The poll secret is held only as a
// SHA-256 and is never part of this value.
type DeviceRequest struct {
	ReplacesDeviceID string       `json:"replaces_device_id,omitempty"`
	ID               string       `json:"id"`
	Code             string       `json:"code"`
	Name             string       `json:"device_name"`
	Kind             string       `json:"device_kind"`
	HardwareID       string       `json:"device_id,omitempty"`
	Source           string       `json:"source"`
	State            RequestState `json:"state"`
	CreatedAt        time.Time    `json:"created_at"`
	ExpiresAt        time.Time    `json:"expires_at"`
	secretHash       string
}

// RequestBook holds pending pairing requests in memory. A restart drops
// them; the device simply starts a new request.
type RequestBook struct {
	mu      sync.Mutex
	byID    map[string]*DeviceRequest
	creates map[string][]time.Time
}

func NewRequestBook() *RequestBook {
	return &RequestBook{byID: map[string]*DeviceRequest{}, creates: map[string][]time.Time{}}
}

// NormalizeDevice validates the device-supplied description.
func NormalizeDevice(name, kind, hardwareID string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxDeviceNameRunes {
		return "", "", "", ErrInvalidDevice
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return "", "", "", ErrInvalidDevice
		}
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = defaultDeviceKind
	}
	if len(kind) > maxDeviceKindLength || !tokenChars(kind, "-_") {
		return "", "", "", ErrInvalidDevice
	}
	hardwareID = strings.TrimSpace(hardwareID)
	if len(hardwareID) > maxHardwareIDLength || (hardwareID != "" && !tokenChars(hardwareID, "-_.:")) {
		return "", "", "", ErrInvalidDevice
	}
	return name, kind, hardwareID, nil
}

func tokenChars(s, extra string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(extra, r)) {
			return false
		}
	}
	return true
}

// NormalizeCode keeps only the digits an operator typed ("123 456",
// "123-456" and "123456" are the same code).
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func randomCode() (string, error) {
	limit := big.NewInt(1)
	for range codeDigits {
		limit.Mul(limit, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}
	s := n.String()
	return strings.Repeat("0", codeDigits-len(s)) + s, nil
}

func (b *RequestBook) sweepLocked(now time.Time) {
	for id, req := range b.byID {
		if !now.Before(req.ExpiresAt) {
			delete(b.byID, id)
		}
	}
	for src, times := range b.creates {
		kept := times[:0]
		for _, t := range times {
			if now.Sub(t) < createWindow {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(b.creates, src)
		} else {
			b.creates[src] = kept
		}
	}
}

// Create opens a pending request for a validated device and returns it with
// the poll secret (shown to the device once).
func (b *RequestBook) Create(name, kind, hardwareID, source string, now time.Time) (DeviceRequest, string, error) {
	name, kind, hardwareID, err := NormalizeDevice(name, kind, hardwareID)
	if err != nil {
		return DeviceRequest{}, "", err
	}
	id, err := randomString(requestIDRandomBytes)
	if err != nil {
		return DeviceRequest{}, "", err
	}
	secret, err := randomString(pollSecretBytes)
	if err != nil {
		return DeviceRequest{}, "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	if len(b.byID) >= maxPendingRequests || len(b.creates[source]) >= maxCreatesPerSource {
		return DeviceRequest{}, "", ErrTooManyRequests
	}
	fromSource := 0
	for _, req := range b.byID {
		if req.Source == source {
			fromSource++
		}
	}
	if fromSource >= maxPendingPerSource {
		return DeviceRequest{}, "", ErrTooManyRequests
	}
	code := ""
	for attempt := 0; attempt < 8 && code == ""; attempt++ {
		candidate, err := randomCode()
		if err != nil {
			return DeviceRequest{}, "", err
		}
		if b.byCodeLocked(candidate) == nil {
			code = candidate
		}
	}
	if code == "" {
		return DeviceRequest{}, "", ErrTooManyRequests
	}
	req := &DeviceRequest{
		ID:         "pr_" + id,
		Code:       code,
		Name:       name,
		Kind:       kind,
		HardwareID: hardwareID,
		Source:     source,
		State:      RequestPending,
		CreatedAt:  now.UTC().Truncate(time.Second),
		ExpiresAt:  now.Add(RequestTTL).UTC().Truncate(time.Second),
		secretHash: HashDeviceToken(secret),
	}
	b.byID[req.ID] = req
	b.creates[source] = append(b.creates[source], now)
	return *req, secret, nil
}

func (b *RequestBook) byCodeLocked(code string) *DeviceRequest {
	for _, req := range b.byID {
		if req.Code == code {
			return req
		}
	}
	return nil
}

// Pending lists open requests for the operator, oldest first.
func (b *RequestBook) Pending(now time.Time) []DeviceRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	out := make([]DeviceRequest, 0, len(b.byID))
	for _, req := range b.byID {
		if req.State == RequestPending {
			out = append(out, *req)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Decide approves or denies the pending request that shows code.
func (b *RequestBook) Decide(code string, approve bool, now time.Time) (DeviceRequest, error) {
	code = NormalizeCode(code)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	req := b.byCodeLocked(code)
	if code == "" || req == nil || req.State != RequestPending {
		return DeviceRequest{}, ErrRequestNotFound
	}
	if approve {
		req.State = RequestApproved
	} else {
		req.State = RequestDenied
	}
	return *req, nil
}

// Poll returns the request when the poll secret matches. A wrong secret,
// an unknown id and an expired request look the same.
func (b *RequestBook) Poll(id, secret string, now time.Time) (DeviceRequest, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	req, ok := b.byID[strings.TrimSpace(id)]
	if !ok || !secretMatches(req.secretHash, secret) {
		return DeviceRequest{}, ErrRequestNotFound
	}
	if req.State == RequestDenied {
		delete(b.byID, req.ID)
	}
	return *req, nil
}

// Take removes an approved request exactly once, so two concurrent polls
// cannot both mint a device token. Restore puts it back when issuing fails.
func (b *RequestBook) Take(id, secret string, now time.Time) (DeviceRequest, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	req, ok := b.byID[strings.TrimSpace(id)]
	if !ok || !secretMatches(req.secretHash, secret) {
		return DeviceRequest{}, ErrRequestNotFound
	}
	if req.State != RequestApproved {
		return DeviceRequest{}, ErrRequestNotIssued
	}
	delete(b.byID, req.ID)
	return *req, nil
}

// Restore re-inserts a taken request whose token could not be issued.
func (b *RequestBook) Restore(req DeviceRequest, secret string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	req.secretHash = HashDeviceToken(secret)
	b.byID[req.ID] = &req
}

func secretMatches(wantHash, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(wantHash), []byte(HashDeviceToken(secret))) == 1
}

// forwardingHeaders name the headers a proxy uses to state the original
// client. A request that carries any of them did not come straight from the
// LAN peer.
var forwardingHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "True-Client-IP"}

// IsLANAddress reports private (RFC 1918, RFC 4193 ULA), loopback and
// link-local addresses.
func IsLANAddress(ip net.IP) bool {
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

// DeviceRequestHeadersValid admits device JSON only, never browser-origin requests.
func DeviceRequestHeadersValid(r *http.Request) bool {
	if r == nil {
		return false
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Origin") {
			return false
		}
	}
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	return err == nil && mediaType == "application/json"
}

// LANSource returns the client address for a LAN request. A loopback peer
// requires an explicitly configured trusted proxy and a nonempty LAN-only
// Forwarded or X-Forwarded-For chain. The callback must check configured CIDRs
// without implicitly trusting loopback. Every supplied forwarding address must
// be LAN; a lone X-Real-IP never establishes a loopback client's origin.
func LANSource(r *http.Request, trustsProxy func(*http.Request) bool) (string, bool) {
	if r == nil {
		return "", false
	}
	peer := parseHostIP(r.RemoteAddr)
	if !IsLANAddress(peer) {
		return "", false
	}
	client := peer.String()
	forwarded := false
	chain := false
	for _, name := range forwardingHeaders {
		values := r.Header.Values(name)
		if len(values) == 0 {
			continue
		}
		forwarded = true
		addrs, ok := forwardedAddresses(name, values)
		if !ok || len(addrs) == 0 {
			return "", false
		}
		if name == "Forwarded" || name == "X-Forwarded-For" {
			chain = true
		}
		for i, addr := range addrs {
			if !IsLANAddress(addr) {
				return "", false
			}
			if i == 0 && (name == "X-Forwarded-For" || name == "Forwarded") {
				client = addr.String()
			}
		}
	}
	if peer.IsLoopback() && !chain {
		return "", false
	}
	if forwarded && (trustsProxy == nil || !trustsProxy(r)) {
		return "", false
	}
	return client, true
}

func forwardedAddresses(name string, values []string) ([]net.IP, bool) {
	var out []net.IP
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if name == "Forwarded" {
				found := false
				for _, pair := range strings.Split(part, ";") {
					k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
					if ok && strings.EqualFold(strings.TrimSpace(k), "for") {
						part = strings.Trim(strings.TrimSpace(v), `"`)
						found = true
					}
				}
				if !found {
					continue
				}
			}
			ip := parseHostIP(part)
			if ip == nil {
				return nil, false
			}
			out = append(out, ip)
		}
	}
	return out, true
}

func parseHostIP(raw string) net.IP {
	host := strings.TrimSpace(raw)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	return net.ParseIP(host)
}
