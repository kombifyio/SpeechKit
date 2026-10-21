package config

import (
	"context"
	"log/slog"
	"maps"
	"time"
)

// PolicyPollInterval is how often the machine policy is re-read when the
// platform offers no change notification (or as a fallback to it).
const PolicyPollInterval = 5 * time.Minute

// PolicyRefresh describes one re-read of the machine policy while SpeechKit
// runs.
type PolicyRefresh struct {
	// Tightened names the policy keys whose new or stricter value was
	// applied at once.
	Tightened []string
	// Deferred names the policy keys whose removal or wider value takes
	// effect at the next start. A running process never widens access.
	Deferred []string
	// Rejected lists values in the new read that were not applied.
	Rejected []PolicyRejection
	// Policy is the policy now bound to the config.
	Policy PolicyValues
}

// Changed reports whether the refresh found anything to apply or defer.
func (r PolicyRefresh) Changed() bool {
	return len(r.Tightened) > 0 || len(r.Deferred) > 0
}

// RefreshPolicy compares next, a fresh read of the machine policy, with the
// policy bound to cfg. Every tightening (a new restrictive value, a stricter
// scope, a narrower list, a new pin of the value cfg already holds) is bound
// and applied to cfg at once. Every loosening (a removed pin, a wider or
// different value, entries added to a list) is left for the next start.
func RefreshPolicy(cfg *Config, next PolicyValues) PolicyRefresh {
	result := PolicyRefresh{Rejected: next.Rejected}
	if cfg == nil {
		return result
	}
	cur := cfg.Policy()
	out := cur
	out.origins = maps.Clone(cur.origins)
	if out.origins == nil {
		out.origins = map[string]string{}
	}
	for _, k := range policyKeys {
		verdict := k.ops.refresh(cur, next, cfg, &out)
		if verdict.tightened {
			result.Tightened = append(result.Tightened, k.name)
			if source, ok := next.origins[k.name]; ok {
				out.origins[k.name] = source
			}
		}
		if verdict.deferred {
			result.Deferred = append(result.Deferred, k.name)
		}
	}
	out.IgnoredUserKeys = next.IgnoredUserKeys
	out.Rejected = next.Rejected
	out.finalize()
	if len(result.Tightened) > 0 {
		AttachPolicy(cfg, out)
	}
	result.Policy = cfg.Policy()
	return result
}

// samePolicyValues reports whether two reads carry the same values.
func samePolicyValues(a, b PolicyValues) bool {
	for _, k := range policyKeys {
		if !k.ops.same(a, b) {
			return false
		}
	}
	return true
}

// WatchPolicy re-reads the machine policy whenever the platform reports a
// change (on Windows a registry change notification on the Policies key) and
// at least every PolicyPollInterval, and passes each read that differs from
// the previous one through RefreshPolicy. onRefresh receives every refresh
// that tightened or deferred something, for the audit trail. It returns at
// once; the watch stops when ctx ends. On platforms without a policy source
// it does nothing.
//
// Every tightening is committed through shared (Shared.RefreshPolicy), so
// its subscribers (the egress policy, the provider runtimes) re-apply it on
// the watcher goroutine.
func WatchPolicy(ctx context.Context, shared *Shared, onRefresh func(PolicyRefresh)) {
	if shared == nil {
		return
	}
	last := LastPolicy()
	watchPolicyChanges(ctx, PolicyPollInterval, func() {
		next := readPolicyValues()
		if samePolicyValues(last, next) {
			return
		}
		last = next
		refresh := shared.RefreshPolicy(next)
		logPolicyRejections(refresh.Rejected)
		if !refresh.Changed() {
			return
		}
		slog.Info("config: machine policy changed",
			"tightened", refresh.Tightened,
			"deferred_to_next_start", refresh.Deferred)
		if onRefresh != nil {
			onRefresh(refresh)
		}
	})
}

// pollPolicyChanges calls notify every interval until ctx ends.
func pollPolicyChanges(ctx context.Context, interval time.Duration, notify func()) { //nolint:unused // used by the windows and darwin policy watchers
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				notify()
			}
		}
	}()
}

func logPolicyRejections(rejected []PolicyRejection) {
	for _, r := range rejected {
		slog.Warn("config: policy value rejected", "key", r.Key, "reason", r.Err)
	}
}
