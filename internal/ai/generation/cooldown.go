package generation

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// How long a provider sits out after a failure about the account rather than
// the request. An exhausted quota does not come back within minutes; a missing
// sign-in or grant is fixed by the user, so it is asked again soon.
const (
	CooldownQuota   = 10 * time.Minute
	CooldownAccount = 2 * time.Minute
)

// ProviderIdentifier is implemented by generators that serve exactly one
// provider, so a chain can route a request pinned to a model without listing
// every generator's models first.
type ProviderIdentifier interface {
	ProviderID() string
}

// Cooldown sets a generator aside after a failure that the next request would
// hit again: an exhausted quota, a signed-out session, a missing grant. While
// it rests, Generate returns that failure at once and Models lists nothing, so
// a chain falls through to the next generator immediately and callers size
// their work for the model that will actually answer.
//
// Before, a GitHub Copilot account over its monthly quota was started and
// asked again for every meeting batch and every pass of a write-up — each
// attempt spent about 13 s before failing, and the write-up's time budget was
// sized for Copilot while the slow local fallback did the work.
type Cooldown struct {
	inner Generator
	now   func() time.Time

	mu    sync.Mutex
	until time.Time
	cause error
}

func NewCooldown(inner Generator) *Cooldown {
	return &Cooldown{inner: inner, now: time.Now}
}

func (c *Cooldown) Generate(ctx context.Context, request Request) (Result, error) {
	if cause := c.resting(); cause != nil {
		return Result{}, cause
	}
	result, err := c.inner.Generate(ctx, request)
	if err != nil && ctx.Err() == nil {
		c.observe(err)
	}
	return result, err
}

func (c *Cooldown) Models(ctx context.Context, query ModelQuery) (Catalog, error) {
	if c.paused() {
		return Catalog{}, nil
	}
	return c.inner.Models(ctx, query)
}

func (c *Cooldown) paused() bool {
	return c.resting() != nil
}

// ProviderID forwards the wrapped generator's provider, if it names one.
func (c *Cooldown) ProviderID() string {
	if named, ok := c.inner.(ProviderIdentifier); ok {
		return named.ProviderID()
	}
	return ""
}

func (c *Cooldown) resting() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cause == nil || !c.now().Before(c.until) {
		return nil
	}
	return &restingError{cause: c.cause}
}

// restingError is the failure a resting provider answers with. It unwraps to
// the failure that set the provider aside, so its kind is unchanged; it is
// distinct so a chain does not log the same pause again for every request.
type restingError struct {
	cause error
}

func (e *restingError) Error() string { return e.cause.Error() }

func (e *restingError) Unwrap() error { return e.cause }

func isResting(err error) bool {
	var resting *restingError
	return errors.As(err, &resting)
}

func (c *Cooldown) observe(err error) {
	var pause time.Duration
	switch Kind(err) {
	case ErrorQuota:
		pause = CooldownQuota
	case ErrorAuthentication, ErrorConsent, ErrorConfiguration:
		pause = CooldownAccount
	default:
		return
	}
	c.mu.Lock()
	c.until = c.now().Add(pause)
	c.cause = err
	c.mu.Unlock()
	slog.Warn("generation.provider_paused", "provider", c.ProviderID(), "kind", string(Kind(err)), "for", pause, "err", err)
}
