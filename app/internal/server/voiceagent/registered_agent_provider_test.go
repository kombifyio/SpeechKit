//go:build linux

package voiceagent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
	publiccascaded "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
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
			}), provider: "deepgram"}
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
