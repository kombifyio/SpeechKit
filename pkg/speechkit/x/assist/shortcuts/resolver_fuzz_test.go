package shortcuts

import "testing"

func FuzzResolve(f *testing.F) {
	f.Add("", "")
	f.Add("copy that", "en")
	f.Add("  Bitte kopiere das  ", "de-DE")
	f.Add("\xff\xfe\x00 invalid utf8", "zh-Hans")
	f.Add("insert summary of the meeting", "ar")

	f.Fuzz(func(t *testing.T, text, locale string) {
		first := ResolveWithLocale(text, locale)
		second := ResolveWithLocale(text, locale)
		if first != second {
			t.Fatalf("resolution is not deterministic: %+v vs %+v", first, second)
		}
	})
}
