package shortcuts

import "strings"

type Resolver struct {
	registry *Registry
}

func NewResolver(registry *Registry) *Resolver {
	if registry == nil {
		registry = defaultRegistry
	}
	return &Resolver{registry: registry}
}

func DefaultResolver() *Resolver {
	return defaultResolver
}

func Resolve(text string) Resolution {
	return defaultResolver.Resolve(text, "")
}

func ResolveWithLocale(text, locale string) Resolution {
	return defaultResolver.Resolve(text, locale)
}

func (r *Resolver) Resolve(text, locale string) Resolution {
	if r == nil {
		return defaultResolver.Resolve(text, locale)
	}

	normalized := normalize(text)
	normalized = r.stripLeadingFillers(normalized, locale)
	if normalized == "" {
		return Resolution{}
	}

	for _, phrase := range r.registry.orderedPhrases(locale) {
		if alias, payload, ok := matchPhrase(normalized, phrase); ok {
			return Resolution{
				Intent:  phrase.intent,
				Alias:   alias,
				Payload: payload,
			}
		}
	}

	return Resolution{}
}

func (r *Resolver) stripLeadingFillers(text, locale string) string {
	for _, filler := range r.registry.orderedFillers(locale) {
		if text == filler.value {
			return ""
		}
		if strings.HasPrefix(text, filler.value+" ") {
			return strings.TrimSpace(strings.TrimPrefix(text, filler.value))
		}
	}
	return text
}

func matchPhrase(text string, phrase registeredPhrase) (string, string, bool) {
	alias := phrase.value
	if alias == "" {
		return "", "", false
	}

	if text == alias {
		return alias, "", true
	}

	if phrase.prefix {
		if strings.HasPrefix(text, alias+" ") {
			return alias, strings.TrimSpace(strings.TrimPrefix(text, alias)), true
		}

		if strings.HasPrefix(text, alias+",") || strings.HasPrefix(text, alias+":") || strings.HasPrefix(text, alias+".") || strings.HasPrefix(text, alias+"!") || strings.HasPrefix(text, alias+"?") {
			payload := strings.TrimLeft(text[len(alias):], " ,:.!?")
			return alias, payload, true
		}
	}

	return "", "", false
}

var defaultResolver = NewResolver(defaultRegistry)
