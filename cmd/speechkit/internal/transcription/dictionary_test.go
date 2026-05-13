package transcription

import "testing"

func TestBuildVoiceAgentVocabularyHintUsesCanonicalTerms(t *testing.T) {
	entries := ParseVocabularyDictionary("kombi fire => Kombify\nAcmeOS\nGemma\nacmeos")

	if got, want := BuildVoiceAgentVocabularyHint(entries), "Prefer these names and product terms in recognition and responses: Kombify, AcmeOS, Gemma."; got != want {
		t.Fatalf("BuildVoiceAgentVocabularyHint() = %q, want %q", got, want)
	}
}
