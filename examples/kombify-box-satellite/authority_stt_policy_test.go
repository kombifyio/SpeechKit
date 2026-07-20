package main

import "testing"

func TestRealtimeHAAuthoritySTTPolicyIsLocalOnly(t *testing.T) {
	for _, provider := range []string{"local", "whispercpp", "whisper.cpp", "stt.local.whispercpp", " LOCAL "} {
		if !localAuthoritySTTProvider(provider) {
			t.Fatalf("local provider %q was rejected", provider)
		}
	}
	for _, provider := range []string{"", "deepgram", "assemblyai", "openai", "gateway", "vps", "ollama"} {
		if localAuthoritySTTProvider(provider) {
			t.Fatalf("remote or ambiguous provider %q was accepted", provider)
		}
	}
}
