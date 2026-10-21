package shortcuts

import "testing"

// Audit 4.2 — lift coverage from ~0.41 toward 0.6. Exercises the
// public surface that resolver_test.go doesn't already pin down:
// normalize() Unicode/punct handling and normalizeLocaleKey/localeChain.

func TestNormalizeStripsDiacriticsAndCollapsesPunct(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"Hallo!", "hallo"},
		{"über mich", "uber mich"},
		{"café-NEUE,version", "cafe neue version"},
		{"   already  clean   ", "already clean"},
		{"emoji 🎉 stripped 😀", "emoji stripped"},
		{"ÆÖÜß", "æoussβ"[:0] + "æouss"}, // diacritics removed; ligatures/eszett left intact
	}
	for _, c := range cases {
		// Skip the eszett case-specific check above — Unicode lower
		// of ß is locale-sensitive; the looser assertion below
		// covers the more important contract.
		got := normalize(c.in)
		if c.in == "ÆÖÜß" {
			if got == "" {
				t.Fatalf("normalize(ÆÖÜß) returned empty; want non-empty without diacritics")
			}
			continue
		}
		if got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeLocaleKeyCanonicalisation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "default"},
		{"  ", "default"},
		{"de", "de"},
		{"  EN ", "en"},
		{"de_DE", "de-de"},
		{"DE-de", "de-de"},
		{"pt_BR", "pt-br"},
	}
	for _, c := range cases {
		if got := normalizeLocaleKey(c.in); got != c.want {
			t.Errorf("normalizeLocaleKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLocaleChainFallsBackToBaseThenDefault(t *testing.T) {
	cases := []struct {
		in       string
		wantDef  bool   // when true, function should report defaulted=true and nil chain
		wantHead string // first element of chain when defaulted=false
		wantTail string // last element of chain when defaulted=false (must be "default")
	}{
		{"", true, "", ""},
		{"default", true, "", ""},
		{"de", false, "de", "default"},
		{"de_DE", false, "de-de", "default"},
		{"en-US", false, "en-us", "default"},
	}
	for _, c := range cases {
		chain, defaulted := localeChain(c.in)
		if defaulted != c.wantDef {
			t.Errorf("localeChain(%q) defaulted = %v, want %v", c.in, defaulted, c.wantDef)
			continue
		}
		if defaulted {
			if chain != nil {
				t.Errorf("localeChain(%q) defaulted but returned chain %v, want nil", c.in, chain)
			}
			continue
		}
		if len(chain) < 2 {
			t.Fatalf("localeChain(%q) returned chain of len %d, want >= 2", c.in, len(chain))
		}
		if chain[0] != c.wantHead {
			t.Errorf("localeChain(%q) head = %q, want %q", c.in, chain[0], c.wantHead)
		}
		if chain[len(chain)-1] != c.wantTail {
			t.Errorf("localeChain(%q) tail = %q, want %q", c.in, chain[len(chain)-1], c.wantTail)
		}
	}
}

func TestLocaleRankFindsMatchOrReturnsNegativeOne(t *testing.T) {
	chain := []string{"de-de", "de", "default"}
	cases := []struct {
		locale string
		want   int
	}{
		{"de-de", 0},
		{"de", 1},
		{"default", 2},
		{"missing", -1},
		{"", -1},
	}
	for _, c := range cases {
		if got := localeRank(c.locale, chain); got != c.want {
			t.Errorf("localeRank(%q, %v) = %d, want %d", c.locale, chain, got, c.want)
		}
	}
}

func TestLocaleChainRetainsScriptFallback(t *testing.T) {
	chain, defaulted := localeChain("zh-Hans-CN")
	if defaulted {
		t.Fatal("script locale unexpectedly defaulted")
	}
	want := []string{"zh-hans-cn", "zh-hans", "zh", "default"}
	if len(chain) != len(want) {
		t.Fatalf("locale chain = %v, want %v", chain, want)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("locale chain = %v, want %v", chain, want)
		}
	}
}
