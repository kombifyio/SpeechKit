package skills

import (
	"context"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/shortcuts"
)

// matcher is the catalog's assist.ToolMatcher: it resolves the transcript
// against the shortcuts Resolver and claims the utterance when the resolved
// intent is an enabled utility in the registry.
type matcher struct {
	resolver *shortcuts.Resolver
	registry *UtilityRegistry
}

// decision is the outcome of routing one transcript.
type decision struct {
	matched bool
	intent  shortcuts.Intent
	utility UtilityDefinition
	payload string
}

func (m *matcher) decide(text, locale string) decision {
	if m == nil || strings.TrimSpace(text) == "" {
		return decision{}
	}
	resolver := m.resolver
	if resolver == nil {
		resolver = shortcuts.DefaultResolver()
	}
	resolution := resolver.Resolve(text, locale)
	if resolution.Intent == shortcuts.IntentNone {
		return decision{}
	}
	utility, ok := m.registry.Definition(resolution.Intent)
	if !ok {
		return decision{}
	}
	return decision{
		matched: true,
		intent:  resolution.Intent,
		utility: utility,
		payload: resolution.Payload,
	}
}

// MatchTool implements assist.ToolMatcher. The returned ToolCall carries the
// full transcript, the host context and the output target so skills and host
// fallbacks see the same inputs a follow-up turn provides.
func (m *matcher) MatchTool(_ context.Context, req speechkit.AssistRequest) (assist.ToolCall, bool, error) {
	dec := m.decide(req.Text, req.Locale)
	if !dec.matched {
		return assist.ToolCall{}, false, nil
	}
	return assist.ToolCall{
		Intent:     string(dec.intent),
		Payload:    dec.payload,
		Transcript: req.Text,
		Locale:     req.Locale,
		Selection:  req.Selection,
		Context:    req.Context,
		Target:     req.Target,
	}, true, nil
}
