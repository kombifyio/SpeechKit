package cloudlink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/server/discovery"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const (
	checkInSchema      = "kombify.edge_device.checkin.v1"
	defaultPollAfter   = 60 * time.Second
	minPollAfter       = 10 * time.Second
	maxPollAfter       = time.Hour
	maxErrorBackoff    = 15 * time.Minute
	httpTimeout        = 20 * time.Second
	maxCheckInResponse = 64 << 10
)

// Link states reported by Status.
const (
	StateDisabled       = "disabled"
	StateUnavailable    = "unavailable"
	StateUnlinked       = "unlinked"
	StatePending        = "pending"
	StateLinked         = "linked"
	StateRelinkRequired = "relink_required"
)

// Report is what one check-in tells the registry about this server.
type Report struct {
	DisplayName string
	Version     string
	LocalIP     string
	LANURL      string
	Modes       []string
	STT         string
	TTS         string
	LLM         string
}

// Status is the operator view of the link. It never carries a token.
type Status struct {
	Enabled          bool                 `json:"enabled"`
	State            string               `json:"state"`
	Reason           string               `json:"reason,omitempty"`
	DeviceID         string               `json:"device_id,omitempty"`
	LinkedAt         *time.Time           `json:"linked_at,omitempty"`
	Pending          *DeviceAuthorization `json:"pending,omitempty"`
	LastCheckInAt    *time.Time           `json:"last_check_in_at,omitempty"`
	LastError        string               `json:"last_error,omitempty"`
	PollAfterSeconds int                  `json:"poll_after_seconds,omitempty"`
}

// Manager owns one server's link: the device grant, the credential, the
// access token in memory and the check-in loop.
type Manager struct {
	enabled    bool
	settings   Settings
	resolveErr error
	report     func() Report
	version    string
	oauth      *oauthClient
	http       *http.Client

	// opMu serializes the credential and the access token (check-in,
	// refresh, unlink); mu guards the reported state, so Status never waits
	// for a network call.
	opMu        sync.Mutex
	mu          sync.Mutex
	pending     *DeviceAuthorization
	cancelFlow  context.CancelFunc
	access      string
	accessUntil time.Time
	// accessLink names the stored link the cached access token was issued
	// for; the CLI can unlink and relink another account beside the running
	// server, and a token of the previous account must never be reused.
	accessLink  string
	lastCheckIn time.Time
	lastError   string
	pollAfter   time.Duration
	wake        chan struct{}
	lifetime    context.Context
}

// NewManager builds the link for resolved settings; resolveErr is what
// Resolve returned (ErrDisabled when the section is off). report supplies the
// current check-in content.
func NewManager(settings Settings, resolveErr error, version string, report func() Report) *Manager {
	client := netsec.NewClient(netsec.ClientOptions{Timeout: httpTimeout})
	return &Manager{
		enabled:    !errors.Is(resolveErr, ErrDisabled),
		settings:   settings,
		resolveErr: resolveErr,
		report:     report,
		version:    strings.TrimSpace(version),
		oauth:      &oauthClient{settings: settings, http: client},
		http:       client,
		pollAfter:  defaultPollAfter,
		wake:       make(chan struct{}, 1),
	}
}

// Status reads the link state. It never calls out.
func (m *Manager) Status() Status {
	st := Status{Enabled: m.enabled}
	if m.resolveErr != nil {
		st.State = StateUnavailable
		if errors.Is(m.resolveErr, ErrDisabled) {
			st.State = StateDisabled
		}
		st.Reason = m.resolveErr.Error()
		return st
	}
	if id, err := discovery.LoadInstanceID(m.settings.StateDir); err == nil {
		st.DeviceID = id
	}
	cred, err := loadCredential(m.settings.StateDir)
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case err != nil:
		st.State = StateRelinkRequired
		st.Reason = err.Error()
	case cred == nil && m.pending != nil:
		st.State = StatePending
		p := *m.pending
		st.Pending = &p
	case cred == nil:
		st.State = StateUnlinked
	default:
		linked := cred.LinkedAt
		st.LinkedAt = &linked
		st.DeviceID = cred.DeviceID
		st.State = StateLinked
		if reason := m.credentialMismatch(cred); reason != "" {
			st.State = StateRelinkRequired
			st.Reason = reason
		}
	}
	if !m.lastCheckIn.IsZero() {
		at := m.lastCheckIn
		st.LastCheckInAt = &at
	}
	st.LastError = m.lastError
	if st.State == StateLinked {
		st.PollAfterSeconds = int(m.pollAfter / time.Second)
	}
	return st
}

func (m *Manager) credentialMismatch(cred *credential) string {
	switch {
	case cred.RelinkRequired:
		return "the identity provider no longer accepts the stored credential; unlink and link this server again"
	case cred.Issuer != m.settings.Issuer || cred.ClientID != m.settings.ClientID || cred.Audience != m.settings.Audience:
		return "the stored link belongs to a different [server.cloud_link] issuer, client_id or audience; unlink and link again"
	}
	return ""
}

// Begin starts a device authorization. It refuses while a link exists.
func (m *Manager) Begin(ctx context.Context) (*DeviceAuthorization, error) {
	if m.resolveErr != nil {
		return nil, m.resolveErr
	}
	if _, err := discovery.EnsureInstanceID(m.settings.StateDir); err != nil {
		return nil, err
	}
	cred, err := loadCredential(m.settings.StateDir)
	if err != nil {
		return nil, err
	}
	if cred != nil {
		return nil, errors.New("this server is already linked; unlink it first")
	}
	da, err := m.oauth.startDeviceAuthorization(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.pending = da
	m.mu.Unlock()
	return da, nil
}

// Complete waits for the person's approval of da and stores the credential.
func (m *Manager) Complete(ctx context.Context, da *DeviceAuthorization) error {
	tokens, err := m.oauth.awaitApproval(ctx, da)
	m.mu.Lock()
	if m.pending == da {
		m.pending = nil
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	id, err := discovery.EnsureInstanceID(m.settings.StateDir)
	if err != nil {
		return err
	}
	cred := &credential{
		DeviceID:     id,
		Issuer:       m.settings.Issuer,
		ClientID:     m.settings.ClientID,
		Audience:     m.settings.Audience,
		RefreshToken: tokens.RefreshToken,
		LinkedAt:     time.Now().UTC().Truncate(time.Second),
	}
	if err := saveCredential(m.settings.StateDir, cred); err != nil {
		return err
	}
	m.access, m.accessUntil, m.accessLink = tokens.AccessToken, tokens.ExpiresAt, linkIdentity(cred)
	m.mu.Lock()
	m.lastError = ""
	m.mu.Unlock()
	m.signal()
	return nil
}

// StartInBackground begins a link with the request context and waits for
// approval in a goroutine bound to the server lifetime that Run received. It
// returns the codes to show.
func (m *Manager) StartInBackground(ctx context.Context) (*DeviceAuthorization, error) {
	da, err := m.Begin(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	lifetime := m.lifetime
	m.mu.Unlock()
	if lifetime == nil {
		lifetime = context.Background()
	}
	flowCtx, cancel := context.WithDeadline(lifetime, da.ExpiresAt.Add(time.Minute))
	m.mu.Lock()
	if m.cancelFlow != nil {
		m.cancelFlow()
	}
	m.cancelFlow = cancel
	m.mu.Unlock()
	go func() {
		defer cancel()
		if err := m.Complete(flowCtx, da); err != nil {
			if !errors.Is(err, context.Canceled) {
				m.setError(err)
				slog.Warn("cloud link: approval not completed", "err", err)
			}
			return
		}
		slog.Info("cloud link: server linked")
	}()
	return da, nil
}

// CheckIn sends one check-in and returns the registry's next poll hint.
func (m *Manager) CheckIn(ctx context.Context) (time.Duration, error) {
	if m.resolveErr != nil {
		return 0, m.resolveErr
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	cred, err := loadCredential(m.settings.StateDir)
	if err != nil {
		return 0, err
	}
	if cred == nil {
		return 0, errNotLinked
	}
	if reason := m.credentialMismatch(cred); reason != "" {
		return 0, errors.New(reason)
	}
	body, err := json.Marshal(m.checkInBody())
	if err != nil {
		return 0, err
	}
	resp, err := m.authorized(ctx, cred, http.MethodPut, m.deviceURL(cred.DeviceID), body)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, maxCheckInResponse))
	if resp.StatusCode != http.StatusOK {
		return 0, registryError(resp.StatusCode, payload)
	}
	var answer struct {
		PollAfterSeconds int `json:"poll_after_seconds"`
	}
	_ = json.Unmarshal(payload, &answer)
	poll := defaultPollAfter
	if answer.PollAfterSeconds > 0 {
		poll = time.Duration(answer.PollAfterSeconds) * time.Second
	}
	poll = min(max(poll, minPollAfter), maxPollAfter)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pollAfter = poll
	m.lastCheckIn = time.Now().UTC().Truncate(time.Second)
	m.lastError = ""
	return poll, nil
}

var errNotLinked = errors.New("this server is not linked")

// UnlinkResult says what an unlink achieved beyond removing the credential.
type UnlinkResult struct {
	WasLinked       bool   `json:"was_linked"`
	RegistryRemoved bool   `json:"registry_removed"`
	TokenRevoked    bool   `json:"token_revoked"`
	Warning         string `json:"warning,omitempty"`
}

// Unlink removes this server from the account's registry, revokes the
// refresh token where the issuer allows it, and deletes the credential. The
// local credential goes even when a remote step fails; Warning names it.
func (m *Manager) Unlink(ctx context.Context) (UnlinkResult, error) {
	var res UnlinkResult
	m.mu.Lock()
	if m.cancelFlow != nil {
		m.cancelFlow()
		m.cancelFlow = nil
	}
	m.pending = nil
	m.mu.Unlock()
	m.opMu.Lock()
	defer m.opMu.Unlock()
	dir := m.settings.StateDir
	if dir == "" {
		return res, m.resolveErr
	}
	cred, loadErr := loadCredential(dir)
	if cred == nil {
		if err := deleteCredential(dir); err != nil {
			return res, err
		}
		res.WasLinked = loadErr != nil
		return res, nil
	}
	res.WasLinked = true
	var warnings []string
	if m.resolveErr != nil {
		warnings = append(warnings, "registry entry not removed: "+m.resolveErr.Error())
	} else if !cred.RelinkRequired {
		resp, err := m.authorized(ctx, cred, http.MethodDelete, m.deviceURL(cred.DeviceID), nil)
		switch {
		case err != nil:
			warnings = append(warnings, "registry entry not removed: "+err.Error())
		default:
			payload, _ := io.ReadAll(io.LimitReader(resp.Body, maxCheckInResponse))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
				res.RegistryRemoved = true
			} else {
				warnings = append(warnings, "registry entry not removed: "+registryError(resp.StatusCode, payload).Error())
			}
		}
		if ok, err := m.oauth.revoke(ctx, cred.RefreshToken); err != nil {
			warnings = append(warnings, "refresh token not revoked: "+err.Error())
		} else {
			res.TokenRevoked = ok
		}
	}
	if err := deleteCredential(dir); err != nil {
		return res, err
	}
	m.access, m.accessUntil, m.accessLink = "", time.Time{}, ""
	m.mu.Lock()
	m.lastCheckIn, m.lastError = time.Time{}, ""
	m.mu.Unlock()
	if len(warnings) > 0 {
		res.Warning = strings.Join(warnings, "; ") + "; remove the server from the account's device list if it is still shown"
	}
	return res, nil
}

// Run checks in while a link exists, until ctx ends. Without a link it only
// re-reads the local credential file, so a link made with the CLI is picked
// up without a restart; nothing calls out.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.lifetime = ctx
	m.mu.Unlock()
	if m.resolveErr != nil {
		return
	}
	backoff := defaultPollAfter
	for {
		wait := defaultPollAfter
		poll, err := m.CheckIn(ctx)
		switch {
		case err == nil:
			wait, backoff = poll, defaultPollAfter
		case errors.Is(err, errNotLinked), ctx.Err() != nil:
		default:
			m.setError(err)
			slog.Warn("cloud link: check-in failed", "err", err, "retry_in", backoff.String())
			wait = backoff
			backoff = min(backoff*2, maxErrorBackoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-time.After(wait):
		}
	}
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) setError(err error) {
	m.mu.Lock()
	m.lastError = err.Error()
	m.mu.Unlock()
}

func (m *Manager) deviceURL(id string) string {
	return m.settings.GatewayURL + "/v1/connect/edge-devices/" + id
}

// checkInBody is the kombify.edge_device.checkin.v1 document.
func (m *Manager) checkInBody() map[string]any {
	r := Report{}
	if m.report != nil {
		r = m.report()
	}
	version := firstNonEmpty(r.Version, m.version, "dev")
	if len(version) > 32 {
		version = version[:32]
	}
	reported := map[string]any{"execution": "local"}
	modes := r.Modes
	if modes == nil {
		modes = []string{}
	}
	reported["modes"] = modes
	for key, value := range map[string]string{"lan_url": r.LANURL, "stt": r.STT, "tts": r.TTS, "llm": r.LLM} {
		if v := strings.TrimSpace(value); v != "" {
			reported[key] = v
		}
	}
	body := map[string]any{
		"schema":           checkInSchema,
		"kind":             Kind,
		"board":            "speechkit-server-linux-" + runtime.GOARCH,
		"firmware_version": version,
		"reported":         reported,
	}
	if name := clip(strings.TrimSpace(r.DisplayName), 64); name != "" {
		body["display_name"] = name
	}
	if ip := strings.TrimSpace(r.LocalIP); ip != "" {
		body["local_ip"] = ip
	}
	return body
}

// authorized sends one request with a current access token, refreshing it
// first when needed and once more after a 401. Caller holds m.opMu.
func (m *Manager) authorized(ctx context.Context, cred *credential, method, endpoint string, body []byte) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if m.accessLink != linkIdentity(cred) {
			m.access = ""
		}
		if m.access == "" || time.Now().Add(time.Minute).After(m.accessUntil) {
			if err := m.refreshLocked(ctx, cred); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+m.access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "speechkit-server/"+firstNonEmpty(m.version, "dev"))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := m.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("registry: %w", err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_ = resp.Body.Close()
			m.access = ""
			continue
		}
		return resp, nil
	}
	return nil, errors.New("registry: access token refused after refresh")
}

func (m *Manager) refreshLocked(ctx context.Context, cred *credential) error {
	tokens, err := m.oauth.refresh(ctx, cred.RefreshToken)
	if errors.Is(err, ErrInvalidGrant) {
		cred.RelinkRequired = true
		if saveErr := saveCredential(m.settings.StateDir, cred); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	if err != nil {
		return err
	}
	if tokens.RefreshToken != "" && tokens.RefreshToken != cred.RefreshToken {
		// Rotation: the old token is spent the moment this answer exists.
		cred.RefreshToken = tokens.RefreshToken
		if err := saveCredential(m.settings.StateDir, cred); err != nil {
			return err
		}
	}
	m.access, m.accessUntil, m.accessLink = tokens.AccessToken, tokens.ExpiresAt, linkIdentity(cred)
	return nil
}

// linkIdentity names one stored link. A rotated refresh token keeps it; an
// unlink followed by a link (another account, another grant) changes it.
func linkIdentity(cred *credential) string {
	return strings.Join([]string{cred.Issuer, cred.ClientID, cred.Audience, cred.DeviceID, cred.LinkedAt.Format(time.RFC3339Nano)}, "|")
}

// registryError turns a registry denial into its code and guidance, never
// the raw body.
func registryError(status int, payload []byte) error {
	var e struct {
		Error        string `json:"error"`
		ErrorCode    string `json:"error_code"`
		UserGuidance struct {
			Body string `json:"body"`
		} `json:"user_guidance"`
	}
	_ = json.Unmarshal(payload, &e)
	code := firstNonEmpty(e.ErrorCode, e.Error)
	switch {
	case code != "" && e.UserGuidance.Body != "":
		return fmt.Errorf("registry answered HTTP %d (%s): %s", status, code, clip(e.UserGuidance.Body, 200))
	case code != "":
		return fmt.Errorf("registry answered HTTP %d (%s)", status, code)
	}
	return fmt.Errorf("registry answered HTTP %d", status)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
