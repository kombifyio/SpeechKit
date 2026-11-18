//go:build linux

package voiceagent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	publiccascaded "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/voiceagent/a2a"
)

// Sensitive boundary: only an operator-supplied signer admits native media
// and authorizes the resulting canonical agent request.
func TestVoiceAgentNativeSignerRequiredBeforeProviderConnect(t *testing.T) {
	for _, mode := range []string{"absent", "empty", "configured"} {
		t.Run(mode, func(t *testing.T) {
			signed := false
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mac := hmac.New(sha256.New, []byte("operator-secret"))
				_, _ = mac.Write([]byte("v1\nprimary\n" + r.Header.Get("X-Kombify-A2a-Delegation-Timestamp") + "\n" + r.Header.Get("X-Kombify-A2a-Delegation-Context")))
				signed = hmac.Equal([]byte(r.Header.Get("X-Kombify-A2a-Delegation-Signature")), []byte("v1="+base64.RawURLEncoding.EncodeToString(mac.Sum(nil))))
				_, _ = io.WriteString(w, `{"jsonrpc":"2.0","result":{"parts":[{"kind":"text","text":"Ready."}]}}`)
			}))
			defer upstream.Close()
			var signer func() string
			switch mode {
			case "empty":
				signer = func() string { return " " }
			case "configured":
				signer = func() string { return "operator-secret" }
			}
			media := newFakeProvider()
			provider := &registeredNativeProvider{LiveProviderAdapter: media, sessions: newNativeVoiceSessions("https://voice.example/v1", nil, "", signer, func(_ context.Context, cfg LiveConfigFrame) (NativeVoiceConsent, error) {
				return cfg.NativeConsent, nil
			}, nil), provider: "deepgram"}
			defer func() { _ = provider.Close() }()
			expiry := time.Now().Add(time.Minute).Unix()
			cfg := LiveConfigFrame{AgentTargetID: "registered-agent", AgentEndpoint: upstream.URL, CapabilityLease: boundTestToken(expiry), OboSubjectToken: boundTestToken(expiry), CredentialExpiresAt: expiry, VoiceSessionID: "native-session", AISessionID: "durable-thread", OwnerUserID: "owner", Locale: "en"}
			cfg.NativeConsent = NativeVoiceConsent{Verified: true, CloudProcessing: true, ExpiresAt: expiry}
			err := provider.Connect(context.Background(), cfg)
			if mode != "configured" {
				if err == nil || media.connectCfg != nil || signed {
					t.Fatal("missing operator signer admitted native media or agent work")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			media.push(&LiveMessage{InputTranscript: "Hello", InputTranscriptDone: true})
			if _, err := provider.Receive(context.Background()); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"Hello"}]}`))
			req.Header.Set("Authorization", "Bearer "+media.connectCfg.NativeLLMToken)
			response := httptest.NewRecorder()
			provider.callback.ServeHTTP(response, req)
			if response.Code != http.StatusOK || !signed {
				t.Fatal("native callback failed to authorize canonical agent work with the configured signer")
			}
		})
	}
}

func TestRegisteredAgentHeadersBindTurnAndDisclosureIsSpokenOnce(t *testing.T) {
	leasePayload, _ := json.Marshal(map[string]any{"capabilities": []string{"agent.conversation"}, "exp": time.Now().Add(time.Minute).Unix()})
	lease := "header." + base64.RawURLEncoding.EncodeToString(leasePayload) + ".signature"
	var received http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","result":{"parts":[{"kind":"text","text":"Alles läuft."}]}}`)
	}))
	defer server.Close()
	agent, err := a2a.New(a2a.Config{
		Endpoint:      server.URL,
		TargetAgentID: "kombify-ai",
		SessionID:     "thread-1",
		Headers: registeredAgentHeaders(LiveConfigFrame{
			AgentTargetID: "kombify-ai", AgentEndpoint: server.URL, CapabilityLease: lease,
			OwnerUserID: "owner", OwnerOrgID: "org", OwnerPlan: "pro", OboSubjectToken: "subject-token",
			CredentialExpiresAt: time.Now().Add(time.Minute).Unix(),
		}, "signing-secret"),
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	disclosing := &disclosingAgent{inner: agent}
	first, err := disclosing.Run(context.Background(), publiccascaded.AgentInput{Utterance: "Status?", Locale: "de-DE"})
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if !strings.HasPrefix(first.Text, "Hinweis: Du sprichst mit einem KI-Assistenten.") {
		t.Fatalf("missing spoken disclosure: %q", first.Text)
	}
	second, err := disclosing.Run(context.Background(), publiccascaded.AgentInput{Utterance: "Und jetzt?", Locale: "de-DE"})
	if err != nil || strings.HasPrefix(second.Text, "Hinweis:") {
		t.Fatalf("disclosure must occur once: %q err=%v", second.Text, err)
	}
	if received.Get("Authorization") != "Bearer subject-token" {
		t.Fatal("delegated AI session credential missing")
	}
	if received.Get("X-Kombify-Capability-Lease") != lease {
		t.Fatal("registered-agent capability lease missing")
	}
	encoded := received.Get("X-Kombify-A2a-Delegation-Context")
	timestamp := received.Get("X-Kombify-A2a-Delegation-Timestamp")
	mac := hmac.New(sha256.New, []byte("signing-secret"))
	_, _ = mac.Write([]byte("v1\nprimary\n" + timestamp + "\n" + encoded))
	want := "v1=" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(received.Get("X-Kombify-A2a-Delegation-Signature")), []byte(want)) {
		t.Fatal("delegation signature mismatch")
	}
}

func TestRegisteredInstanceTurnCarriesOnlyCapturedRuntimeCredentials(t *testing.T) {
	expiry := time.Now().Add(time.Minute).Unix()
	leasePayload, _ := json.Marshal(map[string]any{"capabilities": []string{"agent.conversation"}, "exp": expiry})
	lease := "header." + base64.RawURLEncoding.EncodeToString(leasePayload) + ".signature"
	forwarded := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Get("X-Kombify-Instance-Auth") == "v1.captured.handle" && r.Header.Get("X-Kombify-Instance-Ai-Token") == "captured-ai-token"
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+`{"jsonrpc":"2.0","result":{"kind":"task","status":{"state":"completed"},"artifacts":[{"name":"run_task","parts":[{"kind":"data","data":{"text":"Owned instance answered."}}]}]}}`+"\n\n")
	}))
	defer upstream.Close()
	agent, err := a2a.New(a2a.Config{Endpoint: upstream.URL, TargetAgentID: "instance:owned-uuid", SessionID: "current-chat",
		Headers: registeredAgentHeaders(LiveConfigFrame{AgentTargetID: "instance:owned-uuid", AgentEndpoint: upstream.URL, AgentInstanceAuth: "v1.captured.handle",
			CapabilityLease: lease, OboSubjectToken: "captured-ai-token", CredentialExpiresAt: expiry, OwnerUserID: "owner", OwnerOrgID: "org"}, "operator-secret")})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := agent.Run(context.Background(), publiccascaded.AgentInput{Utterance: "Answer in this instance"})
	if err != nil || answer.Text != "Owned instance answered." || !forwarded {
		t.Fatal("Captured instance handoff failed", err, answer.Text, forwarded)
	}
}

func modeTestConfig(expiresAt int64) LiveConfigFrame {
	return LiveConfigFrame{OwnerUserID: "auth0|owner", OwnerOrgID: "org_test", VoiceSessionID: "go-session-1", SystemPrompt: "be brief",
		ModeBinding: "binding", ModeSignature: "signature", ModeEndpoint: "https://api.kombify.io/v1/ai/responses", ModeExpiresAt: expiresAt}
}

// A mode turn is authorized by one narrow, request-bound, Ed25519-only service token: it names the actor, the scope,
// this body, this turn, this session and this owner, lives at most a minute and never outlives the binding; with no
// per-service key (the shared HS256 secret is never a fallback), an expired binding or no turn identity it is refused.
func TestModeTurnHeadersAreNarrowRequestBoundAndEd25519Only(t *testing.T) {
	body := []byte(`{"stream":true,"idempotencyKey":"turn-0000001","messages":[{"role":"user","content":"hi"}],"modelMode":"default"}`)
	ctx := context.Background()
	t.Setenv("SERVICE_AUTH_SECRET", "shared-secret")
	t.Setenv("SERVICE_AUTH_ED25519_PRIVATE_KEY", "")
	if _, err := modeTurnHeaders(modeTestConfig(time.Now().Add(time.Hour).Unix()))(ctx, body); err == nil {
		t.Fatal("a mode turn must not be signed with the shared secret when the per-service key is absent")
	}
	if err := ModeTurnSignerPreflight(); err == nil {
		t.Fatal("the preflight must fail without the per-service Ed25519 key")
	}
	t.Setenv("SERVICE_AUTH_ED25519_PRIVATE_KEY", "speechkit-1="+base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if err := ModeTurnSignerPreflight(); err != nil {
		t.Fatalf("a valid per-service key must pass the preflight: %v", err)
	}
	if _, err := modeTurnHeaders(modeTestConfig(time.Now().Add(-time.Minute).Unix()))(ctx, body); !errors.Is(err, errAuthorizationExpired) {
		t.Fatalf("an expired binding must refuse the turn, got %v", err)
	}
	if _, err := modeTurnHeaders(modeTestConfig(time.Now().Add(time.Hour).Unix()))(ctx, []byte(`{"messages":[]}`)); err == nil {
		t.Fatal("a body without a turn identity must be refused")
	}
	expiresAt := time.Now().Add(20 * time.Second).Unix()
	headers, err := modeTurnHeaders(modeTestConfig(expiresAt))(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{}
	parsed, _, err := jwt.NewParser().ParseUnverified(headers.Get("X-Kombify-Service-Auth"), claims)
	if err != nil || parsed.Method.Alg() != "EdDSA" || parsed.Header["kid"] != "speechkit-1" {
		t.Fatalf("the token must be an Ed25519 token with its key id: %v %v", err, parsed)
	}
	digest := sha256.Sum256(append([]byte("POST\n/v1/ai/responses\n"), body...))
	exp, _ := claims["exp"].(float64)
	iat, _ := claims["iat"].(float64)
	owner, _ := claims["on_behalf_of"].(map[string]any)
	if claims["scope"] != "speechkit.voice_think.execute" || claims["svc"] != "speechkit-server" || claims["aud"] != "kombify-gateway" ||
		claims["req_id"] != "turn-0000001" || headers.Get("X-Kombify-Request-Id") != "turn-0000001" || claims["session_id"] != "go-session-1" ||
		claims["request_binding"] != hex.EncodeToString(digest[:]) || owner["sub"] != "auth0|owner" || owner["org_id"] != "org_test" ||
		claims["voice_mode_binding"] != "binding" || claims["voice_mode_signature"] != "signature" ||
		int64(exp) > expiresAt || exp-iat > 60 {
		t.Fatalf("the turn token must be narrow and request-bound: %v", claims)
	}
	for _, forbidden := range []string{"model", "tier", "roles", "target_agent_id"} {
		if _, present := claims[forbidden]; present {
			t.Fatalf("the turn token must not carry %q", forbidden)
		}
	}
}

// A denial of the platform (permission, quota, rate limit) reaches the vendor leg once as a refusal and is never a
// transient failure; the Companion's completed turns are handed to the fallback reasoner so it continues the same
// conversation.
func TestModeChatAgentReportsDenialsAndKeepsTheCompanionConversation(t *testing.T) {
	agent, err := newModeChatAgent(modeTestConfig(time.Now().Add(time.Hour).Unix()), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var refused []string
	agent.SetRefusalNotifier(func(code string) { refused = append(refused, code) })
	for _, code := range []string{"permission_denied", "quota_exhausted", "rate_limited", "auth_expired"} {
		agent.refused(&modeTurnError{code: code})
	}
	for _, code := range []string{"provider_unavailable", "turn_timeout", "provider_failed"} {
		agent.refused(&modeTurnError{code: code})
	}
	if strings.Join(refused, ",") != "permission_denied,quota_exhausted,rate_limited,auth_expired" {
		t.Fatalf("only denials are refusals, got %v", refused)
	}
	agent.Observe("what is the capital of France", "Paris.")
	agent.Observe("  ", "ignored")
	if len(agent.history) != 3 || agent.history[0].Role != "system" || agent.history[1].Content != "what is the capital of France" || agent.history[2].Content != "Paris." {
		t.Fatalf("the fallback must keep the system prompt and the Companion's turns, got %#v", agent.history)
	}
	agent.SetInstructions("be formal", "")
	if agent.history[0].Content != "be formal" || len(agent.history) != 3 {
		t.Fatalf("a persona change replaces only the system prompt, got %#v", agent.history)
	}
	if _, err := newModeChatAgent(modeTestConfig(time.Now().Add(-time.Minute).Unix()), nil, nil); err == nil {
		t.Fatal("an expired binding must not start a reasoner")
	}
}
