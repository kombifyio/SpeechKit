package shortcuts

import "strings"

// Resolver classifies transcripts against a Registry. The zero value is not
// usable; construct one with NewResolver or use DefaultResolver.
type Resolver struct {
	registry *Registry
}

// NewResolver returns a resolver over registry. A nil registry selects the
// built-in default catalog.
func NewResolver(registry *Registry) *Resolver {
	if registry == nil {
		registry = defaultRegistry
	}
	return &Resolver{registry: registry}
}

// DefaultResolver returns the process-wide resolver over the built-in
// catalog. It is shared; do not mutate the registry behind it.
func DefaultResolver() *Resolver {
	return defaultResolver
}

// Resolve classifies text with the default resolver and no locale
// preference.
func Resolve(text string) Resolution {
	return defaultResolver.Resolve(text, "")
}

// ResolveWithLocale classifies text with the default resolver, preferring
// phrases registered for locale.
func ResolveWithLocale(text, locale string) Resolution {
	return defaultResolver.Resolve(text, locale)
}

// Resolve normalises text, strips leading filler words registered for
// locale, and returns the first matching phrase's Resolution. A nil
// receiver resolves against the default catalog.
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

		if phrase.noSpacePrefix && strings.HasPrefix(text, alias) {
			payload := strings.TrimSpace(strings.TrimPrefix(text, alias))
			if payload != "" {
				return alias, payload, true
			}
		}
	}

	return "", "", false
}

var defaultResolver = NewResolver(defaultRegistry)
