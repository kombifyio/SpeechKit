package stt

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestResolveTranscribeOptionsPrecedence(t *testing.T) {
	resolved := ResolveTranscribeOptions("deepgram", "stt.deepgram.nova-3", TranscribeOpts{
		Language: "it",
		Options: provideropts.Values{
			provideropts.OptionLanguage:       "en",
			provideropts.OptionVocabularyBias: true,
		},
	}, provideropts.Values{
		provideropts.OptionLanguage: "de",
	}, provideropts.Values{
		provideropts.OptionLanguage: "fr",
	})

	if resolved.Language != "it" {
		t.Fatalf("Language = %q, want request override", resolved.Language)
	}
	if got := resolved.Effective.Options[provideropts.OptionLanguage].Source; got != provideropts.SourceRequestOverride {
		t.Fatalf("language source = %q", got)
	}
}

func TestResolveTranscribeOptionsProviderOverrideBeatsGlobalLanguage(t *testing.T) {
	resolved := ResolveTranscribeOptions("openai", "", TranscribeOpts{
		Options: provideropts.Values{
			provideropts.OptionLanguage: "en",
		},
	}, nil, provideropts.Values{
		provideropts.OptionLanguage: "de",
	})

	if resolved.Language != "de" {
		t.Fatalf("Language = %q, want provider override", resolved.Language)
	}
	if got := resolved.Effective.Options[provideropts.OptionLanguage].Source; got != provideropts.SourceProviderOverride {
		t.Fatalf("language source = %q", got)
	}
}

func TestResolveTranscribeOptionsGlobalLanguageBeatsProviderDefault(t *testing.T) {
	resolved := ResolveTranscribeOptions("openai", "", TranscribeOpts{
		Options: provideropts.Values{
			provideropts.OptionLanguage: "en",
		},
	}, provideropts.Values{
		provideropts.OptionLanguage: "fr",
	}, nil)

	if resolved.Language != "en" {
		t.Fatalf("Language = %q, want global default", resolved.Language)
	}
	if got := resolved.Effective.Options[provideropts.OptionLanguage].Source; got != provideropts.SourceGlobalDefault {
		t.Fatalf("language source = %q", got)
	}
}
