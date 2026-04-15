package shortcuts

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func normalize(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}

	decomposed := norm.NFD.String(text)
	var builder strings.Builder
	builder.Grow(len(decomposed))

	lastSpace := false
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			builder.WriteRune(r)
			lastSpace = false
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			if !lastSpace {
				builder.WriteByte(' ')
				lastSpace = true
			}
		default:
			if !lastSpace {
				builder.WriteByte(' ')
				lastSpace = true
			}
		}
	}

	fields := strings.Fields(builder.String())
	return strings.Join(fields, " ")
}

func normalizeLocaleKey(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	locale = strings.ReplaceAll(locale, "_", "-")
	if locale == "" {
		return "default"
	}
	return locale
}

func localeChain(locale string) ([]string, bool) {
	normalized := normalizeLocaleKey(locale)
	if normalized == "default" {
		return nil, true
	}

	chain := []string{normalized}
	if idx := strings.IndexByte(normalized, '-'); idx > 0 {
		base := normalized[:idx]
		if base != "" && base != normalized {
			chain = append(chain, base)
		}
	}
	chain = append(chain, "default")
	return chain, false
}

func localeRank(locale string, chain []string) int {
	for idx, candidate := range chain {
		if locale == candidate {
			return idx
		}
	}
	return -1
}
