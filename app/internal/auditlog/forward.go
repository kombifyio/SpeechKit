package auditlog

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Forward bounds: how long a forward-required write waits for one sink to
// acknowledge a record before the triggering action proceeds without it.
// The Event Log is a local API call (ReportEvent) that normally returns in
// well under a millisecond, so a few hundred milliseconds only trip on a hung
// service. OTLP is a network round trip with TLS, so it gets seconds.
const (
	EventLogForwardBound = 300 * time.Millisecond
	OTLPForwardBound     = 3 * time.Second
	// DefaultForwardBound applies to a Forwarder whose Bound is not positive.
	DefaultForwardBound = 3 * time.Second

	// forwardRetryInterval is how often a degraded package retries the oldest
	// unacknowledged record without waiting for a new event.
	forwardRetryInterval = 15 * time.Second
	// forwardBacklogLimit caps the unacknowledged records kept for
	// redelivery. Older ones are dropped; the forwarded copy then shows a
	// sequence gap, which is the off-box alarm.
	forwardBacklogLimit = 512
)

// Degraded reasons reported in ForwardState.Reason and in the
// audit.forward_degraded record.
const (
	ForwardReasonNoSink        = "no_sink"
	ForwardReasonTimeout       = "timeout"
	ForwardReasonSinkError     = "sink_error"
	ForwardReasonAuditDisabled = "audit_disabled"
)

// ErrForwardDegraded is returned by CloudSessionAllowed while forwarding is
// required and no forward sink has acknowledged the latest record.
var ErrForwardDegraded = errors.New("auditlog: audit forwarding is degraded; cloud sessions are refused until a forward succeeds")

var (
	errNoForwardSink = errors.New("auditlog: no forward sink configured")
	errNotExported   = errors.New("auditlog: forward sink did not export the record")
)

// Forwarder is one off-box sink for audit records. Forward must return nil
// only once the record has been handed to a transport that takes it off the
// device (the Event Log service, an OTLP receiver).
type Forwarder interface {
	// Name identifies the sink in audit.started ("event_log", "otlp").
	Name() string
	// Bound is how long a forward-required write waits for this sink.
	Bound() time.Duration
	// Forward delivers rec; ctx carries the Bound deadline.
	Forward(ctx context.Context, rec Record) error
}

// ForwardState is the forwarding health the UI and the session gates read.
type ForwardState struct {
	// Required reports forward_required.
	Required bool `json:"required"`
	// Degraded reports that a required forward was not acknowledged and no
	// forward has succeeded since.
	Degraded bool `json:"degraded"`
	// Since is when the degraded state began.
	Since time.Time `json:"since,omitzero"`
	// Reason is one of the ForwardReason values.
	Reason string `json:"reason,omitempty"`
	// Pending counts records waiting for redelivery.
	Pending int `json:"pending,omitempty"`
}

var (
	fwdMu       sync.Mutex
	fwdRequired bool
	fwdSinks    []Forwarder
	fwdDegraded bool
	fwdSince    time.Time
	fwdReason   string
	fwdBacklog  []Record
	fwdHook     func(ForwardState)
	// fwdStop is closed when forwarding is reconfigured or reset; the retry
	// loop and redelivery of the previous configuration watch it.
	fwdStop     chan struct{}
	fwdRetrying bool
)

// configureForwarding installs the sinks and the forward-required flag.
// Forward-required with the audit log switched off is degraded from the
// start: no record can be forwarded, and that must not pass silently.
func configureForwarding(required, auditEnabled bool, sinks []Forwarder) {
	fwdMu.Lock()
	fwdRequired = required
	fwdSinks = sinks
	fwdDegraded = false
	fwdSince = time.Time{}
	fwdReason = ""
	fwdBacklog = nil
	fwdStop = make(chan struct{})
	fwdRetrying = false
	var notify func(ForwardState)
	if required && !auditEnabled {
		fwdDegraded = true
		fwdSince = time.Now().UTC()
		fwdReason = ForwardReasonAuditDisabled
	}
	notify = fwdHook
	st := forwardStateLocked()
	fwdMu.Unlock()
	if notify != nil {
		notify(st)
	}
}

// stopForwarding ends the retry loop and redelivery of the current
// configuration.
func stopForwarding() {
	fwdMu.Lock()
	if fwdStop != nil {
		close(fwdStop)
		fwdStop = nil
	}
	fwdRetrying = false
	fwdMu.Unlock()
}

func resetForwarding() {
	fwdMu.Lock()
	fwdRequired = false
	fwdSinks = nil
	fwdDegraded = false
	fwdSince = time.Time{}
	fwdReason = ""
	fwdBacklog = nil
	fwdHook = nil
	fwdMu.Unlock()
}

func forwardStateLocked() ForwardState {
	return ForwardState{
		Required: fwdRequired,
		Degraded: fwdDegraded,
		Since:    fwdSince,
		Reason:   fwdReason,
		Pending:  len(fwdBacklog),
	}
}

// ForwardStatus reports the forwarding health.
func ForwardStatus() ForwardState {
	fwdMu.Lock()
	defer fwdMu.Unlock()
	return forwardStateLocked()
}

// CloudSessionAllowed returns ErrForwardDegraded while forwarding is
// required and degraded, and nil otherwise. Hosts ask it before starting a
// session that sends audio or text to a cloud provider; local-only work does
// not ask.
func CloudSessionAllowed() error {
	fwdMu.Lock()
	defer fwdMu.Unlock()
	if fwdRequired && fwdDegraded {
		return ErrForwardDegraded
	}
	return nil
}

// OnForwardStateChange registers fn to be told when the package enters or
// leaves the degraded state. A degraded state that began before fn was
// registered is reported right away. fn runs without package locks held.
func OnForwardStateChange(fn func(ForwardState)) {
	fwdMu.Lock()
	fwdHook = fn
	st := forwardStateLocked()
	fwdMu.Unlock()
	if fn != nil && st.Degraded {
		fn(st)
	}
}

// deliver forwards one written record. Without forward_required the sinks
// get it best effort, as before; with it, the call waits for an
// acknowledgement and moves the package into or out of the degraded state.
func deliver(ctx context.Context, rec Record) {
	fwdMu.Lock()
	required := fwdRequired
	sinks := fwdSinks
	stop := fwdStop
	fwdMu.Unlock()

	if !required {
		emitToEventLog(rec)
		emitToOTLP(ctx, rec)
		for _, sink := range sinks {
			if isBuiltinSink(sink) {
				continue
			}
			go func(s Forwarder) {
				fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), boundOf(s))
				defer cancel()
				_ = s.Forward(fctx, rec)
			}(sink)
		}
		return
	}

	if err := forwardOnce(ctx, rec, sinks); err != nil {
		enterDegraded(ctx, stop, &rec, reasonFor(err))
		return
	}
	forwardSucceeded(ctx, stop)
}

func boundOf(s Forwarder) time.Duration {
	if b := s.Bound(); b > 0 {
		return b
	}
	return DefaultForwardBound
}

func reasonFor(err error) string {
	switch {
	case errors.Is(err, errNoForwardSink):
		return ForwardReasonNoSink
	case errors.Is(err, context.DeadlineExceeded):
		return ForwardReasonTimeout
	default:
		return ForwardReasonSinkError
	}
}

// forwardOnce hands rec to every sink at once and returns nil as soon as one
// acknowledges it. Each sink waits at most its own bound; the caller never
// waits longer than the largest bound.
func forwardOnce(ctx context.Context, rec Record, sinks []Forwarder) error {
	if len(sinks) == 0 {
		return errNoForwardSink
	}
	base := context.WithoutCancel(ctx)
	var longest time.Duration
	results := make(chan error, len(sinks))
	for _, sink := range sinks {
		bound := boundOf(sink)
		if bound > longest {
			longest = bound
		}
		go func(s Forwarder, bound time.Duration) {
			fctx, cancel := context.WithTimeout(base, bound)
			defer cancel()
			results <- s.Forward(fctx, rec)
		}(sink, bound)
	}
	timer := time.NewTimer(longest + 50*time.Millisecond)
	defer timer.Stop()
	var last error
	for range sinks {
		select {
		case err := <-results:
			if err == nil {
				return nil
			}
			last = err
		case <-timer.C:
			return context.DeadlineExceeded
		}
	}
	return last
}

// enterDegraded queues rec (when given) for redelivery and, on the
// transition, writes the audit.forward_degraded record locally, tells the
// hook and starts the retry loop.
func enterDegraded(ctx context.Context, stop chan struct{}, rec *Record, reason string) {
	ctx = context.WithoutCancel(ctx)
	fwdMu.Lock()
	if stop == nil || stop != fwdStop {
		fwdMu.Unlock()
		return
	}
	if rec != nil {
		queueLocked(*rec)
	}
	transition := !fwdDegraded
	if transition {
		fwdDegraded = true
		fwdSince = time.Now().UTC()
		fwdReason = reason
	}
	startRetry := !fwdRetrying && len(fwdSinks) > 0
	if startRetry {
		fwdRetrying = true
	}
	hook := fwdHook
	fwdMu.Unlock()

	if transition {
		resource := map[string]any{"reason": reason}
		if rec != nil {
			resource["seq"] = rec.Seq
		}
		marker, built, _ := writeRecord(ctx, Record{
			Event:    EventAuditForwardDegraded,
			Resource: resource,
			Outcome:  OutcomeFailure,
		})
		fwdMu.Lock()
		if built && stop == fwdStop {
			queueLocked(marker)
		}
		st := forwardStateLocked()
		fwdMu.Unlock()
		if hook != nil {
			hook(st)
		}
	}
	if startRetry {
		go retryLoop(ctx, stop)
	}
}

func queueLocked(rec Record) {
	fwdBacklog = append(fwdBacklog, rec)
	if over := len(fwdBacklog) - forwardBacklogLimit; over > 0 {
		fwdBacklog = append([]Record(nil), fwdBacklog[over:]...)
	}
}

// forwardSucceeded leaves the degraded state after a forward was
// acknowledged: it records audit.forward_restored (itself forwarded) and
// redelivers the backlog in the background.
func forwardSucceeded(ctx context.Context, stop chan struct{}) {
	ctx = context.WithoutCancel(ctx)
	fwdMu.Lock()
	if stop == nil || stop != fwdStop || !fwdDegraded {
		fwdMu.Unlock()
		return
	}
	since := fwdSince
	var from, to uint64
	if n := len(fwdBacklog); n > 0 {
		from, to = fwdBacklog[0].Seq, fwdBacklog[n-1].Seq
	}
	fwdDegraded = false
	fwdSince = time.Time{}
	fwdReason = ""
	hook := fwdHook
	st := forwardStateLocked()
	fwdMu.Unlock()

	if hook != nil {
		hook(st)
	}
	_ = AppendEvent(ctx, Record{
		Event: EventAuditForwardRestored,
		Resource: map[string]any{
			"degraded_seconds": time.Since(since).Seconds(),
			"pending_seq_from": from,
			"pending_seq_to":   to,
		},
	})
	go redeliver(ctx, stop)
}

// redeliver forwards the backlog oldest first. It stops at the first record
// no sink acknowledges, which puts the package back into the degraded state.
func redeliver(ctx context.Context, stop chan struct{}) {
	for {
		fwdMu.Lock()
		if stop != fwdStop || len(fwdBacklog) == 0 {
			fwdMu.Unlock()
			return
		}
		next := fwdBacklog[0]
		sinks := fwdSinks
		fwdMu.Unlock()

		if err := forwardOnce(ctx, next, sinks); err != nil {
			enterDegraded(ctx, stop, nil, reasonFor(err))
			return
		}
		fwdMu.Lock()
		if stop == fwdStop && len(fwdBacklog) > 0 && fwdBacklog[0].Seq == next.Seq {
			fwdBacklog = fwdBacklog[1:]
		}
		fwdMu.Unlock()
	}
}

// retryLoop retries the oldest unacknowledged record while degraded, so
// forwarding recovers without waiting for the next user action.
func retryLoop(ctx context.Context, stop chan struct{}) {
	ticker := time.NewTicker(forwardRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		fwdMu.Lock()
		if stop != fwdStop || !fwdDegraded || len(fwdBacklog) == 0 {
			if stop == fwdStop {
				fwdRetrying = false
			}
			fwdMu.Unlock()
			return
		}
		next := fwdBacklog[0]
		sinks := fwdSinks
		fwdMu.Unlock()

		if forwardOnce(ctx, next, sinks) != nil {
			continue
		}
		fwdMu.Lock()
		if stop == fwdStop && len(fwdBacklog) > 0 && fwdBacklog[0].Seq == next.Seq {
			fwdBacklog = fwdBacklog[1:]
		}
		fwdRetrying = false
		fwdMu.Unlock()
		forwardSucceeded(ctx, stop)
		return
	}
}
