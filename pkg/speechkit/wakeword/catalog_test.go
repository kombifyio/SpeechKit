package wakeword

import "testing"

func TestLookupPhraseResolvesCatalogIDsAndLegacyAlias(t *testing.T) {
	// "hey_quby" is the pre-rename ID of the brand default; configs in the
	// wild still carry it and must keep resolving to the hey_kubi entry.
	for _, id := range []string{"hey_kubi", " Hey_Kubi ", "hey_quby"} {
		entry := LookupPhrase(id)
		if entry == nil {
			t.Fatalf("LookupPhrase(%q) = nil, want the hey_kubi entry", id)
		}
		if entry.ID != "hey_kubi" || entry.KeywordLabel != "hey_quby" {
			t.Fatalf("LookupPhrase(%q) = %+v, want ID hey_kubi with KeywordLabel hey_quby", id, *entry)
		}
	}
	if LookupPhrase("") != nil || LookupPhrase("hey_nobody") != nil {
		t.Fatal("LookupPhrase resolved an empty or unknown ID")
	}

	seen := map[string]bool{}
	for _, e := range DefaultCatalog() {
		if seen[e.ID] {
			t.Fatalf("duplicate catalog ID %q", e.ID)
		}
		seen[e.ID] = true
		if got := LookupPhrase(e.ID); got == nil || got.ID != e.ID || got.KeywordLabel == "" {
			t.Fatalf("LookupPhrase(%q) = %+v, want the catalog entry with a keyword label", e.ID, got)
		}
	}
}
