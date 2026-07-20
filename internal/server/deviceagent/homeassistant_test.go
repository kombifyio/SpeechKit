package deviceagent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func TestHomeAssistantConversationIsLocalOpaqueAndCredentialed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != homeAssistantConversationPath || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ha-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["text"] != "mach das licht aus" || body["language"] != "de" || body["agent_id"] != "conversation.home_assistant" {
			t.Fatalf("body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"conversation_id": "conversation-1",
			"response": map[string]any{
				"response_type": "action_done",
				"language":      "de",
				"speech":        map[string]any{"plain": map[string]string{"speech": "Das Licht ist aus."}},
				"data": map[string]any{
					"success": []map[string]string{{"type": "entity", "id": "light.kitchen", "name": "Kitchen light"}},
					"failed":  []map[string]string{},
				},
			},
		})
	}))
	defer server.Close()
	client, err := NewHomeAssistantClient(HomeAssistantOptions{
		BaseURL: server.URL,
		Token:   "ha-token",
		AgentID: "conversation.home_assistant",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := client.Converse(t.Context(), "mach das licht aus", "de-DE")
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if result.Speech != "Das Licht ist aus." || result.ConversationID != "conversation-1" || result.Language != "de" || result.ActionExecuted != "unknown" || result.ReasonCode != "ha_state_verification_required" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.SuccessTargets) != 1 || result.SuccessTargets[0].Type != "entity" || result.SuccessTargets[0].ID != "light.kitchen" || result.SuccessTargets[0].Name != "Kitchen light" || len(result.FailedTargets) != 0 {
		t.Fatalf("targets = success %#v failed %#v", result.SuccessTargets, result.FailedTargets)
	}
}

func TestHomeAssistantClientRejectsPublicTarget(t *testing.T) {
	_, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: "https://8.8.8.8", Token: "ha-token"})
	if !errors.Is(err, netsec.ErrPublicBlocked) {
		t.Fatalf("New error = %v", err)
	}
}

func TestHomeAssistantClientRequiresHTTPSOutsideLiteralLoopback(t *testing.T) {
	for _, rawURL := range []string{
		"http://192.168.10.20:8123",
		"http://homeassistant.local:8123",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: rawURL, Token: "ha-token"})
			if !errors.Is(err, netsec.ErrInsecureHTTP) {
				t.Fatalf("New error = %v, want ErrInsecureHTTP", err)
			}
		})
	}

	if _, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: "https://192.168.10.20:8123", Token: "ha-token"}); err != nil {
		t.Fatalf("private HTTPS target rejected: %v", err)
	}
	if _, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: "http://localhost:8123", Token: "ha-token"}); err != nil {
		t.Fatalf("literal localhost HTTP target rejected: %v", err)
	}
}

func TestHomeAssistantClientRejectsNonOriginOrUnsafeCredentialOptions(t *testing.T) {
	for _, tc := range []HomeAssistantOptions{
		{BaseURL: "http://localhost:8123/api", Token: "ha-token"},
		{BaseURL: "http://localhost:8123?target=elsewhere", Token: "ha-token"},
		{BaseURL: "http://localhost:8123#fragment", Token: "ha-token"},
		{BaseURL: "http://localhost:8123", Token: "ha-token\r\nInjected: value"},
		{BaseURL: "http://localhost:8123", Token: "ha-token", AgentID: "conversation\nother"},
		{BaseURL: "http://localhost:8123", Token: "ha-token", Language: "en/../../"},
	} {
		if _, err := NewHomeAssistantClient(tc); err == nil {
			t.Fatalf("NewHomeAssistantClient accepted unsafe options: %#v", tc)
		}
	}
}

func TestHomeAssistantClientRejectsRedirectWithoutForwardingToken(t *testing.T) {
	redirectCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		redirectCalls++
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: source.URL, Token: "ha-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.Converse(t.Context(), "turn off", "en")
	var dispatchErr *HomeAssistantDispatchError
	if !errors.As(err, &dispatchErr) || dispatchErr.ActionExecuted != "unknown" {
		t.Fatalf("Converse error = %#v", err)
	}
	if redirectCalls != 0 {
		t.Fatalf("redirect target calls = %d", redirectCalls)
	}
}

func TestHomeAssistantHTTPFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		wantAction string
	}{
		{name: "unauthorized definitive", status: http.StatusUnauthorized, wantAction: "no"},
		{name: "unprocessable definitive", status: http.StatusUnprocessableEntity, wantAction: "no"},
		{name: "conflict ambiguous", status: http.StatusConflict, wantAction: "unknown"},
		{name: "rate limit ambiguous", status: http.StatusTooManyRequests, wantAction: "unknown"},
		{name: "server failure ambiguous", status: http.StatusInternalServerError, wantAction: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: server.URL, Token: "ha-token"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = client.Converse(t.Context(), "turn off", "en")
			var dispatchErr *HomeAssistantDispatchError
			if !errors.As(err, &dispatchErr) || dispatchErr.ActionExecuted != tc.wantAction {
				t.Fatalf("error = %#v, want action %q", err, tc.wantAction)
			}
		})
	}
}

func TestHomeAssistantConversationOutcomeClassificationIsConservative(t *testing.T) {
	tests := []struct {
		name          string
		responseType  string
		code          string
		success       []map[string]string
		failed        []map[string]string
		wantAction    string
		wantReason    string
		wantSuccesses int
		wantFailures  int
	}{
		{
			name: "verified action target", responseType: "action_done",
			success:    []map[string]string{{"type": "entity", "id": "light.kitchen", "name": "Kitchen light"}},
			wantAction: "unknown", wantReason: "ha_state_verification_required", wantSuccesses: 1,
		},
		{name: "action without targets is unknown", responseType: "action_done", wantAction: "unknown", wantReason: "ha_action_outcome_unverified"},
		{
			name: "partial action is unknown", responseType: "action_done",
			success:    []map[string]string{{"type": "entity", "id": "light.kitchen", "name": "Kitchen light"}},
			failed:     []map[string]string{{"type": "entity", "id": "light.office", "name": "Office light"}},
			wantAction: "unknown", wantReason: "ha_action_outcome_unverified", wantSuccesses: 1, wantFailures: 1,
		},
		{name: "no intent match is definite", responseType: "error", code: "no_intent_match", wantAction: "no", wantReason: "ha_no_intent_match"},
		{name: "no valid targets is definite", responseType: "error", code: "no_valid_targets", wantAction: "no", wantReason: "ha_no_valid_targets"},
		{name: "failed handling is unknown", responseType: "error", code: "failed_to_handle", wantAction: "unknown", wantReason: "ha_failed_to_handle"},
		{name: "explicit unknown is unknown", responseType: "error", code: "unknown", wantAction: "unknown", wantReason: "ha_unknown"},
		{name: "missing error code is unknown", responseType: "error", wantAction: "unknown", wantReason: "ha_conversation_error_unknown"},
		{name: "query is not an action", responseType: "query_answer", wantAction: "not_applicable"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"response_type": tc.responseType,
						"language":      "en",
						"speech":        map[string]any{"plain": map[string]string{"speech": "Home Assistant response"}},
						"data": map[string]any{
							"code":    tc.code,
							"success": tc.success,
							"failed":  tc.failed,
						},
					},
				})
			}))
			defer server.Close()
			client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: server.URL, Token: "ha-token"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			result, err := client.Converse(t.Context(), "test command", "en")
			if err != nil {
				t.Fatalf("Converse: %v", err)
			}
			if result.ActionExecuted != tc.wantAction || result.ReasonCode != tc.wantReason {
				t.Fatalf("result = %#v, want action=%q reason=%q", result, tc.wantAction, tc.wantReason)
			}
			if len(result.SuccessTargets) != tc.wantSuccesses || len(result.FailedTargets) != tc.wantFailures {
				t.Fatalf("targets = success %#v failed %#v", result.SuccessTargets, result.FailedTargets)
			}
		})
	}
}

func TestHomeAssistantConversationRejectsMalformedSuccessTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"response_type": "action_done",
				"language":      "en",
				"data": map[string]any{
					"success": []map[string]string{{}},
					"failed":  []map[string]string{},
				},
			},
		})
	}))
	defer server.Close()
	client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: server.URL, Token: "ha-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.Converse(t.Context(), "turn off", "en")
	var dispatchErr *HomeAssistantDispatchError
	if !errors.As(err, &dispatchErr) || dispatchErr.ActionExecuted != "unknown" || dispatchErr.ReasonCode != "ha_response_indeterminate" {
		t.Fatalf("Converse error = %#v", err)
	}
}

func TestHomeAssistantVerifyStateMatchesExactEntityAndState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/states/light.kitchen" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ha-token" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entity_id": "light.kitchen",
			"state":     "off",
			"attributes": map[string]any{
				"friendly_name": "Kitchen light",
			},
		})
	}))
	defer server.Close()
	client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: server.URL, Token: "ha-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := client.VerifyState(t.Context(), "light.kitchen", "off"); err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
}

func TestHomeAssistantVerifyStateFailsClosedOnMismatch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		entityID   string
		state      string
		wantReason string
	}{
		{name: "state mismatch", entityID: "light.kitchen", state: "on", wantReason: "ha_state_mismatch"},
		{name: "entity mismatch", entityID: "light.office", state: "off", wantReason: "ha_state_identity_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{"entity_id": tc.entityID, "state": tc.state})
			}))
			defer server.Close()
			client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: server.URL, Token: "ha-token"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			err = client.VerifyState(t.Context(), "light.kitchen", "off")
			var dispatchErr *HomeAssistantDispatchError
			if !errors.As(err, &dispatchErr) || dispatchErr.ReasonCode != tc.wantReason || dispatchErr.ActionExecuted != "unknown" {
				t.Fatalf("VerifyState error = %#v, want reason %q", err, tc.wantReason)
			}
		})
	}
}

func TestHomeAssistantVerifyStateRejectsUnboundedOrPathLikeInputBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer server.Close()
	client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: server.URL, Token: "ha-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, tc := range []struct {
		entityID string
		state    string
	}{
		{entityID: "light.kitchen/../../config", state: "off"},
		{entityID: "light.kitchen", state: strings.Repeat("x", maxHAStateBytes+1)},
	} {
		err := client.VerifyState(t.Context(), tc.entityID, tc.state)
		var dispatchErr *HomeAssistantDispatchError
		if !errors.As(err, &dispatchErr) || dispatchErr.ReasonCode != "ha_state_verification_request_invalid" {
			t.Fatalf("VerifyState(%q) error = %#v", tc.entityID, err)
		}
	}
	if calls != 0 {
		t.Fatalf("network calls = %d, want 0", calls)
	}
}

func TestHomeAssistantErrorsNeverExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("sensitive-home-assistant-body"))
	}))
	defer server.Close()
	client, err := NewHomeAssistantClient(HomeAssistantOptions{BaseURL: server.URL, Token: "ha-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.Converse(t.Context(), "turn off", "en")
	if err == nil || strings.Contains(err.Error(), "sensitive-home-assistant-body") {
		t.Fatalf("Converse error exposed response body: %v", err)
	}
}
