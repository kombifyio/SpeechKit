//go:build linux

package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// VoiceUsage is provider-authoritative connected time. The server starts the
// clock only after the realtime provider accepts the session and stops it when
// the adapter terminates.
type VoiceUsage struct {
	SessionID     string
	AISessionID   string
	Provider      string
	Duration      time.Duration
	ReservationID string
	// ReceiptToken is retained from the signed admission, never from media or
	// client frames. It authorizes only this reservation's final accounting.
	ReceiptToken string `json:"-"`
	// Segment numbers the reservations a renewed session settled in order;
	// each Cloud reservation settles exactly once under its own event.
	Segment int
	// Retained from the authenticated session owner, never from the expired
	// bridge credential or a client-controlled usage payload.
	OwnerUserID string
	OwnerOrgID  string
}

// voiceUsageEventID keeps the first reservation's historical event and gives
// every renewed reservation its own idempotency key.
func voiceUsageEventID(usage VoiceUsage) string {
	if usage.Segment > 0 {
		return "voice-session:" + usage.SessionID + ":r" + strconv.Itoa(usage.Segment) + ":connected"
	}
	return "voice-session:" + usage.SessionID + ":connected"
}

type UsageReporter interface {
	Report(context.Context, string, VoiceUsage) error
}

func (h *Handler) reportUnstartedSession(session *ManagedSession) {
	if session.AnonymousGuide || session.VoiceBudget.ReservationID == "" {
		return
	}
	h.reportVoiceUsage("", VoiceUsage{
		SessionID: session.ID, AISessionID: session.AISessionID, ReservationID: session.VoiceBudget.ReservationID,
		ReceiptToken: session.VoiceBudget.ReceiptToken,
		OwnerUserID:  session.Owner.UserID, OwnerOrgID: session.Owner.OrgID,
	})
}

// usageRetryDelays spaces the retries of a final receipt over about ten
// minutes, inside the hold's receipt grace in Cloud. The event id is stable,
// so a retry never charges twice.
var usageRetryDelays = []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second, 90 * time.Second, 180 * time.Second, 300 * time.Second}

// usageSettlement counts the final receipts in flight (first send and
// retries), so shutdown waits for each of them before the process exits.
type usageSettlement struct {
	mu       sync.Mutex
	inflight int
	stop     chan struct{} // closed at shutdown: retries no longer back off
	stopOnce sync.Once
}

func newUsageSettlement() *usageSettlement {
	return &usageSettlement{stop: make(chan struct{})}
}

func (s *usageSettlement) begin() {
	s.mu.Lock()
	s.inflight++
	s.mu.Unlock()
}

func (s *usageSettlement) end() {
	s.mu.Lock()
	s.inflight--
	s.mu.Unlock()
}

func (s *usageSettlement) idle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight == 0
}

// pause waits out one retry delay; once shutdown has begun every retry
// follows a second after the previous one.
func (s *usageSettlement) pause(delay time.Duration) {
	select {
	case <-s.stop:
		time.Sleep(time.Second)
	default:
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-s.stop:
		}
	}
}

func (h *Handler) reportVoiceUsage(credential string, usage VoiceUsage) {
	if h.usage == nil {
		return
	}
	s := h.settlement
	if s != nil {
		s.begin()
	}
	// Teardown may outlive the request/credential. The existing hosted service
	// reporter signs a fresh, bounded owner-specific accounting request.
	err := h.sendVoiceUsage(credential, usage)
	if err == nil || s == nil || !usageRetryable(err) {
		if err != nil {
			slog.Error("voiceagent: usage report failed", "session_id", usage.SessionID, "reservation_id", usage.ReservationID, "code", "usage_report_failed") // #nosec G706 -- IDs are structured attributes; upstream details may contain credentials.
		}
		if s != nil {
			s.end()
		}
		return
	}
	slog.Warn("voiceagent: usage report failed; retrying", "session_id", usage.SessionID, "reservation_id", usage.ReservationID, "code", "usage_report_retry") // #nosec G706 -- IDs are structured attributes; upstream details may contain credentials.
	go func() {
		defer s.end()
		for i, delay := range usageRetryDelays {
			s.pause(delay)
			err := h.sendVoiceUsage(credential, usage)
			if err == nil {
				slog.Info("voiceagent: usage report settled after retry", "session_id", usage.SessionID, "reservation_id", usage.ReservationID, "attempt", i+2) // #nosec G706 -- IDs are structured attributes.
				return
			}
			if !usageRetryable(err) {
				break
			}
		}
		slog.Error("voiceagent: usage report failed", "session_id", usage.SessionID, "reservation_id", usage.ReservationID, "code", "usage_report_failed") // #nosec G706 -- IDs are structured attributes; upstream details may contain credentials.
	}()
}

func (h *Handler) sendVoiceUsage(credential string, usage VoiceUsage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return h.usage.Report(ctx, credential, usage)
}

// Shutdown settles every Voice reservation this process holds before it
// exits: unconnected sessions at zero, live ones with their connected time,
// and the receipts still being retried are sent again every second until
// they land or ctx ends.
func (h *Handler) Shutdown(ctx context.Context) error {
	if h == nil || h.manager == nil {
		return nil
	}
	s := h.settlement
	if s != nil {
		s.stopOnce.Do(func() { close(s.stop) })
	}
	// Settlements run side by side: each may wait on the ledger.
	for _, settle := range h.manager.Drain() {
		go settle()
	}
	for !h.manager.Settled() || (s != nil && !s.idle()) {
		select {
		case <-ctx.Done():
			return errors.New("voiceagent: Voice reservations still unsettled at shutdown")
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}

// usageRejectedError is a non-2xx answer of the usage endpoint.
type usageRejectedError struct{ status int }

func (e usageRejectedError) Error() string {
	return fmt.Sprintf("voiceagent usage rejected with status %d", e.status)
}

// usageRetryable: transport failures, 5xx and 429 may succeed later; any
// other refusal and a local validation error will not.
func usageRetryable(err error) bool {
	var rejected usageRejectedError
	if errors.As(err, &rejected) {
		return rejected.status >= 500 || rejected.status == http.StatusTooManyRequests
	}
	return errors.Is(err, errUsageTransport)
}

var errUsageTransport = errors.New("voiceagent usage transport failed")

type HTTPUsageReporter struct {
	Endpoint string
	Client   *http.Client
}

func NewHTTPUsageReporter(endpoint string) *HTTPUsageReporter {
	return &HTTPUsageReporter{
		Endpoint: strings.TrimSpace(endpoint),
		Client:   netsec.NewClient(netsec.ClientOptions{Timeout: 5 * time.Second}),
	}
}

func (r *HTTPUsageReporter) Report(ctx context.Context, credential string, usage VoiceUsage) error {
	if strings.TrimSpace(credential) == "" {
		return errors.New("voiceagent usage credential is missing")
	}
	return r.report(ctx, usage, func(req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+credential)
		return nil
	}, false)
}

// report shares the ledger event and retry semantics across legacy bearer and
// hosted service credentials. A retry never creates a second usage event.
func (r *HTTPUsageReporter) report(ctx context.Context, usage VoiceUsage, authorize func(*http.Request) error, forbidRedirects bool) error {
	if r == nil || strings.TrimSpace(r.Endpoint) == "" {
		return errors.New("voiceagent usage endpoint is not configured")
	}
	if usage.Duration < 0 || (usage.Duration == 0 && usage.ReservationID == "") || strings.TrimSpace(usage.SessionID) == "" {
		return errors.New("voiceagent usage is invalid")
	}
	event := map[string]any{
		"tool": "SPEECHKIT",
		"events": []map[string]any{{
			"event_id": voiceUsageEventID(usage),
			"metric":   "audio_minutes",
			"value":    usage.Duration.Minutes(),
			"metadata": map[string]string{
				"session_id":    usage.SessionID,
				"ai_session_id": usage.AISessionID,
				"provider":      usage.Provider,
			},
		}},
	}
	if usage.ReservationID != "" {
		event["reservation_id"] = usage.ReservationID
		if usage.ReceiptToken != "" {
			event["reservation_receipt"] = usage.ReceiptToken
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode voiceagent usage: %w", err)
	}
	client := r.Client
	if client == nil {
		client = netsec.NewClient(netsec.ClientOptions{Timeout: 5 * time.Second})
	}
	if forbidRedirects || usage.ReceiptToken != "" {
		// Redirects can retain custom service-auth headers or replay a receipt
		// in the POST body. Both capabilities are bound to the configured broker.
		isolated := *client
		isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &isolated
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("create voiceagent usage request: %w", err)
		}
		if err := authorize(req); err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("send voiceagent usage: %w: %w", errUsageTransport, err)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = usageRejectedError{status: resp.StatusCode}
		if resp.StatusCode < 500 {
			break
		}
	}
	return lastErr
}
