package skills_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
)

// newService wires the catalog into a real assist.Service so the tests exercise
// the exact matcher→executor path a host uses.
func newService(t *testing.T, opts skills.Options) *assist.Service {
	t.Helper()
	cat := skills.New(opts)
	svc, err := assist.NewService(assist.Options{
		Matcher:  cat.Matcher(),
		Executor: cat.Executor(),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestServiceAnswersTimeThroughRealSkills(t *testing.T) {
	svc := newService(t, skills.Options{})
	res, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "what time is it", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.HasPrefix(res.Text, "It is") {
		t.Errorf("time result = %q, want the real TimeSkill output (\"It is ...\")", res.Text)
	}
	if res.Surface != speechkit.AssistSurfacePanel {
		t.Errorf("surface = %q, want panel", res.Surface)
	}
	if res.ShortcutID != "time" {
		t.Errorf("shortcut id = %q, want time", res.ShortcutID)
	}
}

func TestServiceAnswersMathThroughRealSkills(t *testing.T) {
	svc := newService(t, skills.Options{})
	res, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "what is 2 plus 2", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(res.Text, "4") {
		t.Errorf("math result = %q, want it to contain 4", res.Text)
	}
}

func TestServiceConvertsTemperatureThroughRealSkills(t *testing.T) {
	svc := newService(t, skills.Options{})
	res, err := svc.Process(context.Background(),
		speechkit.AssistRequest{Text: "convert 20 celsius to fahrenheit", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(res.Text, "68 °F") {
		t.Errorf("temperature result = %q, want it to contain \"68 °F\"", res.Text)
	}
}

func TestMatcherIgnoresUnknownUtterance(t *testing.T) {
	cat := skills.New(skills.Options{})
	_, matched, err := cat.Matcher().MatchTool(context.Background(),
		speechkit.AssistRequest{Text: "tell me a long story about dragons", Locale: "en"})
	if err != nil {
		t.Fatalf("MatchTool: %v", err)
	}
	if matched {
		t.Error("a free-form request should not match a deterministic skill (host falls through to the LLM)")
	}
}

func TestMathSilentOnNonMathPayload(t *testing.T) {
	// "what is" routes to Math, but a non-math payload must come back silent so a
	// host can hand it to the LLM instead of answering with a bogus number.
	cat := skills.New(skills.Options{})
	call, matched, err := cat.Matcher().MatchTool(context.Background(),
		speechkit.AssistRequest{Text: "what is the capital of france", Locale: "en"})
	if err != nil || !matched {
		t.Fatalf("MatchTool matched=%v err=%v; expected a math match on \"what is\"", matched, err)
	}
	res, err := cat.Executor().ExecuteTool(context.Background(), call)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if res.Surface != speechkit.AssistSurfaceSilent {
		t.Errorf("surface = %q, want silent for a non-math payload", res.Surface)
	}
}

func TestSilentSkillFallsThroughToGenerator(t *testing.T) {
	cat := skills.New(skills.Options{})
	svc, err := assist.NewService(assist.Options{
		Matcher:  cat.Matcher(),
		Executor: cat.Executor(),
		Generator: assist.GenerateFunc(func(_ context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "Paris", SpeakText: "Paris", Surface: speechkit.AssistSurfacePanel, Locale: req.Locale}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	// "what is ..." matches Math, which goes silent on a non-math payload. The
	// Service must then fall through to the generator, not return an empty
	// silent result — otherwise delegating to this catalog would swallow the
	// query instead of letting the LLM answer.
	res, err := svc.Process(context.Background(),
		speechkit.AssistRequest{Text: "what is the capital of france", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(res.Text, "Paris") {
		t.Errorf("silent math should fall through to the generator; got %q", res.Text)
	}
}

func TestHomeAssistantBridgeForwardsCommand(t *testing.T) {
	var gotPath, gotBody string
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"speech":{"plain":{"speech":"Licht ist an."}},"response_type":"action_done"}}`))
	}))
	defer ha.Close()

	svc := newService(t, skills.Options{HomeAssistantURL: ha.URL, HomeAssistantToken: "test-token"})
	res, err := svc.Process(context.Background(),
		speechkit.AssistRequest{Text: "turn on the kitchen light", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.Text != "Licht ist an." {
		t.Errorf("HA result = %q, want the bridged HA speech", res.Text)
	}
	if gotPath != "/api/conversation/process" {
		t.Errorf("HA request path = %q, want /api/conversation/process", gotPath)
	}
	// The full command must reach HA, not just the payload after the trigger.
	if !strings.Contains(gotBody, "turn on the kitchen light") {
		t.Errorf("HA request body = %q, want the full command", gotBody)
	}
}

func TestHomeAssistantNoMatchAndFailureNeverReachGenerator(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		payload string
	}{
		{
			name:   "no intent match",
			status: http.StatusOK,
			payload: `{"response":{"speech":{"plain":{"speech":"I could not match that command."}},` +
				`"response_type":"error","data":{"code":"no_intent_match"}}}`,
		},
		{
			name:    "Home Assistant unavailable",
			status:  http.StatusServiceUnavailable,
			payload: `{"internal":"raw-body-must-not-escape"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer ha.Close()

			catalog := skills.New(skills.Options{HomeAssistantURL: ha.URL, HomeAssistantToken: "test-token"})
			var generatorCalls int
			svc, err := assist.NewService(assist.Options{
				Matcher:  catalog.Matcher(),
				Executor: catalog.Executor(),
				Generator: assist.GenerateFunc(func(_ context.Context, _ speechkit.AssistRequest) (speechkit.AssistResult, error) {
					generatorCalls++
					return speechkit.AssistResult{Text: "unsafe general model response"}, nil
				}),
			})
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			res, err := svc.Process(context.Background(), speechkit.AssistRequest{
				Text:   "turn on the unknown light",
				Locale: "en",
			})
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if generatorCalls != 0 {
				t.Fatalf("smart-home request reached the general generator %d times", generatorCalls)
			}
			if res.Surface == speechkit.AssistSurfaceSilent || strings.TrimSpace(res.Text) == "" ||
				strings.Contains(res.Text, "raw-body-must-not-escape") {
				t.Fatalf("Home Assistant failure result = %#v", res)
			}
		})
	}
}

func TestTimerFiresThroughServiceWithDefaultScheduler(t *testing.T) {
	fired := make(chan skills.Alarm, 1)
	cat := skills.New(skills.Options{OnAlarm: func(a skills.Alarm) { fired <- a }})
	t.Cleanup(cat.Close)
	svc, err := assist.NewService(assist.Options{Matcher: cat.Matcher(), Executor: cat.Executor()})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	res, err := svc.Process(context.Background(),
		speechkit.AssistRequest{Text: "set a timer for 1 second", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(res.Text, "Timer set for") {
		t.Errorf("confirmation = %q, want a \"Timer set for ...\" reply", res.Text)
	}

	// With the default scheduler the timer actually fires — no host scheduler.
	select {
	case a := <-fired:
		if a.Kind != "timer" {
			t.Errorf("alarm kind = %q, want timer", a.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timer did not fire within 3s")
	}
}

func TestTimerVerbalOnlyWithoutOnAlarm(t *testing.T) {
	// No OnAlarm → the timer still confirms verbally but nothing is scheduled.
	svc := newService(t, skills.Options{})
	res, err := svc.Process(context.Background(),
		speechkit.AssistRequest{Text: "set a timer for 5 minutes", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(res.Text, "Timer set for") {
		t.Errorf("confirmation = %q, want a verbal timer confirmation", res.Text)
	}
}

func TestHomeAssistantFailsClosedWhenUnconfigured(t *testing.T) {
	cat := skills.New(skills.Options{}) // no HA URL/token
	var generatorCalls int
	svc, err := assist.NewService(assist.Options{
		Matcher:  cat.Matcher(),
		Executor: cat.Executor(),
		Generator: assist.GenerateFunc(func(_ context.Context, _ speechkit.AssistRequest) (speechkit.AssistResult, error) {
			generatorCalls++
			return speechkit.AssistResult{Text: "unsafe general model response"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	res, err := svc.Process(context.Background(),
		speechkit.AssistRequest{Text: "turn on the kitchen light", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if generatorCalls != 0 {
		t.Fatalf("smart-home request reached the general generator %d times", generatorCalls)
	}
	if res.Surface == speechkit.AssistSurfaceSilent || !strings.Contains(res.Text, "not configured") {
		t.Fatalf("unconfigured Home Assistant result = %#v", res)
	}
	if res.MessageID != localization.CompanionHomeAssistantNotConfigured || res.ReasonCode != "not_configured" {
		t.Fatalf("unconfigured Home Assistant metadata = %q/%q", res.MessageID, res.ReasonCode)
	}
}
