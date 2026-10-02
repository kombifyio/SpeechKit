package templates

import (
	"testing"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func TestListTemplatesAndResolvePacks(t *testing.T) {
	list := ListTemplates()
	if len(list) < 2 {
		t.Fatalf("ListTemplates() = %+v, want built-in templates", list)
	}
	seen := map[string]Template{}
	for _, template := range list {
		seen[template.ID] = template
		if template.Source == "" || template.Version == "" {
			t.Fatalf("template metadata incomplete: %+v", template)
		}
		pack, _, ok := ResolveTemplatePack(template.ID, template.Version)
		if !ok {
			t.Fatalf("ResolveTemplatePack(%q, %q) failed", template.ID, template.Version)
		}
		if err := speechcustomize.ValidatePack(pack); err != nil {
			t.Fatalf("ValidatePack(%q): %v", template.ID, err)
		}
	}
	if !seen[StandardPunctuationID].DefaultActive {
		t.Fatalf("%s should be default active", StandardPunctuationID)
	}
	if seen[DeveloperID].DefaultActive {
		t.Fatalf("%s should be opt-in", DeveloperID)
	}
}

func TestNormalizeActiveTemplateIDs(t *testing.T) {
	if got := NormalizeActiveTemplateIDs(nil); len(got) != 1 || got[0] != StandardPunctuationID {
		t.Fatalf("NormalizeActiveTemplateIDs(nil) = %v", got)
	}
	got := NormalizeActiveTemplateIDs([]string{" developer-de-en ", "developer-de-en", "standard-punctuation-de-en"})
	want := []string{DeveloperID, StandardPunctuationID}
	if len(got) != len(want) {
		t.Fatalf("NormalizeActiveTemplateIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeActiveTemplateIDs = %v, want %v", got, want)
		}
	}
}
