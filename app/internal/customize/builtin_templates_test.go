package customize

import (
	"testing"

	customtemplates "github.com/kombifyio/SpeechKit/app/internal/customize/templates"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

// The built-in templates turn spoken symbol names into symbols. The developer
// template once also triggered on everyday words and replaced them in ordinary
// dictation ("nicht gleich nur" became "nicht = nur"); such sentences must
// come through with every word.
func TestBuiltInTemplatesKeepOrdinaryWords(t *testing.T) {
	var replacements []speechcustomize.Replacement
	for _, template := range customtemplates.ListTemplates() {
		pack, _, ok := customtemplates.ResolveTemplatePack(template.ID, template.Version)
		if !ok {
			t.Fatalf("ResolveTemplatePack(%q) failed", template.ID)
		}
		replacements = append(replacements, pack.Replacements...)
	}
	for _, said := range []string{
		"Das ist nicht gleich nur ein Detail",
		"Ein wichtiger Punkt ist die Latenz",
		"Wohin der Pfeil geht ist klar",
		"That equals more work for the arrow keys",
	} {
		got, err := Apply(said, replacements, speechcustomize.StagePostSTT)
		if err != nil {
			t.Fatalf("Apply(%q): %v", said, err)
		}
		if got.Text != said {
			t.Fatalf("%q became %q", said, got.Text)
		}
	}
}
