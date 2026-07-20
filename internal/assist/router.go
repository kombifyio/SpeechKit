package assist

import "github.com/kombifyio/SpeechKit/internal/shortcuts"

type Router struct {
	resolver  *shortcuts.Resolver
	utilities *UtilityRegistry
}

type RouterOption func(*Router)

func NewRouter(opts ...RouterOption) *Router {
	router := &Router{
		resolver:  shortcuts.DefaultResolver(),
		utilities: DefaultUtilityRegistry(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(router)
		}
	}
	return router
}

func WithResolver(resolver *shortcuts.Resolver) RouterOption {
	return func(router *Router) {
		if resolver != nil {
			router.resolver = resolver
		}
	}
}

func WithUtilityRegistry(registry *UtilityRegistry) RouterOption {
	return func(router *Router) {
		if registry != nil {
			router.utilities = registry
		}
	}
}

// UtilityFor returns the registered UtilityDefinition for an intent,
// or the zero value when none is registered. Used by the multi-turn
// follow-up code path in Process() so the pipeline can rebuild a
// Decision without re-running keyword resolution. v0.38.0.
func (r *Router) UtilityFor(intent shortcuts.Intent) UtilityDefinition {
	registry := DefaultUtilityRegistry()
	if r != nil && r.utilities != nil {
		registry = r.utilities
	}
	if def, ok := registry.Definition(intent); ok {
		return def
	}
	return UtilityDefinition{}
}

func (r *Router) Decide(transcript string, opts ProcessOpts) Decision {
	decision := Decision{
		Route:  RouteDirectReply,
		Locale: opts.Locale,
	}

	resolver := shortcuts.DefaultResolver()
	if r != nil && r.resolver != nil {
		resolver = r.resolver
	}

	resolution := resolver.Resolve(transcript, opts.Locale)
	// Preserve the security-sensitive Home Assistant intent even when its
	// utility is disabled. The pipeline uses it to fail closed instead of
	// treating a smart-home request as a generic prompt. Other disabled utility
	// intents retain the existing zero-value behavior.
	if resolution.Intent == shortcuts.IntentHomeAssistant {
		decision.Intent = resolution.Intent
	}
	registry := DefaultUtilityRegistry()
	if r != nil && r.utilities != nil {
		registry = r.utilities
	}
	utility, ok := registry.Definition(resolution.Intent)
	if resolution.Intent == shortcuts.IntentNone || !ok {
		return decision
	}

	decision.Route = RouteToolIntent
	decision.Intent = resolution.Intent
	decision.Utility = utility
	decision.Payload = resolution.Payload
	return decision
}
