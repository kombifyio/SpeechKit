package assist

import "github.com/kombifyio/SpeechKit/internal/shortcuts"

type Router struct {
	resolver *shortcuts.Resolver
}

type RouterOption func(*Router)

func NewRouter(opts ...RouterOption) *Router {
	router := &Router{
		resolver: shortcuts.DefaultResolver(),
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
	if resolution.Intent == shortcuts.IntentNone || !supportsToolIntent(resolution.Intent) {
		return decision
	}

	decision.Route = RouteToolIntent
	decision.Intent = resolution.Intent
	decision.Payload = resolution.Payload
	return decision
}

func supportsToolIntent(intent shortcuts.Intent) bool {
	switch intent {
	case shortcuts.IntentCopyLast, shortcuts.IntentInsertLast, shortcuts.IntentSummarize:
		return true
	default:
		return false
	}
}
