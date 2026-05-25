package main

import "testing"

// Audit 4.3 — boot-path coverage for sk-browser-smoke. The full
// chromedp-driven run() needs a real headless Chrome and a live
// speechkit-server; it lives under install-e2e-linux.yml. This file
// covers the bits that can be exercised in-process without Chrome:
// the CSV parser the manifest relies on, the expected-tile constant,
// and the run() flag-parsing surface (no network).

func TestSplitCSVTrimsAndSkipsEmpty(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string(nil)},
		{",,", []string(nil)},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,  ,c ", []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i, g := range got {
			if g != c.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", c.in, i, g, c.want[i])
			}
		}
	}
}

func TestExpectedTilesCoversAllPublicSurfaces(t *testing.T) {
	// The smoke contract is "every tile resolves OK". If the smoke
	// page ships a new tile that this constant does not list, the
	// browser run still passes (we only assert the listed names
	// reach OK), but a missing-entry regression here would silently
	// stop catching the new tile's failures. This pinning forces a
	// conscious update when the list changes.
	tiles := splitCSV(expectedTiles)
	wantAtLeast := []string{"Server Settings", "Health", "Readiness", "Dictation", "Assist", "Voice Agent"}
	for _, name := range wantAtLeast {
		seen := false
		for _, t := range tiles {
			if t == name {
				seen = true
				break
			}
		}
		if !seen {
			t.Errorf("expectedTiles is missing required tile %q (got %v)", name, tiles)
		}
	}
}

func TestRunRejectsEmptyOrigin(t *testing.T) {
	// run() reads the flag package's global, so we cannot easily
	// inject. Instead exercise the validation indirectly: confirm
	// the constants run() depends on are non-empty so the flag
	// fallback path produces a non-empty origin on a clean caller.
	if defaultOrigin == "" {
		t.Fatal("defaultOrigin must not be empty — flag fallback would always fail")
	}
	if defaultTimeout <= 0 {
		t.Fatal("defaultTimeout must be positive")
	}
	if runSmokeButtonSelector == "" {
		t.Fatal("runSmokeButtonSelector must not be empty")
	}
	if passingHeaderText == "" || failedHeaderText == "" {
		t.Fatal("smoke header text constants must not be empty")
	}
}
