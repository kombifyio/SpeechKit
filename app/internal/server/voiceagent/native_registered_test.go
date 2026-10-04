//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
)

type nativeJournalFixture struct {
	mu        sync.Mutex
	resources map[string]store.VoiceProviderResource
}

func (j *nativeJournalFixture) SaveVoiceProviderResource(_ context.Context, r store.VoiceProviderResource) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.resources[r.Name] = r
	return nil
}
func (j *nativeJournalFixture) ListVoiceProviderResources(context.Context) ([]store.VoiceProviderResource, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var rows []store.VoiceProviderResource
	for _, r := range j.resources {
		rows = append(rows, r)
	}
	return rows, nil
}
func (j *nativeJournalFixture) DeleteVoiceProviderResource(_ context.Context, name string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.resources, name)
	return nil
}

// Regression: closing while Create is pending cannot reactivate the resource
// after a late acknowledgement, even if the first provider deletion fails.
func TestNativeCloseDuringCreateKeepsLateResourceReleased(t *testing.T) {
	journal := &nativeJournalFixture{resources: make(map[string]store.VoiceProviderResource)}
	creating, acknowledge := make(chan struct{}), make(chan struct{})
	deleteAllowed := false
	client := &http.Client{Transport: nativeRESTFixture(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusServiceUnavailable, ""
		if r.Method == http.MethodPost {
			close(creating)
			<-acknowledge
			status, body = http.StatusCreated, `{"id":"late-agent"}`
		} else if r.Method == http.MethodDelete && r.URL.Path == "/v1/agents/late-agent" && deleteAllowed {
			status = http.StatusNoContent
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	sessions := &nativeVoiceSessions{journal: journal, agents: assemblyai.Agents{APIKey: "fixture-provider-key", HTTPClient: client}, callbacks: make(map[string]*a2a.OpenAIHandler), publicURL: "https://voice.example/v1", signingSecret: func() string { return "fixture-secret" }, consentReader: func(_ context.Context, cfg LiveConfigFrame) (NativeVoiceConsent, error) {
		return cfg.NativeConsent, nil
	}}
	provider := &registeredNativeProvider{LiveProviderAdapter: newFakeProvider(), sessions: sessions, provider: "assemblyai"}
	result := make(chan error, 1)
	go func() {
		result <- provider.Connect(context.Background(), nativeConsentTestConfig())
	}()
	<-creating
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	close(acknowledge)
	if err := <-result; err == nil {
		t.Fatal("late provision connected a closed native session")
	}
	rows, err := journal.ListVoiceProviderResources(context.Background())
	if err != nil || len(rows) != 1 || !rows[0].Released || rows[0].VendorID != "late-agent" {
		t.Fatal("late acknowledgement lost released cleanup identity")
	}
	deleteAllowed = true
	sessions.cleanup(context.Background())
	rows, _ = journal.ListVoiceProviderResources(context.Background())
	if len(rows) != 0 {
		t.Fatal("released late resource could not be reconciled")
	}
}

func nativeConsentTestConfig() LiveConfigFrame {
	expiry := time.Now().Add(time.Minute).Unix()
	return LiveConfigFrame{AgentTargetID: "registered-agent", AgentEndpoint: "https://agent.example/a2a/registered-agent", CapabilityLease: boundTestToken(expiry), OboSubjectToken: boundTestToken(expiry), CredentialExpiresAt: expiry, VoiceSessionID: "voice-session", AISessionID: "durable-thread", OwnerUserID: "owner", NativeConsent: NativeVoiceConsent{Verified: true, CloudProcessing: true, RecordingAllowed: true, RecordingUpdatedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), ExpiresAt: expiry}}
}

// Sensitive provider boundary: the current authority read must not follow a
// credential-bearing redirect or accept incomplete/unbounded decisions.
func TestVoiceAgentCurrentConsentHTTPReadFailsClosed(t *testing.T) {
	var redirected atomic.Bool
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer redirect.Close()
	for _, mode := range []string{"valid", "redirect", "oversized", "trailing", "missing", "expired", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer bound-credential" {
					t.Error("current authority lost the trusted request signer")
				}
				if mode == "redirect" {
					w.Header().Set("Location", redirect.URL)
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				}
				if mode == "canceled" {
					<-r.Context().Done()
					return
				}
				expiry := time.Now().Add(time.Minute).Unix()
				if mode == "expired" {
					expiry = 1
				}
				body, _ := json.Marshal(map[string]any{"cloud_processing": true, "voice_agent_recording": false, "voice_agent_recording_updated_at": nil, "expiresAt": expiry})
				if mode == "missing" {
					body = []byte(`{"cloud_processing":true}`)
				}
				if mode == "trailing" {
					body = append(body, []byte(` {}`)...)
				}
				if mode == "oversized" {
					body = append(body, []byte(strings.Repeat(" ", 4096))...)
				}
				_, _ = w.Write(body)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			reader := NativeConsentHTTPReader{Endpoint: server.URL, Client: server.Client(), Authorize: func(r *http.Request, _ LiveConfigFrame) error {
				r.Header.Set("Authorization", "Bearer bound-credential")
				return nil
			}}
			decision, err := reader.Read(ctx, nativeConsentTestConfig())
			if mode == "valid" {
				if err != nil || !decision.Verified || !decision.CloudProcessing || decision.RecordingAllowed {
					t.Fatalf("valid decision rejected: %v", err)
				}
			} else if err == nil || decision.Verified {
				t.Fatal("unsafe current decision admitted provider authority")
			}
			if redirected.Load() {
				t.Fatal("current read forwarded its credential through a redirect")
			}
		})
	}
}

// Regression: revocation while journal/provider initialization is held cannot
// admit a later provider mutation or media connection, including a late create.
func TestVoiceAgentNativeInitializationRevocationStopsBeforeProviderIO(t *testing.T) {
	for _, stage := range []string{"journal", "create"} {
		t.Run(stage, func(t *testing.T) {
			granted := atomic.Bool{}
			granted.Store(true)
			entered, release, requestCanceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			journal := &nativeJournalFixture{resources: make(map[string]store.VoiceProviderResource)}
			var storage store.VoiceProviderResourceStore = journal
			if stage == "journal" {
				storage = &heldNativeJournal{nativeJournalFixture: journal, entered: entered, release: release}
			}
			var creates atomic.Int32
			client := &http.Client{Transport: nativeRESTFixture(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusNoContent, ""
				if r.Method == http.MethodPost {
					creates.Add(1)
					close(entered)
					<-r.Context().Done()
					close(requestCanceled)
					<-release // Simulate a late acknowledgement that ignored cancellation.
					status, body = http.StatusCreated, `{"id":"late-native"}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			sessions := &nativeVoiceSessions{publicURL: "https://voice.example/v1", journal: storage, agents: assemblyai.Agents{APIKey: "fixture", HTTPClient: client}, signingSecret: func() string { return "fixture" }, callbacks: make(map[string]*a2a.OpenAIHandler), consentReader: func(_ context.Context, cfg LiveConfigFrame) (NativeVoiceConsent, error) {
				consent := cfg.NativeConsent
				consent.CloudProcessing = granted.Load()
				return consent, nil
			}}
			media := newFakeProvider()
			provider := &registeredNativeProvider{LiveProviderAdapter: media, sessions: sessions, provider: "assemblyai"}
			defer func() { _ = provider.Close() }()
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			result := make(chan error, 1)
			go func() { result <- provider.Connect(context.Background(), nativeConsentTestConfig()) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("native initialization did not reach the held boundary")
			}
			granted.Store(false)
			if stage == "create" {
				select {
				case <-requestCanceled:
				case <-time.After(6 * time.Second):
					t.Fatal("revocation did not cancel held provider initialization")
				}
			}
			unblock()
			if err := <-result; err == nil || media.connectCfg != nil || (stage == "journal" && creates.Load() != 0) {
				t.Fatal("revoked initialization admitted provider or media effects")
			}
			if stage == "journal" {
				rows, err := journal.ListVoiceProviderResources(context.Background())
				if err != nil || len(rows) != 0 {
					t.Fatal("positively unissued provision intent remained pending forever")
				}
			}
			if err := provider.Connect(context.Background(), nativeConsentTestConfig()); err == nil || creates.Load() > 1 {
				t.Fatal("revoked initialization replayed a provider create")
			}
		})
	}
}

type heldNativeJournal struct {
	*nativeJournalFixture
	once             sync.Once
	entered, release chan struct{}
}

func (j *heldNativeJournal) SaveVoiceProviderResource(ctx context.Context, resource store.VoiceProviderResource) error {
	j.once.Do(func() { close(j.entered); <-j.release })
	return j.nativeJournalFixture.SaveVoiceProviderResource(ctx, resource)
}

type nativeRESTFixture func(*http.Request) (*http.Response, error)

func (f nativeRESTFixture) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Sensitive lifecycle boundary: a lost create acknowledgement is reconciled
// using its journaled name; cleanup cannot recreate it or select foreign agents.
func TestNativeCleanupReconcilesUncertainCreateWithoutTouchingForeignAgents(t *testing.T) {
	journal := &nativeJournalFixture{resources: map[string]store.VoiceProviderResource{
		"opaque-session-correlation": {Name: "opaque-session-correlation", Released: true},
	}}
	ownedExists, foreignExists := true, true
	client := &http.Client{Transport: nativeRESTFixture(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, ""
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents":
			encoded, _ := json.Marshal([]assemblyai.StoredAgent{{ID: "owned", Name: "opaque-session-correlation"}, {ID: "foreign", Name: "other-session"}})
			body = string(encoded)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/agents/owned":
			ownedExists = false
			status = http.StatusNoContent
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/agents/foreign":
			foreignExists = false
			status = http.StatusNoContent
		default:
			t.Error("cleanup attempted an unexpected provider mutation")
			status = http.StatusForbidden
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	sessions := &nativeVoiceSessions{journal: journal, agents: assemblyai.Agents{APIKey: "fixture-provider-key", HTTPClient: client}}
	sessions.cleanup(context.Background())
	if ownedExists || !foreignExists {
		t.Fatal("cleanup failed to delete only the journal-owned resource")
	}
	if rows, _ := journal.ListVoiceProviderResources(context.Background()); len(rows) != 0 {
		t.Fatal("acknowledged provider deletion remained pending")
	}
}
