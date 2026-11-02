package templates

import (
	"strconv"
	"strings"
	"sync"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

// OperatorTermsID is the template that carries the vocabulary an operator
// configures for their own deployment ([[customization.terms]] in the server
// config). It is empty, and absent from the catalog, until terms are set, so
// the framework ships no product vocabulary of its own.
const OperatorTermsID = "operator-terms"

// OperatorTerm is one recognition term of the operator vocabulary. SoundsLike
// lists the mishearings that resolve back to Term as post-STT replacements.
type OperatorTerm struct {
	Term       string
	SoundsLike []string
	Language   string
}

var operatorTerms struct {
	sync.RWMutex
	terms []OperatorTerm
}

// SetOperatorTerms replaces the operator vocabulary. An empty list removes the
// template again.
func SetOperatorTerms(terms []OperatorTerm) {
	kept := make([]OperatorTerm, 0, len(terms))
	for _, term := range terms {
		term.Term = strings.TrimSpace(term.Term)
		if term.Term == "" {
			continue
		}
		term.Language = speechcustomize.NormalizeLanguage(term.Language)
		term.SoundsLike = append([]string(nil), term.SoundsLike...)
		kept = append(kept, term)
	}
	operatorTerms.Lock()
	operatorTerms.terms = kept
	operatorTerms.Unlock()
}

func operatorDefinition() (templateDefinition, bool) {
	operatorTerms.RLock()
	terms := append([]OperatorTerm(nil), operatorTerms.terms...)
	operatorTerms.RUnlock()
	if len(terms) == 0 {
		return templateDefinition{}, false
	}
	return templateDefinition{
		meta: Template{
			ID:          OperatorTermsID,
			Version:     VersionV1,
			Name:        "Operator terms",
			Description: "Product and domain vocabulary configured by the operator of this deployment.",
			Source:      source(OperatorTermsID, VersionV1),
			Modes:       []speechcustomize.Mode{speechcustomize.ModeDictation, speechcustomize.ModeAssist, speechcustomize.ModeVoiceAgent},
			BuiltIn:     true,
		},
		factory: func() speechcustomize.Pack { return operatorPack(terms) },
	}, true
}

func operatorPack(terms []OperatorTerm) speechcustomize.Pack {
	pack := speechcustomize.Pack{
		SchemaVersion: speechcustomize.PackSchemaVersion,
		ID:            OperatorTermsID,
		Name:          "Operator terms",
	}
	for i, term := range terms {
		w := word(OperatorTermsID, VersionV1, strconv.Itoa(i), term.Term, term.Language, templateTag(OperatorTermsID), term.SoundsLike...)
		w.Weight = 2
		pack.Words = append(pack.Words, w)
	}
	return pack
}
