//go:build linux

package core

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/app/internal/server/pairing"
)

// LAN device pairing (speechkit.pairing.v2, docs/server/pairing.md): a
// device on the local network asks for its own credential, shows a short
// code, the operator approves that code, and the device collects a
// revocable per-device token once. The shared server bearer never leaves
// the server.
const (
	devicePairingRequestsPath = "/v1/pairing/device-requests"
	devicePairingDevicesPath  = "/v1/pairing/devices"
	// DevicePairingSecretHeader carries the poll secret of one request.
	DevicePairingSecretHeader = "X-Pairing-Secret" //nolint:gosec // header name, not a credential
	devicePairingPollSeconds  = 3
	devicePairingBodyLimit    = 4 * 1024
)

// deviceTokenRoutes are the device-facing API families a paired device may
// call. Operator surfaces (settings, personas, admin, device registry,
// transcripts) stay closed to device tokens.
var deviceTokenRoutes = []string{"/v1/voiceagent", "/v1/dictation", "/v1/assist", "/v1/tts", "/v1/wakeword"}

type devicePairing struct {
	app      *App
	registry *pairing.DeviceRegistry
	requests *pairing.RequestBook
	proxies  httpx.TrustedProxies
	now      func() time.Time
}

func registerDevicePairing(app *App) {
	if app == nil || app.Mux == nil || app.Cfg == nil {
		return
	}
	path := config.PairedDevicesPath(app.Cfg)
	registry, err := pairing.OpenDeviceRegistry(path)
	if err != nil {
		// Fail closed: without its registry no device token is accepted
		// and no new one is issued.
		slog.Warn("device pairing unavailable", "path", path, "err", err)
		return
	}
	proxies, _ := httpx.NewTrustedProxies(app.Cfg.Server.TrustedProxyCIDRs)
	p := &devicePairing{app: app, registry: registry, requests: pairing.NewRequestBook(), proxies: proxies, now: time.Now}
	app.DevicePairing = p
	app.Mux.HandleFunc(devicePairingRequestsPath, p.serveRequests)
	app.Mux.HandleFunc(devicePairingRequestsPath+"/", p.serveRequest)
	app.Mux.HandleFunc(devicePairingDevicesPath, p.serveDevices)
	app.Mux.HandleFunc(devicePairingDevicesPath+"/", p.serveDevice)
	slog.Info("device pairing ready", "devices", len(registry.List()), "device_requests", p.enabled())
}

// devicePairingPublicRoutes are reachable without credentials: creating a
// request (LAN source, enabled, rate-limited in the handler) and polling one
// (poll secret checked in the handler). Listing, approval and the device
// registry stay behind the normal auth chain.
func devicePairingPublicRoutes() []middleware.PublicRoute {
	return []middleware.PublicRoute{
		{Path: devicePairingRequestsPath, Methods: []string{http.MethodPost}},
		{PathPrefix: devicePairingRequestsPath + "/", Methods: []string{http.MethodGet}},
	}
}

func (p *devicePairing) enabled() bool {
	var mode string
	if p.app.AuthState != nil {
		mode = p.app.AuthState.Mode()
	} else {
		mode = p.app.Cfg.Server.AuthMode
	}
	return config.DeviceRequestsEnabled(p.app.Cfg, mode)
}

// verify admits a device token on the device-facing routes only. The
// identity carries no role: a paired device is a client, never an operator.
func (p *devicePairing) verify(r *http.Request) (middleware.Identity, bool) {
	if p == nil || r == nil || !deviceTokenRoute(r.URL.Path) {
		return middleware.Identity{}, false
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return middleware.Identity{}, false
	}
	dev, ok := p.registry.Verify(strings.TrimSpace(strings.TrimPrefix(header, prefix)), p.now())
	if !ok {
		return middleware.Identity{}, false
	}
	return middleware.Identity{
		UserID:   "device:" + dev.ID,
		OrgID:    "default",
		Plan:     "internal",
		DeviceID: dev.ID,
		Source:   "device_token",
	}, true
}

func deviceTokenRoute(path string) bool {
	path = strings.TrimPrefix(path, "/api")
	for _, prefix := range deviceTokenRoutes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// pairingOperator is the identity that may approve, list and revoke: the
// admin session or role, or the holder of the server bearer (who already
// has every right a device token grants).
func pairingOperator(r *http.Request) bool {
	id := middleware.IdentityFromContext(r.Context())
	switch id.Source {
	case "bearer", "admin_session", "basic":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(id.Role), "admin")
}

func writePairingJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writePairingError(w http.ResponseWriter, status int, code, message string) {
	writePairingJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func (p *devicePairing) requireOperator(w http.ResponseWriter, r *http.Request) bool {
	if !pairingOperator(r) {
		writePairingError(w, http.StatusForbidden, "operator_required", "device pairing is managed by the server operator")
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !middleware.EnforceAdminCSRF(w, r) {
		return false
	}
	return true
}

// serveRequests: POST creates a request (device), GET lists pending ones
// (operator).
func (p *devicePairing) serveRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		p.createRequest(w, r)
	case http.MethodGet:
		if !p.requireOperator(w, r) {
			return
		}
		writePairingJSON(w, http.StatusOK, map[string]any{
			"device_requests": p.pendingRequests(),
			"enabled":         p.enabled(),
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		writePairingError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET or POST")
	}
}

// pendingRequests marks self-asserted hardware ids that would replace a device.
func (p *devicePairing) pendingRequests() []pairing.DeviceRequest {
	requests := p.requests.Pending(p.now())
	devices := p.registry.List()
	for i := range requests {
		markReplacement(&requests[i], devices)
	}
	return requests
}

func markReplacement(req *pairing.DeviceRequest, devices []pairing.Device) {
	for _, dev := range devices {
		if req.HardwareID != "" && req.HardwareID == dev.HardwareID {
			req.ReplacesDeviceID = dev.ID
			return
		}
	}
}

func (p *devicePairing) createRequest(w http.ResponseWriter, r *http.Request) {
	if !p.enabled() {
		writePairingError(w, http.StatusForbidden, "device_pairing_disabled", "device pairing is off on this server ([server.pairing] device_requests)")
		return
	}
	source, ok := pairing.LANSource(r, p.proxies.ExplicitlyTrustsRequest)
	if !ok {
		writePairingError(w, http.StatusForbidden, "not_local_network", "device pairing is accepted only from the local network")
		return
	}
	if !pairing.DeviceRequestHeadersValid(r) {
		writePairingError(w, http.StatusBadRequest, "invalid_request", "expected application/json without an Origin header")
		return
	}
	var body struct {
		DeviceName string `json:"device_name"`
		DeviceKind string `json:"device_kind"`
		DeviceID   string `json:"device_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, devicePairingBodyLimit)).Decode(&body); err != nil {
		writePairingError(w, http.StatusBadRequest, "invalid_request", "expected JSON {device_name, device_kind, device_id}")
		return
	}
	req, secret, err := p.requests.Create(body.DeviceName, body.DeviceKind, body.DeviceID, source, p.now())
	switch {
	case errors.Is(err, pairing.ErrInvalidDevice):
		writePairingError(w, http.StatusBadRequest, "invalid_device", "device_name (1-64 printable characters), device_kind ([a-z0-9_-], max 32) and device_id ([A-Za-z0-9._:-], max 64) are required in that form")
		return
	case errors.Is(err, pairing.ErrTooManyRequests):
		w.Header().Set("Retry-After", "60")
		writePairingError(w, http.StatusTooManyRequests, "too_many_pairing_requests", "too many open pairing requests; wait or let the operator decide the open ones")
		return
	case err != nil:
		writePairingError(w, http.StatusInternalServerError, "pairing_failed", "could not open a pairing request")
		return
	}
	slog.Info("device pairing requested", "request_id", req.ID,
		"device_name", req.Name, "device_kind", req.Kind, "source", req.Source)
	writePairingJSON(w, http.StatusCreated, map[string]any{
		"request_id":    req.ID,
		"poll_secret":   secret,
		"code":          req.Code,
		"expires_in":    int(time.Until(req.ExpiresAt).Seconds()),
		"poll_interval": devicePairingPollSeconds,
	})
}

// serveRequest: GET /{id} polls (device, poll secret); POST /approve and
// POST /deny decide by code (operator).
func (p *devicePairing) serveRequest(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, devicePairingRequestsPath+"/"), "/")
	switch {
	case r.Method == http.MethodPost && (rest == "approve" || rest == "deny"):
		if !p.requireOperator(w, r) {
			return
		}
		p.decide(w, r, rest == "approve")
	case r.Method == http.MethodGet && rest != "" && !strings.Contains(rest, "/"):
		p.poll(w, r, rest)
	default:
		writePairingError(w, http.StatusNotFound, "not_found", "unknown pairing route")
	}
}

func (p *devicePairing) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, devicePairingBodyLimit)).Decode(&body); err != nil {
		writePairingError(w, http.StatusBadRequest, "invalid_request", "expected JSON {code}")
		return
	}
	req, err := p.requests.Decide(body.Code, approve, p.now())
	if err != nil {
		writePairingError(w, http.StatusNotFound, "code_not_found", "no open pairing request shows this code (expired, already decided or mistyped)")
		return
	}
	markReplacement(&req, p.registry.List())
	actor := middleware.IdentityFromContext(r.Context()).Source
	slog.Info("device pairing decided", "request_id", req.ID, "approved", approve,
		"device_name", req.Name, "source", req.Source, "by", actor)
	writePairingJSON(w, http.StatusOK, map[string]any{"device_request": req})
}

func (p *devicePairing) poll(w http.ResponseWriter, r *http.Request, id string) {
	secret := r.Header.Get(DevicePairingSecretHeader)
	now := p.now()
	req, err := p.requests.Poll(id, secret, now)
	if err != nil {
		writePairingJSON(w, http.StatusNotFound, map[string]any{"status": "expired"})
		return
	}
	switch req.State {
	case pairing.RequestPending:
		writePairingJSON(w, http.StatusOK, map[string]any{
			"status":        "pending",
			"code":          req.Code,
			"expires_in":    int(req.ExpiresAt.Sub(now).Seconds()),
			"poll_interval": devicePairingPollSeconds,
		})
		return
	case pairing.RequestDenied:
		writePairingJSON(w, http.StatusOK, map[string]any{"status": "denied"})
		return
	}
	taken, err := p.requests.Take(id, secret, now)
	if err != nil {
		// Another poll already collected this token.
		writePairingJSON(w, http.StatusNotFound, map[string]any{"status": "expired"})
		return
	}
	dev, token, err := p.registry.Issue(taken.Name, taken.Kind, taken.HardwareID, taken.Source, now)
	if err != nil {
		p.requests.Restore(taken, secret)
		slog.Error("device pairing: could not store the device", "request_id", taken.ID, "err", err)
		writePairingError(w, http.StatusInternalServerError, "device_store_failed", "the server could not store the device; poll again")
		return
	}
	slog.Info("device paired", "device_id", dev.ID, "device_name", dev.Name, "device_kind", dev.Kind, "source", dev.PairedFrom)
	writePairingJSON(w, http.StatusOK, map[string]any{
		"status":      "approved",
		"device_id":   dev.ID,
		"device_name": dev.Name,
		"token_type":  "bearer",
		"token":       token,
	})
}

// serveDevices: GET lists paired devices (operator).
func (p *devicePairing) serveDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writePairingError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	if !p.requireOperator(w, r) {
		return
	}
	writePairingJSON(w, http.StatusOK, map[string]any{"devices": p.registry.List()})
}

// serveDevice: DELETE /{id} revokes one device (operator). The device's
// next request is refused at once.
func (p *devicePairing) serveDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, devicePairingDevicesPath+"/"), "/")
	if r.Method != http.MethodDelete || id == "" || strings.Contains(id, "/") {
		w.Header().Set("Allow", "DELETE")
		writePairingError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use DELETE /v1/pairing/devices/{id}")
		return
	}
	if !p.requireOperator(w, r) {
		return
	}
	dev, err := p.registry.Revoke(id)
	if errors.Is(err, pairing.ErrDeviceNotFound) {
		writePairingError(w, http.StatusNotFound, "device_not_found", "no paired device has this id")
		return
	}
	if err != nil {
		slog.Error("device revoked in memory but not persisted", "device_id", dev.ID, "err", err)
		writePairingError(w, http.StatusInternalServerError, "device_store_failed", "revoked until restart; the registry file could not be written")
		return
	}
	slog.Info("device revoked", "device_id", dev.ID, "device_name", dev.Name)
	writePairingJSON(w, http.StatusOK, map[string]any{"revoked": dev})
}
