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

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
	publiccascaded "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

func TestRegisteredAgentHeadersBindTurnAndDisclosureIsSpokenOnce(t *testing.T) {
	leasePayload, _ := json.Marshal(map[string]any{"capabilities": []string{"agent.conversation"}})
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
