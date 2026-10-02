package store

import "strings"

func normalizeTranscriptionModelHints(hints map[string]string) map[string]string {
	if len(hints) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(hints))
	for provider, model := range hints {
		provider = strings.TrimSpace(strings.ToLower(provider))
		model = strings.TrimSpace(model)
		if provider == "" || model == "" {
			continue
		}
		normalized[provider] = model
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
