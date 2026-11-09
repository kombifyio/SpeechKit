package customize

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	customtemplates "github.com/kombifyio/SpeechKit/app/internal/customize/templates"
)

// VocabularyOff is the vocabulary ref that turns the operator vocabulary off
// for a request (contract speechkit.voice_prefs.v2: the edge sends it for an
// account that chose no product vocabulary).
const VocabularyOff = "none"

type vocabularyRefKey struct{}

// WithVocabularyRef attaches a caller's vocabulary set ref to ctx, typically
// the signed x-speechkit-pref-vocabulary value a fronting edge proxy
// forwards; [Service.Resolve] honours it:
//
//   - "" (no choice) keeps the deployment default: the configured active
//     templates plus the operator vocabulary;
//   - [VocabularyOff] drops the operator vocabulary;
//   - a ref that names the operator vocabulary
//     ([customtemplates.IsOperatorVocabularyRef]) keeps it;
//   - a ref that names another builtin template activates that template
//     instead of the operator vocabulary;
//   - an unknown ref is a hint that cannot be satisfied and keeps the default.
func WithVocabularyRef(ctx context.Context, ref string) context.Context {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if ref == "" {
		return ctx
	}
	return context.WithValue(ctx, vocabularyRefKey{}, ref)
}

func vocabularyRefFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	ref, _ := ctx.Value(vocabularyRefKey{}).(string)
	return ref
}

// applyVocabularyRef returns the active template ids and whether the
// operator vocabulary joins them for ref.
func applyVocabularyRef(ids []string, ref string) ([]string, bool) {
	switch {
	case ref == "":
		return ids, true
	case ref == VocabularyOff:
		return ids, false
	case customtemplates.IsOperatorVocabularyRef(ref):
		return ids, true
	}
	if _, _, ok := customtemplates.ResolveTemplatePack(ref, ""); ok {
		if !slices.Contains(ids, ref) {
			ids = append(append([]string(nil), ids...), ref)
		}
		return ids, false
	}
	slog.Debug("customize: vocabulary preference names no known set; keeping the default", "ref", ref)
	return ids, true
}
