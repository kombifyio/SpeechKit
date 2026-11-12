package discovery

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/miekg/dns"
)

// pairingZone evaluates the admission gate for each answer, without mutating
// the shared mDNS service. DNS caches can retain earlier answers until their TTL.
type pairingZone struct {
	service *mdns.MDNSService
	enabled func() bool
}

func (z pairingZone) Records(q dns.Question) []dns.RR {
	service := *z.service
	if z.enabled != nil && z.enabled() {
		service.TXT = append(append([]string(nil), service.TXT...), "pair=1")
	}
	return service.Records(q)
}

const pairingStateName = "discovery-pairing.state"
const pairingStateInterval = time.Second
const pairingStateLifetime = 5 * time.Second

// MaintainPairingState shares the actual admission gate with --discovery-only
// through the existing shared state directory. Missing, unreadable or stale
// state keeps the sidecar's pairing flag off; it never infers it from config.
func MaintainPairingState(ctx context.Context, stateDir string, enabled func() bool) func() {
	ctx, cancel := context.WithCancel(ctx)
	path := filepath.Join(stateDir, pairingStateName)
	publish := func() error {
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return err
		}
		f, err := os.CreateTemp(stateDir, ".discovery-pairing-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(f.Name()) }()
		value := "0\n"
		if enabled() {
			value = "1\n"
		}
		_, err = f.WriteString(value)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Rename(f.Name(), path)
	}
	if err := publish(); err != nil {
		slog.Warn("discovery pairing state unavailable", "err", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(pairingStateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := publish(); err != nil {
					// Remove any older open state immediately; stale state also expires.
					_ = os.Remove(path)
					slog.Debug("discovery pairing state unavailable", "err", err)
				}
			}
		}
	}()
	return func() { cancel(); <-done; _ = os.Remove(path) }
}

func pairingAvailable(stateDir string) bool {
	f, err := os.Open(filepath.Join(stateDir, pairingStateName))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || time.Since(info.ModTime()) > pairingStateLifetime {
		return false
	}
	var value [2]byte
	n, err := f.Read(value[:])
	return err == nil && n == len(value) && string(value[:]) == "1\n"
}
