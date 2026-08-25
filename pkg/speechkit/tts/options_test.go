package tts

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestResolveSynthesizeOptionsPrecedence(t *testing.T) {
	resolved := ResolveSynthesizeOptions("openai", "", SynthesizeOpts{
		Voice:  "request-voice",
		Format: "wav",
		Options: provideropts.Values{
			provideropts.OptionVoice:       "global-voice",
			provideropts.OptionSpeed:       1.2,
			provideropts.OptionAudioFormat: "mp3",
		},
	}, provideropts.Values{
		provideropts.OptionVoice: "provider-default",
	}, provideropts.Values{
		provideropts.OptionSpeed: 0.9,
	})

	if resolved.Voice != "request-voice" {
		t.Fatalf("Voice = %q, want request override", resolved.Voice)
	}
	if resolved.Format != "wav" {
		t.Fatalf("Format = %q, want request override", resolved.Format)
	}
	if resolved.Speed != 0.9 {
		t.Fatalf("Speed = %v, want provider override", resolved.Speed)
	}
}

func TestResolveSynthesizeOptionsReportsUnsupportedGlobal(t *testing.T) {
	resolved := ResolveSynthesizeOptions("deepgram", "tts.deepgram.aura-2", SynthesizeOpts{
		Options: provideropts.Values{
			provideropts.OptionSpeed: 1.4,
		},
	}, nil, nil)

	if len(resolved.Effective.Unsupported) != 1 {
		t.Fatalf("Unsupported = %+v, want speed report", resolved.Effective.Unsupported)
	}
	if resolved.Effective.Unsupported[0].ID != provideropts.OptionSpeed {
		t.Fatalf("Unsupported ID = %q, want speed", resolved.Effective.Unsupported[0].ID)
	}
}

func TestResolveSynthesizeOptionsHuggingFaceAndPiperAreSupported(t *testing.T) {
	for _, provider := range []string{"huggingface", "piper"} {
		resolved := ResolveSynthesizeOptions(provider, "", SynthesizeOpts{
			Locale: "de-DE",
			Voice:  "test-voice",
			Options: provideropts.Values{
				provideropts.OptionSpeed: 1.2,
			},
		}, nil, nil)
		if resolved.Locale != "de-DE" {
			t.Fatalf("%s locale = %q", provider, resolved.Locale)
		}
		if resolved.Voice != "test-voice" {
			t.Fatalf("%s voice = %q", provider, resolved.Voice)
		}
		for _, report := range resolved.Effective.Unsupported {
			if report.ID == provideropts.OptionLanguage || report.ID == provideropts.OptionVoice {
				t.Fatalf("%s reported %s unsupported: %#v", provider, report.ID, report)
			}
		}
	}
}
