package wssession

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ModeAuthority is the renewable platform-mode binding of one live session: the
// Gateway-signed proof that the session's owner opened it and consented to cloud
// processing, which every think turn presents. The Gateway renews it with the
// voice budget; a renewal keeps the session nonce and only moves the expiry
// forward. The HMAC is verified by the edge middleware before it reaches here;
// this type owns the identity of the session's binding across renewals.
type ModeAuthority struct {
	mu            sync.RWMutex
	binding       string
	signature     string
	endpoint      string
	nonce         string
	admission     string
	reservationID string
	expiresAt     int64
}

// ErrModeRenewalInvalid is returned when a renewed binding is not the same session's, later binding.
var ErrModeRenewalInvalid = errors.New("ws session: invalid platform mode renewal")

func modeIdentity(binding string) (nonce, admission, reservationID string) {
	raw, err := base64.StdEncoding.DecodeString(binding)
	var proof struct {
		Nonce         string `json:"sessionNonce"`
		Admission     string `json:"admission"`
		ReservationID string `json:"reservationId"`
	}
	if err != nil || json.Unmarshal(raw, &proof) != nil {
		return "", "", ""
	}
	return proof.Nonce, proof.Admission, proof.ReservationID
}

// NewModeAuthority starts a session's binding from the verified mint binding.
func NewModeAuthority(binding, signature, endpoint string, expiresAt int64) (*ModeAuthority, error) {
	nonce, admission, reservationID := modeIdentity(binding)
	if nonce == "" || admission == "" || reservationID == "" || signature == "" || endpoint == "" || expiresAt <= time.Now().Unix() {
		return nil, ErrModeRenewalInvalid
	}
	return &ModeAuthority{binding: binding, signature: signature, endpoint: endpoint, nonce: nonce, admission: admission, reservationID: reservationID, expiresAt: expiresAt}, nil
}

// Current returns the binding a turn presents now.
func (m *ModeAuthority) Current() (binding, signature, endpoint string, expiresAt int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.binding, m.signature, m.endpoint, m.expiresAt
}

// ReservationID is the Cloud budget reservation the current binding was minted for.
func (m *ModeAuthority) ReservationID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reservationID
}

// CheckRenewal reports whether a verified binding may replace the current one: the same session
// (nonce), admission kind and route, a strictly later expiry.
func (m *ModeAuthority) CheckRenewal(binding, signature, endpoint string, expiresAt int64) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.checkLocked(binding, signature, endpoint, expiresAt)
}

func (m *ModeAuthority) checkLocked(binding, signature, endpoint string, expiresAt int64) error {
	nonce, admission, _ := modeIdentity(binding)
	if signature == "" || endpoint != m.endpoint || nonce != m.nonce || admission != m.admission || expiresAt <= m.expiresAt || expiresAt <= time.Now().Unix() {
		return ErrModeRenewalInvalid
	}
	return nil
}

// Renew swaps in the renewed binding for every later turn.
func (m *ModeAuthority) Renew(binding, signature, endpoint string, expiresAt int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkLocked(binding, signature, endpoint, expiresAt); err != nil {
		return err
	}
	_, _, m.reservationID = modeIdentity(binding)
	m.binding, m.signature, m.expiresAt = binding, signature, expiresAt
	return nil
}

// WatchExpiry calls end once the binding's current expiry has passed, following renewals: the
// watch sleeps to the expiry it sees and looks again, so a renewal extends the session. The
// returned stop releases it.
func (m *ModeAuthority) WatchExpiry(end func()) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			m.mu.RLock()
			remaining := time.Until(time.Unix(m.expiresAt, 0))
			m.mu.RUnlock()
			if remaining <= 0 {
				end()
				return
			}
			timer := time.NewTimer(remaining)
			select {
			case <-done:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}
