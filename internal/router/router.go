// Package router is a compatibility alias shim: the STT routing layer now
// lives in the public framework package pkg/speechkit/stt (Router, Strategy,
// BuildRouter), so both the Device-Target and the Server-Target assemble it
// through one path. Existing internal call sites keep compiling unchanged via
// the aliases below. New code should import pkg/speechkit/stt directly.
//
// The shim also owns the host-side audit wiring: the public router exposes a
// per-instance provider-selected observer instead of importing
// internal/auditlog, and both targets install AuditObserver on the routers
// they build.
package router

import (
	"context"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	pkgstt "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Router selects the best STT provider; see pkg/speechkit/stt.Router.
type Router = pkgstt.Router

// Strategy defines the routing strategy; see pkg/speechkit/stt.Strategy.
type Strategy = pkgstt.Strategy

const (
	StrategyDynamic   = pkgstt.StrategyDynamic
	StrategyLocalOnly = pkgstt.StrategyLocalOnly
	StrategyCloudOnly = pkgstt.StrategyCloudOnly
)

// AuditObserver records a provider.selected audit event for every successful
// routed transcription, exactly as the pre-promotion internal router did.
// Hosts set it as stt.Router.OnProviderSelected (or
// allproviders.RouterConfig.OnProviderSelected). Errors from AppendEvent are
// intentionally discarded — audit failures must never abort a user-facing
// transcription.
func AuditObserver(ctx context.Context, providerName string, strategy Strategy) {
	_ = auditlog.AppendEvent(ctx, auditlog.Record{
		Event: auditlog.EventProviderSelected,
		Resource: map[string]any{
			"provider_name": providerName,
			"provider_kind": "stt",
			"strategy":      string(strategy),
		},
		Outcome: auditlog.OutcomeSuccess,
	})
}
