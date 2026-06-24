package live

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestDeepgramParseConversationText(t *testing.T) {
	t.Parallel()
	p := &DeepgramLive{}

	userFrame := mustMarshal(t, map[string]any{
		"type":    "ConversationText",
		"role":    "user",
		"content": "what's the weather",
	})
	msg, swallow, err := p.parseEvent(userFrame)
	if err != nil || swallow {
		t.Fatalf("user ConversationText: swallow=%v err=%v", swallow, err)
	}
	if msg.InputTranscript != "what's the weather" || !msg.InputTranscriptDone {
		t.Errorf("user transcript mismatch: %+v", msg)
	}
	if msg.EventType != LiveEventInputFinal || msg.ProviderMetadata["provider_event"] != "ConversationText" {
		t.Fatalf("user event metadata mismatch: %+v", msg)
	}

	agentFrame := mustMarshal(t, map[string]any{
		"type":    "ConversationText",
		"role":    "assistant",
		"content": "it is sunny",
	})
	msg, swallow, err = p.parseEvent(agentFrame)
	if err != nil || swallow {
		t.Fatalf("assistant ConversationText: swallow=%v err=%v", swallow, err)
	}
	if msg.OutputTranscript != "it is sunny" || !msg.OutputTranscriptDone || msg.Text != "it is sunny" {
		t.Errorf("assistant transcript mismatch: %+v", msg)
	}
	if msg.EventType != LiveEventOutputText || msg.ProviderMetadata["provider_event"] != "ConversationText" {
		t.Fatalf("assistant event metadata mismatch: %+v", msg)
	}
}

func TestDeepgramParseControlEvents(t *testing.T) {
	t.Parallel()
	p := &DeepgramLive{}

	for _, swallowType := range []string{"Welcome", "SettingsApplied", "AgentThinking", "AgentStartedSpeaking", "History", "Warning"} {
		frame := mustMarshal(t, map[string]any{"type": swallowType})
		msg, swallow, err := p.parseEvent(frame)
		if err != nil {
			t.Fatalf("%s: unexpected err %v", swallowType, err)
		}
		if !swallow || msg != nil {
			t.Errorf("%s should be swallowed, got msg=%v swallow=%v", swallowType, msg, swallow)
		}
	}

	started, _, _ := p.parseEvent(mustMarshal(t, map[string]any{"type": "UserStartedSpeaking"}))
	if started == nil || !started.Interrupted || started.EventType != LiveEventInterrupted {
		t.Errorf("UserStartedSpeaking should set Interrupted, got %+v", started)
	}
	for _, typ := range []string{"StartOfTurn", "TurnResumed"} {
		turn, swallow, err := p.parseEvent(mustMarshal(t, map[string]any{"type": typ}))
		if err != nil || swallow || turn == nil || !turn.Interrupted || turn.EventType != LiveEventInterrupted {
			t.Errorf("%s should surface as interruption, msg=%+v swallow=%v err=%v", typ, turn, swallow, err)
		}
	}
	for _, typ := range []string{"EagerEndOfTurn", "EndOfTurn"} {
		turn, swallow, err := p.parseEvent(mustMarshal(t, map[string]any{"type": typ}))
		if err != nil || swallow || turn == nil || turn.EventType != LiveEventTurnEnd {
			t.Errorf("%s should surface as turn_end, msg=%+v swallow=%v err=%v", typ, turn, swallow, err)
		}
	}

	done, _, _ := p.parseEvent(mustMarshal(t, map[string]any{"type": "AgentAudioDone"}))
	if done == nil || !done.Done || done.EventType != LiveEventTurnEnd {
		t.Errorf("AgentAudioDone should set Done, got %+v", done)
	}
}

func TestDeepgramParseError(t *testing.T) {
	t.Parallel()
	p := &DeepgramLive{}
	frame := mustMarshal(t, map[string]any{
		"type":        "Error",
		"code":        "INVALID_AUTH",
		"description": "bad token",
	})
	_, _, err := p.parseEvent(frame)
	if err == nil {
		t.Fatal("Error event must surface a Go error")
	}
}

func TestDeepgramParseFunctionCall(t *testing.T) {
	t.Parallel()
	p := &DeepgramLive{}
	frame := mustMarshal(t, map[string]any{
		"type":             "FunctionCallRequest",
		"function_call_id": "call_1",
		"function_name":    "get_time",
		"input":            map[string]any{"zone": "UTC"},
	})
	msg, swallow, err := p.parseEvent(frame)
	if err != nil || swallow {
		t.Fatalf("FunctionCallRequest: swallow=%v err=%v", swallow, err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	if msg.EventType != LiveEventToolCall || msg.ProviderMetadata["provider_event"] != "FunctionCallRequest" {
		t.Fatalf("tool call event metadata mismatch: %+v", msg)
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "get_time" || tc.Args["zone"] != "UTC" {
		t.Errorf("tool call mismatch: %+v", tc)
	}
}

func TestDeepgramBuildSettingsManagedThink(t *testing.T) {
	t.Parallel()
	// Default provider: managed Deepgram LLM — provider.{type,model} present,
	// no endpoint block (Deepgram supplies the credential server-side).
	think := deepgramThinkFromSettings(t, (&DeepgramLive{}).buildSettings(LiveConfig{}))
	provider, ok := think["provider"].(map[string]any)
	if !ok {
		t.Fatalf("think.provider missing or wrong type: %#v", think["provider"])
	}
	if provider["type"] != deepgramThinkProviderDefault || provider["model"] != deepgramThinkModelDefault {
		t.Errorf("managed think provider mismatch: %#v", provider)
	}
	if _, hasEndpoint := think["endpoint"]; hasEndpoint {
		t.Errorf("managed think must not emit an endpoint block, got %#v", think["endpoint"])
	}

	// Explicit provider/model override without an endpoint stays managed.
	p := &DeepgramLive{}
	p.ConfigureThink("anthropic", "claude-haiku", "", "")
	think = deepgramThinkFromSettings(t, p.buildSettings(LiveConfig{}))
	provider = think["provider"].(map[string]any)
	if provider["type"] != "anthropic" || provider["model"] != "claude-haiku" {
		t.Errorf("configured managed think mismatch: %#v", provider)
	}
	if _, hasEndpoint := think["endpoint"]; hasEndpoint {
		t.Errorf("override without endpoint must stay managed, got %#v", think["endpoint"])
	}
}

func TestDeepgramBuildSettingsBYOThink(t *testing.T) {
	t.Parallel()
	// Bring-your-own think LLM: endpoint.url + Authorization: Bearer header,
	// per the Deepgram Voice Agent Settings schema. The credential lives on the
	// endpoint, never on the provider object.
	p := &DeepgramLive{}
	p.ConfigureThink("open_ai", "gpt-4o", "https://llm.example/v1/chat", "sk-byo-123")
	think := deepgramThinkFromSettings(t, p.buildSettings(LiveConfig{}))

	provider := think["provider"].(map[string]any)
	if provider["type"] != "open_ai" || provider["model"] != "gpt-4o" {
		t.Errorf("BYO provider mismatch: %#v", provider)
	}
	if _, leaked := provider["headers"]; leaked {
		t.Errorf("credential must not appear on the provider object: %#v", provider)
	}
	endpoint, ok := think["endpoint"].(map[string]any)
	if !ok {
		t.Fatalf("BYO think.endpoint missing: %#v", think)
	}
	if endpoint["url"] != "https://llm.example/v1/chat" {
		t.Errorf("endpoint.url mismatch: %#v", endpoint["url"])
	}
	headers, ok := endpoint["headers"].(map[string]any)
	if !ok || headers["authorization"] != "Bearer sk-byo-123" {
		t.Errorf("endpoint.headers.authorization mismatch: %#v", endpoint["headers"])
	}

	// Endpoint URL without a key still emits the endpoint (e.g. a credential-less
	// internal gateway) but omits the headers block.
	p = &DeepgramLive{}
	p.ConfigureThink("", "", "https://internal.gw/llm", "")
	think = deepgramThinkFromSettings(t, p.buildSettings(LiveConfig{}))
	endpoint = think["endpoint"].(map[string]any)
	if endpoint["url"] != "https://internal.gw/llm" {
		t.Errorf("keyless endpoint.url mismatch: %#v", endpoint["url"])
	}
	if _, hasHeaders := endpoint["headers"]; hasHeaders {
		t.Errorf("keyless endpoint must omit headers, got %#v", endpoint["headers"])
	}
}

func TestDeepgramBuildSettingsMapsProviderNeutralLiveOptions(t *testing.T) {
	t.Parallel()
	settings := (&DeepgramLive{}).buildSettings(LiveConfig{
		ProviderOptions: provideropts.Values{
			provideropts.OptionContextPrompt: "Use the customer CRM glossary.",
			provideropts.OptionLanguageHints: []string{"de", "en"},
			provideropts.OptionKeyterms:      []string{"Kombify"},
		},
	})
	think := deepgramThinkFromSettings(t, settings)
	if got := think["prompt"].(string); !strings.Contains(got, "Use the customer CRM glossary.") {
		t.Fatalf("think.prompt = %q", got)
	}
	listen := deepgramListenProviderFromSettings(t, settings)
	if got := strings.Join(listen["keyterms"].([]string), ","); got != "Kombify" {
		t.Fatalf("keyterms = %q", got)
	}
	if got := strings.Join(listen["language_hints"].([]string), ","); got != "de,en" {
		t.Fatalf("language_hints = %q", got)
	}
}

func TestDeepgramBuildSettingsOmitsAgentLanguageForFluxListen(t *testing.T) {
	t.Parallel()
	settings := (&DeepgramLive{}).buildSettings(LiveConfig{Locale: "en-US"})
	agent := settings["agent"].(map[string]any)
	if _, ok := agent["language"]; ok {
		t.Fatalf("Flux settings must omit deprecated agent.language: %#v", agent["language"])
	}
	listen := deepgramListenProviderFromSettings(t, settings)
	hints := listen["language_hints"].([]string)
	if strings.Join(hints, ",") != "en" {
		t.Fatalf("language_hints = %v, want en", hints)
	}
}

func TestDeepgramBuildSettingsKeepsAgentLanguageForNonFluxListen(t *testing.T) {
	t.Parallel()
	settings := (&DeepgramLive{ListenModel: "nova-3"}).buildSettings(LiveConfig{Locale: "de-DE"})
	agent := settings["agent"].(map[string]any)
	if got := agent["language"]; got != "de" {
		t.Fatalf("agent.language = %v, want de", got)
	}
}

// deepgramThinkFromSettings extracts agent.think from a buildSettings result.
func deepgramThinkFromSettings(t *testing.T, settings map[string]any) map[string]any {
	t.Helper()
	agent, ok := settings["agent"].(map[string]any)
	if !ok {
		t.Fatalf("settings.agent missing or wrong type: %#v", settings["agent"])
	}
	think, ok := agent["think"].(map[string]any)
	if !ok {
		t.Fatalf("agent.think missing or wrong type: %#v", agent["think"])
	}
	return think
}

func deepgramListenProviderFromSettings(t *testing.T, settings map[string]any) map[string]any {
	t.Helper()
	agent, ok := settings["agent"].(map[string]any)
	if !ok {
		t.Fatalf("settings.agent missing or wrong type: %#v", settings["agent"])
	}
	listen, ok := agent["listen"].(map[string]any)
	if !ok {
		t.Fatalf("agent.listen missing or wrong type: %#v", agent["listen"])
	}
	provider, ok := listen["provider"].(map[string]any)
	if !ok {
		t.Fatalf("agent.listen.provider missing or wrong type: %#v", listen["provider"])
	}
	return provider
}

func TestDeepgramResolveSpeakModel(t *testing.T) {
	t.Parallel()
	p := &DeepgramLive{}
	if got := p.resolveSpeakModel(LiveConfig{Locale: "de-DE"}); got != deepgramSpeakModelDefaultDE {
		t.Errorf("German locale: got %q, want %q", got, deepgramSpeakModelDefaultDE)
	}
	if got := p.resolveSpeakModel(LiveConfig{Locale: "en-US"}); got != deepgramSpeakModelDefaultEN {
		t.Errorf("English locale: got %q, want %q", got, deepgramSpeakModelDefaultEN)
	}
	if got := p.resolveSpeakModel(LiveConfig{Voice: "aura-2-apollo-en"}); got != "aura-2-apollo-en" {
		t.Errorf("Voice override: got %q, want aura-2-apollo-en", got)
	}
	configured := &DeepgramLive{SpeakModel: "aura-2-zeus-en"}
	if got := configured.resolveSpeakModel(LiveConfig{Locale: "de"}); got != "aura-2-zeus-en" {
		t.Errorf("configured SpeakModel should win over locale, got %q", got)
	}
}

func TestDeepgramAgentLanguage(t *testing.T) {
	t.Parallel()
	cases := map[string]string{"de-DE": "de", "en-US": "en", "de": "de", "auto": "", "": ""}
	for in, want := range cases {
		if got := deepgramAgentLanguage(in); got != want {
			t.Errorf("deepgramAgentLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestComposeDeepgramPrompt(t *testing.T) {
	t.Parallel()
	got := composeDeepgramPrompt(LiveConfig{FrameworkPrompt: "be concise", RefinementPrompt: "in German"})
	if got != "be concise\n\nin German" {
		t.Errorf("merged prompt mismatch: %q", got)
	}
	if got := composeDeepgramPrompt(LiveConfig{RefinementPrompt: "only this"}); got != "only this" {
		t.Errorf("refinement-only prompt mismatch: %q", got)
	}
}

func TestSplitKeyterms(t *testing.T) {
	t.Parallel()
	got := splitKeyterms("kombify, SpeechKit; Deepgram\nNova-3")
	want := []string{"kombify", "SpeechKit", "Deepgram", "Nova-3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("keyterm %d: got %q, want %q", i, got[i], want[i])
		}
	}
	if splitKeyterms("   ") != nil {
		t.Error("blank hint should yield nil")
	}
}
