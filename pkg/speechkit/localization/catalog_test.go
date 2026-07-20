package localization

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/language"
)

func TestCatalogsContainExactMessageSet(t *testing.T) {
	wantLocales := []string{"en", "de", "es", "zh-Hans", "hi", "ar"}
	if got := SupportedLocales(); !reflect.DeepEqual(got, wantLocales) {
		t.Fatalf("SupportedLocales() = %v, want %v", got, wantLocales)
	}
	for _, locale := range wantLocales {
		t.Run(locale, func(t *testing.T) {
			if _, err := language.Parse(locale); err != nil {
				t.Fatalf("registered locale is not BCP-47: %v", err)
			}
			catalog := catalogs[locale]
			if len(catalog) != len(messageIDs) {
				t.Fatalf("catalog contains %d messages, want %d", len(catalog), len(messageIDs))
			}
			for _, id := range messageIDs {
				if strings.TrimSpace(catalog[id]) == "" {
					t.Errorf("message %q is empty", id)
				}
				if locale != "en" && catalog[id] == catalogs["en"][id] {
					t.Errorf("message %q leaked the English source into %s", id, locale)
				}
			}
		})
	}
}

func TestResolveLocaleUsesDeterministicFallbacks(t *testing.T) {
	tests := []struct {
		locale string
		want   string
	}{
		{locale: "en-US", want: "en"},
		{locale: "de-DE", want: "de"},
		{locale: "es-MX", want: "es"},
		{locale: "zh-Hans-CN", want: "zh-Hans"},
		{locale: "zh-CN", want: "zh-Hans"},
		{locale: "hi-IN", want: "hi"},
		{locale: "ar-EG", want: "ar"},
		{locale: "zh-Hant", want: "en"},
		{locale: "zh-Latn", want: "en"},
		{locale: "zh-Bopo", want: "en"},
		{locale: "zh-TW", want: "en"},
		{locale: "fr-FR", want: "en"},
		{locale: "not a locale", want: "en"},
		{locale: "", want: "en"},
	}
	for _, tc := range tests {
		t.Run(tc.locale, func(t *testing.T) {
			if got := ResolveLocale(tc.locale); got != tc.want {
				t.Fatalf("ResolveLocale(%q) = %q, want %q", tc.locale, got, tc.want)
			}
		})
	}
}

func TestResolveReturnsTextAndNegotiatedLocaleTogether(t *testing.T) {
	got := Resolve("es-MX", CompanionHomeAssistantRejected)
	if got.ID != CompanionHomeAssistantRejected || got.Locale != "es" || got.Text != catalogs["es"][got.ID] {
		t.Fatalf("Resolve() = %#v", got)
	}
	got = Resolve("zh-Latn", CompanionHomeAssistantRejected)
	if got.Locale != "en" || got.Text != catalogs["en"][got.ID] {
		t.Fatalf("unsupported script Resolve() = %#v", got)
	}
}

func TestTextUsesEnglishFinalFallback(t *testing.T) {
	want := catalogs["en"][CompanionHomeAssistantUnavailable]
	if got := Text("fr-FR", CompanionHomeAssistantUnavailable); got != want {
		t.Fatalf("unsupported locale text = %q, want English fallback %q", got, want)
	}
	if got := Text("en", MessageID("unknown.message")); got != "" {
		t.Fatalf("unknown ID text = %q, want empty", got)
	}
}

func TestDecodeCatalogRejectsDuplicateAndUnknownIDs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "duplicate",
			raw:  `{"companion.home_assistant.not_configured":"one","companion.home_assistant.not_configured":"two"}`,
			want: "duplicate message ID",
		},
		{
			name: "unknown",
			raw:  `{"companion.home_assistant.unknown":"text"}`,
			want: "unknown message ID",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeCatalog([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decodeCatalog() error = %v, want %q", err, tc.want)
			}
		})
	}
}
